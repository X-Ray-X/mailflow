package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/idempotency"
	"github.com/X-Ray-X/mailflow/app/internal/mail"
)

// maxIdempotencyKey bounds the caller's key. It is long enough for a UUID and
// short enough that the database key stays small.
const maxIdempotencyKey = 200

// maxFlagBody is the whole PATCH body. Two booleans and JSON punctuation fit
// with room to spare.
const maxFlagBody = 4 << 10

// writeTimeout bounds the bookkeeping calls that must outlive the request
// context, such as recording a completed send after the client disconnected.
const writeTimeout = 5 * time.Second

type sendRequest struct {
	To      []addressDTO `json:"to"`
	Cc      []addressDTO `json:"cc"`
	Subject string       `json:"subject"`
	Text    string       `json:"text"`
}

type sendResponse struct {
	ID string `json:"id"`
}

type flagRequest struct {
	Read    *bool `json:"read"`
	Starred *bool `json:"starred"`
}

// send accepts a plain-text message for delivery. The idempotency claim comes
// first, so a retry cannot submit twice, and it is released only when the send
// certainly did not happen.
func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	var req sendRequest
	if err := decodeJSON(w, r, &req, s.sendBodyLimit()); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	msg, err := s.validateSend(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	hash, err := requestHash(msg)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	if s.records == nil {
		// Fail closed: a send that cannot be recorded could be repeated by a
		// retry, so it is not attempted at all.
		s.log.Error("send refused: no idempotency record store configured")
		writeError(w, http.StatusServiceUnavailable, codeServiceUnavailable, "sending is unavailable")
		return
	}

	creds := credentials(r.Context())
	claim, err := s.records.Claim(r.Context(), creds.User, key, hash, s.idemTTL, s.idemLease)
	if err != nil {
		s.log.Error("could not claim the idempotency key", "error", err)
		writeError(w, http.StatusServiceUnavailable, codeServiceUnavailable, "sending is unavailable")
		return
	}

	switch claim.State {
	case idempotency.Replay:
		w.Header().Set("Idempotency-Replayed", "true")
		writeJSON(w, http.StatusCreated, sendResponse{ID: claim.MessageID})
		return
	case idempotency.Mismatch:
		writeError(w, http.StatusConflict, codeConflict, "the idempotency key was already used for a different request")
		return
	case idempotency.InFlight:
		writeError(w, http.StatusConflict, codeInProgress, "a send with this idempotency key is already in progress")
		return
	}

	id, err := s.service.Send(r.Context(), creds, msg)
	if err != nil {
		if errors.Is(err, mail.ErrRejected) || errors.Is(err, mail.ErrInvalidInput) {
			// The adapter guarantees nothing was submitted, so the caller may
			// retry with the same key once the input is corrected.
			s.release(creds.User, key)
		}
		s.fail(w, r, err)
		return
	}

	// The message has been accepted. If recording fails, reporting an error
	// would invite a retry and a possible duplicate; the lease still bounds the
	// window, so success is reported and the failure is logged.
	if err := s.complete(creds.User, key, id); err != nil {
		s.log.Error("could not record the completed send", "error", err)
	}
	writeJSON(w, http.StatusCreated, sendResponse{ID: id})
}

// setFlags applies a desired-state flag update. Repeating the same patch
// changes nothing, so no idempotency key is involved.
func (s *Server) setFlags(w http.ResponseWriter, r *http.Request) {
	var req flagRequest
	if err := decodeJSON(w, r, &req, maxFlagBody); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}
	if req.Read == nil && req.Starred == nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "at least one of read or starred is required")
		return
	}

	summary, err := s.service.SetFlags(r.Context(), credentials(r.Context()), r.PathValue("messageID"), mail.FlagPatch{
		Read:    req.Read,
		Starred: req.Starred,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMessageSummaryDTO(summary))
}

