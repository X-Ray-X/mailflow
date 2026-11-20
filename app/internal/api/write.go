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

// maxPatchBody is the whole message PATCH body. Two booleans, one identifier,
// and JSON punctuation fit with room to spare.
const maxPatchBody = 4 << 10

// maxMailboxBody is the whole create or rename body. A name is at most 255
// bytes, so this is generous.
const maxMailboxBody = 4 << 10

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

type messagePatchRequest struct {
	Read      *bool   `json:"read"`
	Starred   *bool   `json:"starred"`
	MailboxID *string `json:"mailboxId"`
}

type mailboxRequest struct {
	Name string `json:"name"`
}

type mailboxResponse struct {
	ID string `json:"id"`
}

type draftRequest struct {
	To      []addressDTO `json:"to"`
	Cc      []addressDTO `json:"cc"`
	Subject string       `json:"subject"`
	Text    string       `json:"text"`
}

type replyRequest struct {
	Text string `json:"text"`
}

type draftResponse struct {
	ID string `json:"id"`
}

// Send godoc
// @Summary Send a plain-text message
// @Description Requires an idempotency key; the sender is the authenticated account.
// @Tags messages
// @Security BasicAuth
// @Accept json
// @Produce json
// @Param Idempotency-Key header string true "Idempotency key"
// @Param request body sendRequest true "Message to send"
// @Success 201 {object} sendResponse
// @Failure 400 {object} errorBody
// @Failure 409 {object} errorBody
// @Failure 422 {object} errorBody
// @Router /v1/messages [post]
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

// UpdateMessage godoc
// @Summary Patch a message's flags and optionally move it
// @Tags messages
// @Security BasicAuth
// @Accept json
// @Produce json
// @Param messageID path string true "Mailflow message identifier"
// @Param request body messagePatchRequest true "Desired state"
// @Success 200 {object} messageDTO
// @Failure 400 {object} errorBody
// @Failure 404 {object} errorBody
// @Router /v1/messages/{messageID} [patch]
// updateMessage applies a desired-state message patch: flags, a move, or both.
// Repeating the same patch changes nothing, so no idempotency key is involved.
func (s *Server) updateMessage(w http.ResponseWriter, r *http.Request) {
	var req messagePatchRequest
	if err := decodeJSON(w, r, &req, maxPatchBody); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}
	if req.Read == nil && req.Starred == nil && req.MailboxID == nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "at least one of read, starred, or mailboxId is required")
		return
	}

	patch := mail.MessagePatch{Read: req.Read, Starred: req.Starred}
	if req.MailboxID != nil {
		if *req.MailboxID == "" {
			writeError(w, http.StatusBadRequest, codeInvalidRequest, "mailboxId must not be empty")
			return
		}
		patch.MailboxID = *req.MailboxID
	}

	summary, err := s.service.UpdateMessage(r.Context(), credentials(r.Context()), r.PathValue("messageID"), patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMessageSummaryDTO(summary))
}

// DeleteMessage godoc
// @Summary Move a message to trash
// @Tags messages
// @Security BasicAuth
// @Param messageID path string true "Mailflow message identifier"
// @Success 204 {string} string "No Content"
// @Failure 401 {object} errorBody
// @Failure 404 {object} errorBody
// @Router /v1/messages/{messageID} [delete]
// deleteMessage moves the message to trash. It never erases it, so the caller
// can restore it by moving it out again.
func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	if err := s.service.DeleteMessage(r.Context(), credentials(r.Context()), r.PathValue("messageID")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeNoContent(w)
}

