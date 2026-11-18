// Package api publishes Mailflow's contract over HTTP.
//
// It depends on mail.Reader and mail.Writer, never on the mail server's
// protocol, and every response body is built from this package's own types.
// That is what keeps vendor detail out of the public payload: there is no field
// to carry it.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/idempotency"
	"github.com/X-Ray-X/mailflow/app/internal/mail"
)

// Error codes are part of the contract. Clients may branch on them; messages
// are for humans and may change.
const (
	codeUnauthorized       = "unauthorized"
	codeNotFound           = "not_found"
	codeInvalidCursor      = "invalid_cursor"
	codeInvalidRequest     = "invalid_request"
	codeMethodNotAllowed   = "method_not_allowed"
	codeServiceUnavailable = "service_unavailable"
	codeInternal           = "internal"
	codeConflict           = "conflict"
	codeInProgress         = "in_progress"
	codeSendRejected       = "send_rejected"
	codeAlreadyExists      = "already_exists"
	codeMailboxNotEmpty    = "mailbox_not_empty"
)

// Service is the mail contract the HTTP layer depends on.
type Service interface {
	mail.Reader
	mail.Writer
}

// Options are the server's public bounds and its write-path dependencies.
type Options struct {
	// Timeout bounds one request, including the mail server call.
	Timeout time.Duration
	// PageSizeMax is the largest page the contract will return.
	PageSizeMax int
	Logger      *slog.Logger

	// Records is the durable idempotency record store. A nil store makes
	// sends fail closed; it never lets one through unrecorded.
	Records idempotency.Recorder
	// IdempotencyTTL is how long a completed send key resolves to its result.
	IdempotencyTTL time.Duration
	// IdempotencyLease bounds how long an unfinished claim blocks its key.
	IdempotencyLease time.Duration

	// SendMaxRecipients bounds to plus cc. Zero means no bound, which is only
	// useful in tests; configuration always supplies a positive value.
	SendMaxRecipients int
	// SendMaxBytes bounds the text body in bytes. Zero means no bound.
	SendMaxBytes int64
}

// Server is the HTTP surface.
type Server struct {
	service           Service
	records           idempotency.Recorder
	idemTTL           time.Duration
	idemLease         time.Duration
	sendMaxRecipients int
	sendMaxBytes      int64

	timeout     time.Duration
	pageSizeMax int
	log         *slog.Logger
	handler     http.Handler
}

// New builds the server and its routes.
func New(service Service, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Server{
		service:           service,
		records:           opts.Records,
		idemTTL:           opts.IdempotencyTTL,
		idemLease:         opts.IdempotencyLease,
		sendMaxRecipients: opts.SendMaxRecipients,
		sendMaxBytes:      opts.SendMaxBytes,
		timeout:           opts.Timeout,
		pageSizeMax:       opts.PageSizeMax,
		log:               opts.Logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/v1/mailboxes", s.protected(s.mailboxes))
	mux.HandleFunc("/v1/mailboxes/{mailboxID}", s.protected(s.mailboxByID))
	mux.HandleFunc("/v1/mailboxes/{mailboxID}/messages", s.protected(s.messages))
	mux.HandleFunc("/v1/messages", s.protected(s.send))
	mux.HandleFunc("/v1/messages/{messageID}", s.protected(s.messageByID))
	mux.HandleFunc("/v1/messages/{messageID}/reply", s.protected(s.reply))
	mux.HandleFunc("/v1/drafts", s.protected(s.drafts))
	mux.HandleFunc("/v1/drafts/{draftID}/send", s.protected(s.sendDraft))
	// Catch-all so an unknown path is a JSON error like every other failure.
	mux.HandleFunc("/", s.notFound)

	s.handler = s.withLogging(s.withRecovery(mux))
	return s
}

// protected wraps a contract handler with the request timeout and
// authentication, in that order, so both apply to every mail endpoint.
func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return s.withTimeout(s.withAuth(next))
}

