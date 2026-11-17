// Package mail holds Mailflow's mail model and the translation from the mail
// server's wire types.
//
// Everything a caller sees is defined here in Mailflow terms: read and starred
// rather than protocol keywords, custom rather than a vendor role, Mailflow
// identifiers rather than server identifiers. The public JSON is built from
// these types, so a vendor field cannot appear in a payload by accident.
package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/X-Ray-X/mailflow/app/internal/ids"
	"github.com/X-Ray-X/mailflow/app/internal/jmap"
)

// Errors are the failure classes the HTTP layer maps to status codes.
var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrUnauthorized  = errors.New("authentication required")
	ErrUnavailable   = errors.New("mail service unavailable")
	// ErrRejected means the mail server declined the message itself: a bad
	// recipient, a sender it will not accept, or a message it refuses. Retrying
	// the same input cannot succeed, which is why it is separate from an outage.
	ErrRejected = errors.New("mail server declined the message")
	// ErrConflict means an idempotency key was reused for a different request.
	ErrConflict = errors.New("idempotency key reused for a different request")
	// ErrInProgress means a duplicate arrived while the first attempt is still
	// being processed.
	ErrInProgress = errors.New("a send with this idempotency key is in progress")
	// ErrInvalidInput means the request is well-formed HTTP but cannot be
	// honoured for this account, such as a recipient outside the local mail
	// domain. Nothing was submitted.
	ErrInvalidInput = errors.New("request is not valid for this account")
)

// Credentials are forwarded to the mail server on every request and never
// stored.
type Credentials struct {
	User     string
	Password string
}

// Address is a Mailflow person.
type Address struct {
	Name  string
	Email string
}

// Mailbox is a folder as the contract exposes it.
type Mailbox struct {
	ID     string
	Name   string
	Role   string
	Unread int
	Total  int
}

// MessageSummary is one row of a message listing.
type MessageSummary struct {
	ID            string
	Subject       string
	From          Address
	Received      time.Time
	Read          bool
	Starred       bool
	HasAttachment bool
}

// Message is one message with its text body.
type Message struct {
	MessageSummary
	To            []Address
	Cc            []Address
	Text          string
	TextTruncated bool
}

// PageRequest is one page of a message listing.
type PageRequest struct {
	Limit  int
	Cursor string
}

// Page is one page of a message listing. NextCursor is set only when another
// page exists.
type Page struct {
	Messages   []MessageSummary
	NextCursor string
	HasMore    bool
}

// Reader is the read side of the HTTP contract.
type Reader interface {
	Mailboxes(ctx context.Context, creds Credentials) ([]Mailbox, error)
	Messages(ctx context.Context, creds Credentials, mailboxID string, page PageRequest) (Page, error)
	Message(ctx context.Context, creds Credentials, messageID string) (Message, error)
}

// OutgoingMessage is a validated message to send. It has no sender field: the
// sender is always the authenticated account's identity.
type OutgoingMessage struct {
	To      []Address
	Cc      []Address
	Subject string
	Text    string
}

// FlagPatch is a desired-state flag update. A nil field is left unchanged, so
// repeating a patch changes nothing.
type FlagPatch struct {
	Read    *bool
	Starred *bool
}

// Writer is the write side of the HTTP contract.
type Writer interface {
	// Send returns the Mailflow identifier of the sent message.
	Send(ctx context.Context, creds Credentials, msg OutgoingMessage) (string, error)
	// SetFlags applies the patch and returns the message's new summary.
	SetFlags(ctx context.Context, creds Credentials, messageID string, patch FlagPatch) (MessageSummary, error)
}