// CreateMailbox godoc
// @Summary Create a custom mailbox
// @Tags mailboxes
// @Security BasicAuth
// @Accept json
// @Produce json
// @Param request body mailboxRequest true "Mailbox name"
// @Success 201 {object} mailboxResponse
// @Failure 400 {object} errorBody
// @Failure 409 {object} errorBody
// @Router /v1/mailboxes [post]
// createMailbox creates a top-level custom mailbox. The name is the natural
// key, so a retry cannot make a second one; it answers already_exists instead.
func (s *Server) createMailbox(w http.ResponseWriter, r *http.Request) {
	var req mailboxRequest
	if err := decodeJSON(w, r, &req, maxMailboxBody); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}
	id, err := s.service.CreateMailbox(r.Context(), credentials(r.Context()), req.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, mailboxResponse{ID: id})
}

// RenameMailbox godoc
// @Summary Rename a custom mailbox
// @Tags mailboxes
// @Security BasicAuth
// @Accept json
// @Param mailboxID path string true "Mailflow mailbox identifier"
// @Param request body mailboxRequest true "Resulting name"
// @Success 204 {string} string "No Content"
// @Failure 400 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 409 {object} errorBody
// @Router /v1/mailboxes/{mailboxID} [patch]
func (s *Server) renameMailbox(w http.ResponseWriter, r *http.Request) {
	var req mailboxRequest
	if err := decodeJSON(w, r, &req, maxMailboxBody); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}
	if err := s.service.RenameMailbox(r.Context(), credentials(r.Context()), r.PathValue("mailboxID"), req.Name); err != nil {
		s.fail(w, r, err)
		return
	}
	writeNoContent(w)
}

// DeleteMailbox godoc
// @Summary Delete an empty custom mailbox
// @Tags mailboxes
// @Security BasicAuth
// @Param mailboxID path string true "Mailflow mailbox identifier"
// @Success 204 {string} string "No Content"
// @Failure 400 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 409 {object} errorBody
// @Router /v1/mailboxes/{mailboxID} [delete]
func (s *Server) deleteMailbox(w http.ResponseWriter, r *http.Request) {
	if err := s.service.DeleteMailbox(r.Context(), credentials(r.Context()), r.PathValue("mailboxID")); err != nil {
		s.fail(w, r, err)
		return
	}
	writeNoContent(w)
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
		// Do not echo the field name: it is caller input, and a response should
		// carry only the contract's own vocabulary.
		if strings.Contains(err.Error(), "unknown field") {
			return errors.New("request body contains an unknown field")
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
	return s.records.Complete(ctx, account, key, messageID, s.idemTTL, idempotency.SendEvent{
		Account:   account,
		MessageID: messageID,
	})
}

// CreateDraft godoc
// @Summary Save a plain-text draft
// @Description No idempotency key; every field is optional.
// @Tags drafts
// @Security BasicAuth
// @Accept json
// @Produce json
// @Param request body draftRequest true "Draft content"
// @Success 201 {object} draftResponse
// @Failure 400 {object} errorBody
// @Router /v1/drafts [post]
// drafts accepts a new draft. Draft creation is mailbox state with no external
// side effect, so it takes no idempotency key; a duplicate is visible and can be
// deleted.
func (s *Server) drafts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var req draftRequest
	if err := decodeJSON(w, r, &req, s.sendBodyLimit()); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}
	draft, err := s.validateDraft(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	id, err := s.service.CreateDraft(r.Context(), credentials(r.Context()), draft)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, draftResponse{ID: id})
}