// Handler returns the http.Handler to serve.
func (s *Server) Handler() http.Handler { return s.handler }

type credentialsKey struct{}

// credentials returns the caller's credentials, which only the authentication
// wrapper places in the context.
func credentials(ctx context.Context) mail.Credentials {
	creds, _ := ctx.Value(credentialsKey{}).(mail.Credentials)
	return creds
}

// withAuth requires HTTP Basic credentials and forwards them unchanged. The
// service never validates them itself; the mail server remains the authority.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user == "" {
			writeUnauthorized(w)
			return
		}
		ctx := context.WithValue(r.Context(), credentialsKey{}, mail.Credentials{User: user, Password: password})
		next(w, r.WithContext(ctx))
	}
}

// withTimeout bounds the whole request, so a slow mail server cannot hold a
// connection open past the configured limit.
func (s *Server) withTimeout(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.timeout <= 0 {
			next(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic serving request", "pattern", r.Pattern, "panic", fmt.Sprint(rec))
				writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withLogging records one line per request. It logs the route pattern, not the
// raw path, and never the Authorization header or credentials.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		pattern := r.Pattern
		if pattern == "" {
			pattern = "unmatched"
		}
		s.log.Info("request",
			"method", r.Method,
			"route", pattern,
			"status", rec.statusCode(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// errorBody is the only error shape the contract has.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Mail content is private; no intermediary may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Code: code, Message: message})
}

// writeNoContent answers a successful write that has no response body.
func writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="mailflow", charset="UTF-8"`)
	writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
}

// fail maps one internal failure to a stable code and a fixed message. The
// cause is deliberately not included: it may name the vendor.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, mail.ErrUnauthorized):
		writeUnauthorized(w)
	case errors.Is(err, mail.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, mail.ErrInvalidCursor):
		writeError(w, http.StatusBadRequest, codeInvalidCursor, "the cursor is no longer valid; restart from the first page")
	case errors.Is(err, mail.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "the request is not valid for this account")
	case errors.Is(err, mail.ErrAlreadyExists):
		writeError(w, http.StatusConflict, codeAlreadyExists, "a mailbox with that name already exists")
	case errors.Is(err, mail.ErrMailboxNotEmpty):
		writeError(w, http.StatusConflict, codeMailboxNotEmpty, "the mailbox still holds messages or child mailboxes")
	case errors.Is(err, mail.ErrRejected):
		writeError(w, http.StatusUnprocessableEntity, codeSendRejected, "the message was not accepted for delivery")
	case errors.Is(err, mail.ErrConflict):
		writeError(w, http.StatusConflict, codeConflict, "the idempotency key was already used for a different request")
	case errors.Is(err, mail.ErrInProgress):
		writeError(w, http.StatusConflict, codeInProgress, "a send with this idempotency key is already in progress")
	case errors.Is(err, mail.ErrUnavailable):
		s.log.Warn("mail service unavailable", "route", r.Pattern)
		writeError(w, http.StatusServiceUnavailable, codeServiceUnavailable, "the mail service is unavailable")
	default:
		s.log.Error("request failed", "route", r.Pattern, "error", err.Error())
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, codeNotFound, "not found")
}

// requireGet answers 405 for anything but GET or HEAD.
func (s *Server) requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	writeMethodNotAllowed(w, "GET, HEAD")
	return false
}

// writeMethodNotAllowed answers with the contract's JSON error shape and names
// the methods the path does accept.
func writeMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed")
}

// pageSize parses the limit parameter. Absent means the adapter's default;
// anything above the maximum is clamped, as the contract promises.
func (s *Server) pageSize(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer")
	}
	if n < 1 {
		return 0, fmt.Errorf("limit must be positive")
	}
	if s.pageSizeMax > 0 && n > s.pageSizeMax {
		n = s.pageSizeMax
	}
	return n, nil
}