// upstream is the part of the JMAP client the service uses. It is an interface
// so the translation can be tested without a server.
type upstream interface {
	FetchSession(ctx context.Context, creds jmap.Credentials) (jmap.Session, error)
	Mailboxes(ctx context.Context, creds jmap.Credentials, accountID string) ([]jmap.Mailbox, error)
	MailboxExists(ctx context.Context, creds jmap.Credentials, accountID, mailboxID string) (bool, error)
	QueryEmails(ctx context.Context, creds jmap.Credentials, accountID string, q jmap.Query) (jmap.QueryResult, error)
	GetEmails(ctx context.Context, creds jmap.Credentials, accountID string, opts jmap.GetEmailsOptions) ([]jmap.Email, error)
	Identities(ctx context.Context, creds jmap.Credentials, accountID string) ([]jmap.Identity, error)
	SendMessage(ctx context.Context, creds jmap.Credentials, accountID string, msg jmap.OutgoingEmail) (string, error)
	DestroyEmails(ctx context.Context, creds jmap.Credentials, accountID string, ids ...string) error
	SetEmailKeywords(ctx context.Context, creds jmap.Credentials, accountID, emailID string, read, starred *bool) error
}

// Service adapts the mail server to the Reader and Writer contracts.
type Service struct {
	upstream        upstream
	pageSizeDefault int
	pageSizeMax     int
	bodyMaxBytes    int64
	log             *slog.Logger
}

// NewService wires the adapter with the configured bounds.
func NewService(client upstream, pageSizeDefault, pageSizeMax int, bodyMaxBytes int64, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		upstream:        client,
		pageSizeDefault: pageSizeDefault,
		pageSizeMax:     pageSizeMax,
		bodyMaxBytes:    bodyMaxBytes,
		log:             log,
	}
}

// Mailboxes lists the caller's mailboxes.
func (s *Service) Mailboxes(ctx context.Context, creds Credentials) ([]Mailbox, error) {
	account, err := s.account(ctx, creds)
	if err != nil {
		return nil, err
	}
	list, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return nil, mapError(err)
	}

	sorted := append([]jmap.Mailbox(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].SortOrder != sorted[j].SortOrder {
			return sorted[i].SortOrder < sorted[j].SortOrder
		}
		return sorted[i].Name < sorted[j].Name
	})

	out := make([]Mailbox, 0, len(sorted))
	for _, m := range sorted {
		out = append(out, MapMailbox(account, m))
	}
	return out, nil
}

// Messages returns one page of a mailbox, newest first.
func (s *Service) Messages(ctx context.Context, creds Credentials, mailboxID string, page PageRequest) (Page, error) {
	mailboxAccount, vendorMailbox, err := ids.ParseMailbox(mailboxID)
	if err != nil {
		// A value that is not a mailbox identifier cannot name a mailbox; the
		// caller learns nothing about whether one exists.
		return Page{}, ErrNotFound
	}

	limit := page.Limit
	if limit <= 0 {
		limit = s.pageSizeDefault
	}
	if limit > s.pageSizeMax {
		limit = s.pageSizeMax
	}

	var (
		anchor        string
		cursorAccount string
	)
	if page.Cursor != "" {
		cursorAccount, anchor, err = ids.ParseCursor(page.Cursor)
		if err != nil {
			return Page{}, ErrInvalidCursor
		}
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return Page{}, err
	}

	// The mail server reuses raw identifiers across accounts, so an identifier
	// minted for another account must be refused here rather than resolved
	// against this caller's objects.
	if mailboxAccount != account {
		return Page{}, ErrNotFound
	}
	if cursorAccount != "" && cursorAccount != account {
		return Page{}, ErrInvalidCursor
	}

	// Existence has to be asked for separately: this release answers a query for
	// an unknown mailbox with an empty result, which is indistinguishable from an
	// empty mailbox (see docs/integration-surface.md).
	exists, err := s.upstream.MailboxExists(ctx, toJMAP(creds), account, vendorMailbox)
	if err != nil {
		return Page{}, mapError(err)
	}
	if !exists {
		return Page{}, ErrNotFound
	}

	result, err := s.upstream.QueryEmails(ctx, toJMAP(creds), account, jmap.Query{
		MailboxID: vendorMailbox,
		Limit:     limit,
		Anchor:    anchor,
	})
	if err != nil {
		return Page{}, mapError(err)
	}

	pageOut := Page{
		Messages: []MessageSummary{},
		HasMore:  result.Position+len(result.IDs) < result.Total,
	}
	if len(result.IDs) == 0 {
		return pageOut, nil
	}

	fetched, err := s.upstream.GetEmails(ctx, toJMAP(creds), account, jmap.GetEmailsOptions{IDs: result.IDs})
	if err != nil {
		return Page{}, mapError(err)
	}

	byID := make(map[string]jmap.Email, len(fetched))
	for _, e := range fetched {
		byID[e.ID] = e
	}
	for _, id := range result.IDs {
		e, ok := byID[id]
		if !ok {
			// Removed between query and fetch; skip it rather than fail.
			continue
		}
		pageOut.Messages = append(pageOut.Messages, MapSummary(account, e))
	}
	// The cursor anchors on the last identifier the query returned, not the last
	// one that could be fetched. A message deleted between the two calls would
	// otherwise leave HasMore true with no cursor, which is a dead end for the
	// client; anchoring on the query's identifier either works or produces an
	// invalid_cursor the client can act on.
	if pageOut.HasMore {
		pageOut.NextCursor = ids.Cursor(account, result.IDs[len(result.IDs)-1])
	}
	return pageOut, nil
}