// SendDraft godoc
// @Summary Submit an existing draft
// @Description Uses the send idempotency record and keeps the draft identifier.
// @Tags drafts
// @Security BasicAuth
// @Param Idempotency-Key header string true "Idempotency key"
// @Param draftID path string true "Mailflow draft identifier"
// @Success 201 {object} draftResponse
// @Failure 400 {object} errorBody
// @Failure 404 {object} errorBody
// @Failure 409 {object} errorBody
// @Router /v1/drafts/{draftID}/send [post]
// sendDraft submits an existing draft. It is a send, so it claims the same
// idempotency record a send does: a replay returns the recorded identifier and
// a key reused for another draft is refused.
func (s *Server) sendDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}

	draftID := r.PathValue("draftID")
	hash, err := draftSendHash(draftID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	if s.records == nil {
		// Fail closed, exactly as sending does: an unrecorded submission could
		// be repeated by a retry.
		s.log.Error("draft submission refused: no idempotency record store configured")
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
		writeJSON(w, http.StatusCreated, draftResponse{ID: claim.MessageID})
		return
	case idempotency.Mismatch:
		writeError(w, http.StatusConflict, codeConflict, "the idempotency key was already used for a different request")
		return
	case idempotency.InFlight:
		writeError(w, http.StatusConflict, codeInProgress, "a send with this idempotency key is already in progress")
		return
	}

	id, err := s.service.SendDraft(r.Context(), creds, draftID)
	if err != nil {
		// A refusal, a non-draft, or an unknown identifier all mean nothing was
		// submitted, so the caller may retry with the same key once the input is
		// corrected.
		if errors.Is(err, mail.ErrRejected) || errors.Is(err, mail.ErrInvalidInput) || errors.Is(err, mail.ErrNotFound) {
			s.release(creds.User, key)
		}
		s.fail(w, r, err)
		return
	}

	if err := s.complete(creds.User, key, id); err != nil {
		s.log.Error("could not record the completed draft submission", "error", err)
	}
	writeJSON(w, http.StatusCreated, draftResponse{ID: id})
}

// Reply godoc
// @Summary Create a draft reply to a message
// @Tags drafts
// @Security BasicAuth
// @Accept json
// @Produce json
// @Param messageID path string true "Mailflow message identifier"
// @Param request body replyRequest true "Reply text"
// @Success 201 {object} draftResponse
// @Failure 400 {object} errorBody
// @Failure 404 {object} errorBody
// @Router /v1/messages/{messageID}/reply [post]
// reply composes a draft answer to a message. It creates mailbox state rather
// than submitting anything, so it takes no idempotency key.
func (s *Server) reply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var req replyRequest
	if err := decodeJSON(w, r, &req, s.sendBodyLimit()); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, err.Error())
		return
	}
	if s.sendMaxBytes > 0 && int64(len(req.Text)) > s.sendMaxBytes {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, fmt.Sprintf("the text body may be at most %d bytes", s.sendMaxBytes))
		return
	}

	id, err := s.service.Reply(r.Context(), credentials(r.Context()), r.PathValue("messageID"), req.Text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, draftResponse{ID: id})
}

// validateDraft applies the same shape, recipient-count, and body-size bounds a
// send does, but requires no recipient: an unfinished draft is the point.
func (s *Server) validateDraft(req draftRequest) (mail.Draft, error) {
	to := toMailAddresses(req.To)
	cc := toMailAddresses(req.Cc)

	recipients := len(to) + len(cc)
	if s.sendMaxRecipients > 0 && recipients > s.sendMaxRecipients {
		return mail.Draft{}, fmt.Errorf("a draft may name at most %d recipients", s.sendMaxRecipients)
	}
	if s.sendMaxBytes > 0 && int64(len(req.Text)) > s.sendMaxBytes {
		return mail.Draft{}, fmt.Errorf("the text body may be at most %d bytes", s.sendMaxBytes)
	}
	for _, address := range append(append([]mail.Address{}, to...), cc...) {
		if !validAddress(address.Email) {
			return mail.Draft{}, fmt.Errorf("%q is not a valid e-mail address", address.Email)
		}
	}

	return mail.Draft{
		To:      to,
		Cc:      cc,
		Subject: req.Subject,
		Text:    req.Text,
	}, nil
}

// draftSendHash is the canonical fingerprint of a draft submission. The draft
// identifier is enough to identify the request: draft content is immutable, so
// one identifier always means one message.
func draftSendHash(draftID string) ([]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation string `json:"operation"`
		DraftID   string `json:"draftId"`
	}{Operation: "draft.send", DraftID: draftID})
	if err != nil {
		return nil, fmt.Errorf("fingerprint draft submission: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return sum[:], nil
}