// validateSend applies the contract's bounds before anything reaches the mail
// server. Recipient domain equality is checked by the adapter, which is the
// layer that knows the caller's identity.
func (s *Server) validateSend(req sendRequest) (mail.OutgoingMessage, error) {
	to := toMailAddresses(req.To)
	cc := toMailAddresses(req.Cc)

	recipients := len(to) + len(cc)
	if recipients == 0 {
		return mail.OutgoingMessage{}, errors.New("at least one recipient is required")
	}
	if s.sendMaxRecipients > 0 && recipients > s.sendMaxRecipients {
		return mail.OutgoingMessage{}, fmt.Errorf("a message may name at most %d recipients", s.sendMaxRecipients)
	}
	if s.sendMaxBytes > 0 && int64(len(req.Text)) > s.sendMaxBytes {
		return mail.OutgoingMessage{}, fmt.Errorf("the text body may be at most %d bytes", s.sendMaxBytes)
	}
	for _, address := range append(append([]mail.Address{}, to...), cc...) {
		if !validAddress(address.Email) {
			return mail.OutgoingMessage{}, fmt.Errorf("%q is not a valid e-mail address", address.Email)
		}
	}

	return mail.OutgoingMessage{
		To:      to,
		Cc:      cc,
		Subject: req.Subject,
		Text:    req.Text,
	}, nil
}

// requestHash is the canonical fingerprint of a validated send. The same
// request produces the same bytes; a different recipient, subject, or body does
// not. Nil and empty address lists hash alike because both mean "none".
func requestHash(msg mail.OutgoingMessage) ([]byte, error) {
	fingerprint := struct {
		To      []addressDTO `json:"to"`
		Cc      []addressDTO `json:"cc"`
		Subject string       `json:"subject"`
		Text    string       `json:"text"`
	}{
		To:      toAddressDTOs(msg.To),
		Cc:      toAddressDTOs(msg.Cc),
		Subject: msg.Subject,
		Text:    msg.Text,
	}
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return nil, fmt.Errorf("fingerprint send request: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return sum[:], nil
}

// idempotencyKey reads and validates the caller's key. It is required: without
// one a retry is indistinguishable from a second message.
func idempotencyKey(r *http.Request) (string, error) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return "", errors.New("Idempotency-Key is required")
	}
	if len(key) > maxIdempotencyKey {
		return "", fmt.Errorf("Idempotency-Key must be at most %d characters", maxIdempotencyKey)
	}
	for _, c := range key {
		if c < 0x21 || c > 0x7e {
			return "", errors.New("Idempotency-Key must contain only visible ASCII characters")
		}
	}
	return key, nil
}

// sendBodyLimit allows for JSON escaping on top of the text body. The body
// bound that matters is the decoded text, checked by validateSend.
func (s *Server) sendBodyLimit() int64 {
	const overhead = 1 << 20
	if s.sendMaxBytes <= 0 {
		return 8 << 20
	}
	return s.sendMaxBytes + overhead
}

// decodeJSON reads exactly one JSON object, rejects unknown fields, and bounds
// the body. The message it returns describes the caller's own input, never a
// vendor value.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	if limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errors.New("request body is too large")
		}
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return fmt.Errorf("invalid request body: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}

// validAddress is a shape check, not a deliverability check. The mail server
// remains the authority on whether an address exists.
func validAddress(email string) bool {
	if email == "" || strings.Count(email, "@") != 1 {
		return false
	}
	if strings.ContainsAny(email, " \t\r\n<>") {
		return false
	}
	at := strings.Index(email, "@")
	return at > 0 && at < len(email)-1
}

func toMailAddresses(in []addressDTO) []mail.Address {
	out := make([]mail.Address, 0, len(in))
	for _, a := range in {
		out = append(out, mail.Address{Name: a.Name, Email: a.Email})
	}
	return out
}

// release removes a claim that certainly produced no message. It runs on a
// bounded context so a cancelled request cannot skip the cleanup.
func (s *Server) release(account, key string) {
	if s.records == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := s.records.Release(ctx, account, key); err != nil {
		s.log.Warn("could not release the idempotency claim", "error", err)
	}
}

// complete records a successful send. It also runs detached from the request
// so a client disconnect after the submission does not lose the record.
func (s *Server) complete(account, key, messageID string) error {
	if s.records == nil {
		return errors.New("no idempotency record store configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return s.records.Complete(ctx, account, key, messageID, s.idemTTL)
}