// Message returns one message with its text body.
func (s *Service) Message(ctx context.Context, creds Credentials, messageID string) (Message, error) {
	messageAccount, vendorMessage, err := ids.ParseMessage(messageID)
	if err != nil {
		return Message{}, ErrNotFound
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return Message{}, err
	}
	if messageAccount != account {
		return Message{}, ErrNotFound
	}

	list, err := s.upstream.GetEmails(ctx, toJMAP(creds), account, jmap.GetEmailsOptions{
		IDs:          []string{vendorMessage},
		IncludeBody:  true,
		MaxBodyBytes: s.bodyMaxBytes,
	})
	if err != nil {
		return Message{}, mapError(err)
	}
	if len(list) == 0 {
		return Message{}, ErrNotFound
	}
	return MapMessage(account, list[0], s.bodyMaxBytes), nil
}

// Send submits a plain-text message as the caller. The sender comes from the
// account's identity, never from the request. Recipients are restricted to the
// identity's domain, which is what keeps the service from becoming a relay.
func (s *Service) Send(ctx context.Context, creds Credentials, msg OutgoingMessage) (string, error) {
	account, err := s.account(ctx, creds)
	if err != nil {
		return "", err
	}

	identities, err := s.upstream.Identities(ctx, toJMAP(creds), account)
	if err != nil {
		return "", mapError(err)
	}
	identity, ok := chooseIdentity(identities, creds.User)
	if !ok {
		// A permanent condition for this account, not an outage.
		return "", fmt.Errorf("%w: the account has no sending identity", ErrRejected)
	}

	boxes, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return "", mapError(err)
	}
	var drafts, sent string
	for _, box := range boxes {
		switch box.Role {
		case "drafts":
			drafts = box.ID
		case "sent":
			sent = box.ID
		}
	}
	if drafts == "" || sent == "" {
		return "", fmt.Errorf("%w: the account has no drafts or sent mailbox", ErrUnavailable)
	}

	domain := addressDomain(identity.Email)
	if domain == "" {
		domain = addressDomain(creds.User)
	}
	if domain == "" {
		return "", fmt.Errorf("%w: the account has no usable mail domain", ErrRejected)
	}
	for _, recipient := range append(append([]Address{}, msg.To...), msg.Cc...) {
		if !strings.EqualFold(addressDomain(recipient.Email), domain) {
			return "", fmt.Errorf("%w: a recipient is outside the local mail domain", ErrInvalidInput)
		}
	}

	// The pinned release names the identity after its address. A display name
	// equal to the address is noise, so it is omitted rather than duplicated.
	from := jmap.Address{Email: identity.Email}
	if identity.Name != "" && !strings.EqualFold(identity.Name, identity.Email) {
		from.Name = identity.Name
	}

	vendorID, err := s.upstream.SendMessage(ctx, toJMAP(creds), account, jmap.OutgoingEmail{
		DraftsMailboxID: drafts,
		SentMailboxID:   sent,
		IdentityID:      identity.ID,
		From:            from,
		To:              toJMAPAddresses(msg.To),
		Cc:              toJMAPAddresses(msg.Cc),
		Subject:         msg.Subject,
		Text:            msg.Text,
	})
	if err != nil {
		// SendMessage returns the created identifier when the message was made
		// but not submitted. Remove that draft; if the removal fails, the draft
		// stays visible and is logged rather than silently lost.
		if vendorID != "" {
			if destroyErr := s.upstream.DestroyEmails(ctx, toJMAP(creds), account, vendorID); destroyErr != nil {
				s.log.Warn("could not remove a draft whose submission failed", "error", destroyErr)
			}
		}
		return "", mapError(err)
	}
	return ids.Message(account, vendorID), nil
}

