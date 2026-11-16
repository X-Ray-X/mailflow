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
	"sort"
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

// Reader is what the HTTP layer depends on.
type Reader interface {
	Mailboxes(ctx context.Context, creds Credentials) ([]Mailbox, error)
	Messages(ctx context.Context, creds Credentials, mailboxID string, page PageRequest) (Page, error)
	Message(ctx context.Context, creds Credentials, messageID string) (Message, error)
}

// upstream is the part of the JMAP client the service uses. It is an interface
// so the translation can be tested without a server.
type upstream interface {
	FetchSession(ctx context.Context, creds jmap.Credentials) (jmap.Session, error)
	Mailboxes(ctx context.Context, creds jmap.Credentials, accountID string) ([]jmap.Mailbox, error)
	MailboxExists(ctx context.Context, creds jmap.Credentials, accountID, mailboxID string) (bool, error)
	QueryEmails(ctx context.Context, creds jmap.Credentials, accountID string, q jmap.Query) (jmap.QueryResult, error)
	GetEmails(ctx context.Context, creds jmap.Credentials, accountID string, opts jmap.GetEmailsOptions) ([]jmap.Email, error)
}

// Service adapts the mail server to the Reader contract.
type Service struct {
	upstream        upstream
	pageSizeDefault int
	pageSizeMax     int
	bodyMaxBytes    int64
}

// NewService wires the adapter with the configured bounds.
func NewService(client upstream, pageSizeDefault, pageSizeMax int, bodyMaxBytes int64) *Service {
	return &Service{
		upstream:        client,
		pageSizeDefault: pageSizeDefault,
		pageSizeMax:     pageSizeMax,
		bodyMaxBytes:    bodyMaxBytes,
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

// mapError converts one failure class into another, keeping the cause for logs
// and giving the HTTP layer a stable class to map to a status and message.
func mapError(err error) error {
	switch {
	case errors.Is(err, jmap.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, jmap.ErrAnchorNotFound):
		return ErrInvalidCursor
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