// SetFlags sets the stated flags and returns the updated summary. It patches
// only the named keywords, so other attributes survive.
func (s *Service) SetFlags(ctx context.Context, creds Credentials, messageID string, patch FlagPatch) (MessageSummary, error) {
	messageAccount, vendorMessage, err := ids.ParseMessage(messageID)
	if err != nil {
		return MessageSummary{}, ErrNotFound
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return MessageSummary{}, err
	}
	if messageAccount != account {
		return MessageSummary{}, ErrNotFound
	}

	if err := s.upstream.SetEmailKeywords(ctx, toJMAP(creds), account, vendorMessage, patch.Read, patch.Starred); err != nil {
		return MessageSummary{}, mapError(err)
	}

	list, err := s.upstream.GetEmails(ctx, toJMAP(creds), account, jmap.GetEmailsOptions{IDs: []string{vendorMessage}})
	if err != nil {
		return MessageSummary{}, mapError(err)
	}
	if len(list) == 0 {
		return MessageSummary{}, ErrNotFound
	}
	return MapSummary(account, list[0]), nil
}

// chooseIdentity picks the identity that belongs to the authenticated user.
// An exact address match wins; a bare username matches an identity's local
// part; a sole identity is the fallback the pinned release needs. The caller
// can never name a different identity.
func chooseIdentity(identities []jmap.Identity, user string) (jmap.Identity, bool) {
	for _, identity := range identities {
		if identity.ID != "" && strings.EqualFold(identity.Email, user) {
			return identity, true
		}
	}
	local := user
	if at := strings.LastIndex(user, "@"); at > 0 {
		local = user[:at]
	}
	for _, identity := range identities {
		if identity.ID == "" || identity.Email == "" {
			continue
		}
		identityLocal := identity.Email
		if at := strings.LastIndex(identity.Email, "@"); at > 0 {
			identityLocal = identity.Email[:at]
		}
		if strings.EqualFold(identityLocal, local) {
			return identity, true
		}
	}
	if len(identities) == 1 && identities[0].ID != "" {
		return identities[0], true
	}
	return jmap.Identity{}, false
}

// addressDomain returns the lowercased domain of an address, or "" when the
// value is not a usable address.
func addressDomain(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return ""
	}
	if strings.ContainsAny(email, " \t\r\n<>") {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// account resolves the caller's mail account from the session. It is never
// derived from a request parameter.
func (s *Service) account(ctx context.Context, creds Credentials) (string, error) {
	session, err := s.upstream.FetchSession(ctx, toJMAP(creds))
	if err != nil {
		return "", mapError(err)
	}
	account, err := session.MailAccount()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return account, nil
}

func toJMAP(creds Credentials) jmap.Credentials {
	return jmap.Credentials{User: creds.User, Password: creds.Password}
}

func toJMAPAddresses(list []Address) []jmap.Address {
	if len(list) == 0 {
		return nil
	}
	out := make([]jmap.Address, 0, len(list))
	for _, a := range list {
		out = append(out, jmap.Address{Name: a.Name, Email: a.Email})
	}
	return out
}

// mapError converts one failure class into another, keeping the cause for logs
// and giving the HTTP layer a stable class to map to a status and message.
func mapError(err error) error {
	switch {
	case errors.Is(err, jmap.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, jmap.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, jmap.ErrAnchorNotFound):
		return ErrInvalidCursor
	case errors.Is(err, jmap.ErrRejected):
		return fmt.Errorf("%w: %v", ErrRejected, err)
	case errors.Is(err, jmap.ErrTimeout), errors.Is(err, jmap.ErrUnavailable), errors.Is(err, jmap.ErrUpstream):
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	default:
		// Not a mail server failure the adapter can classify, so it is a defect
		// here rather than an outage. Returning it unchanged lets the HTTP layer
		// answer 500 and log the cause, instead of telling the client to retry a
		// request that will keep failing.
		return err
	}
}

// MapMailbox translates one mailbox. Only the standard roles keep their name;
// everything else, including a missing role, is a custom mailbox. The account is
// encoded into the identifier so it only resolves for the caller it was minted
// for.
func MapMailbox(account string, m jmap.Mailbox) Mailbox {
	role := "custom"
	switch m.Role {
	case "inbox", "sent", "drafts", "trash", "junk":
		role = m.Role
	}
	return Mailbox{
		ID:     ids.Mailbox(account, m.ID),
		Name:   m.Name,
		Role:   role,
		Unread: m.UnreadEmails,
		Total:  m.TotalEmails,
	}
}

// MapSummary translates the fields a listing needs.
func MapSummary(account string, e jmap.Email) MessageSummary {
	return MessageSummary{
		ID:            ids.Message(account, e.ID),
		Subject:       e.Subject,
		From:          firstAddress(e.From),
		Received:      e.ReceivedAt,
		Read:          e.Keywords["$seen"],
		Starred:       e.Keywords["$flagged"],
		HasAttachment: e.HasAttachment,
	}
}

// MapMessage translates a message including its text body, capped at maxBytes.
func MapMessage(account string, e jmap.Email, maxBytes int64) Message {
	text, truncated := textBody(e, maxBytes)
	return Message{
		MessageSummary: MapSummary(account, e),
		To:             mapAddresses(e.To),
		Cc:             mapAddresses(e.Cc),
		Text:           text,
		TextTruncated:  truncated,
	}
}

func firstAddress(list []jmap.Address) Address {
	if len(list) == 0 {
		return Address{}
	}
	return Address{Name: list[0].Name, Email: list[0].Email}
}

func mapAddresses(list []jmap.Address) []Address {
	if len(list) == 0 {
		return nil
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: a.Name, Email: a.Email})
	}
	return out
}

// textBody returns the first text/plain part's value, capped at maxBytes. The
// mail server is asked to cap as well; this is the local guarantee.
func textBody(e jmap.Email, maxBytes int64) (string, bool) {
	for _, part := range e.TextBody {
		if part.Type != "text/plain" || part.Disposition == "attachment" {
			continue
		}
		value, ok := e.BodyValues[part.PartID]
		if !ok {
			continue
		}
		if maxBytes > 0 && int64(len(value.Value)) > maxBytes {
			return truncateUTF8(value.Value, maxBytes), true
		}
		return value.Value, value.IsTruncated
	}
	return "", false
}

// truncateUTF8 cuts at a byte limit without leaving a partial rune. It slices
// the string rather than copying it, so a large body is not duplicated just to
// return a short prefix.
func truncateUTF8(s string, maxBytes int64) string {
	if maxBytes <= 0 || int64(len(s)) <= maxBytes {
		return s
	}
	cut := int(maxBytes)
	for cut > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:cut])
		if r != utf8.RuneError || size > 1 {
			break
		}
		cut--
	}
	return s[:cut]
}
