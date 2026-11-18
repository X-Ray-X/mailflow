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
	"unicode"
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
	// ErrAlreadyExists means a mailbox name is already in use. Create is
	// deduplicated by name, so a retry after an unknown outcome answers this
	// instead of making a second mailbox.
	ErrAlreadyExists = errors.New("a mailbox with that name already exists")
	// ErrMailboxNotEmpty means a mailbox still holds messages or child
	// mailboxes, so deleting it would remove more than the caller asked for.
	ErrMailboxNotEmpty = errors.New("mailbox is not empty")
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

// Draft is a caller-supplied draft. Every field is optional, so a client can
// save an unfinished message. There is no sender field: the sender is the
// authenticated account's identity, exactly as for a send.
type Draft struct {
	To      []Address
	Cc      []Address
	Subject string
	Text    string
}

// MessagePatch is a desired-state message update. A nil field is left
// unchanged, so repeating a patch changes nothing. A non-empty MailboxID moves
// the message to exactly that mailbox; it is not a copy.
type MessagePatch struct {
	Read      *bool
	Starred   *bool
	MailboxID string
}

// Writer is the write side of the HTTP contract.
type Writer interface {
	// Send returns the Mailflow identifier of the sent message.
	Send(ctx context.Context, creds Credentials, msg OutgoingMessage) (string, error)
	// CreateDraft saves a draft and returns its Mailflow identifier.
	CreateDraft(ctx context.Context, creds Credentials, draft Draft) (string, error)
	// SendDraft submits an existing draft and returns its Mailflow identifier.
	SendDraft(ctx context.Context, creds Credentials, draftID string) (string, error)
	// Reply creates a draft reply to a message and returns its Mailflow
	// identifier.
	Reply(ctx context.Context, creds Credentials, messageID, text string) (string, error)
	// CreateMailbox returns the Mailflow identifier of the new mailbox.
	CreateMailbox(ctx context.Context, creds Credentials, name string) (string, error)
	// RenameMailbox changes a custom mailbox's name.
	RenameMailbox(ctx context.Context, creds Credentials, mailboxID, name string) error
	// DeleteMailbox removes an empty custom mailbox.
	DeleteMailbox(ctx context.Context, creds Credentials, mailboxID string) error
	// UpdateMessage applies the patch and returns the message's new summary.
	UpdateMessage(ctx context.Context, creds Credentials, messageID string, patch MessagePatch) (MessageSummary, error)
	// DeleteMessage moves the message to trash without erasing it.
	DeleteMessage(ctx context.Context, creds Credentials, messageID string) error
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
	CreateDraft(ctx context.Context, creds jmap.Credentials, accountID string, draft jmap.DraftContent) (string, error)
	SendDraft(ctx context.Context, creds jmap.Credentials, accountID, draftsMailboxID, sentMailboxID, identityID, emailID string, from jmap.Address, recipients []jmap.Address) error
	GetDraft(ctx context.Context, creds jmap.Credentials, accountID, emailID string, maxBodyBytes int64) (jmap.Email, error)
	GetReplySource(ctx context.Context, creds jmap.Credentials, accountID, emailID string) (jmap.Email, error)
	DestroyEmails(ctx context.Context, creds jmap.Credentials, accountID string, ids ...string) error
	UpdateEmail(ctx context.Context, creds jmap.Credentials, accountID, emailID string, patch jmap.EmailPatch) error
	CreateMailbox(ctx context.Context, creds jmap.Credentials, accountID, name string) (string, error)
	RenameMailbox(ctx context.Context, creds jmap.Credentials, accountID, mailboxID, name string) error
	DestroyMailbox(ctx context.Context, creds jmap.Credentials, accountID, mailboxID string) error
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
	account, identity, drafts, sent, domain, err := s.sender(ctx, creds)
	if err != nil {
		return "", err
	}
	for _, recipient := range append(append([]Address{}, msg.To...), msg.Cc...) {
		if !strings.EqualFold(addressDomain(recipient.Email), domain) {
			return "", fmt.Errorf("%w: a recipient is outside the local mail domain", ErrInvalidInput)
		}
	}

	from := senderAddress(identity)

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

// sender resolves everything an outgoing message needs: the account, the
// sending identity, the drafts and sent mailboxes, and the local domain.
// Sending, draft creation, and replies all share it so the sender and the
// domain rule cannot drift between them.
func (s *Service) sender(ctx context.Context, creds Credentials) (account string, identity jmap.Identity, drafts, sent, domain string, err error) {
	account, err = s.account(ctx, creds)
	if err != nil {
		return "", jmap.Identity{}, "", "", "", err
	}

	identities, err := s.upstream.Identities(ctx, toJMAP(creds), account)
	if err != nil {
		return "", jmap.Identity{}, "", "", "", mapError(err)
	}
	identity, ok := chooseIdentity(identities, creds.User)
	if !ok {
		// A permanent condition for this account, not an outage.
		return "", jmap.Identity{}, "", "", "", fmt.Errorf("%w: the account has no sending identity", ErrRejected)
	}

	boxes, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return "", jmap.Identity{}, "", "", "", mapError(err)
	}
	for _, box := range boxes {
		switch box.Role {
		case "drafts":
			drafts = box.ID
		case "sent":
			sent = box.ID
		}
	}
	if drafts == "" || sent == "" {
		return "", jmap.Identity{}, "", "", "", fmt.Errorf("%w: the account has no drafts or sent mailbox", ErrUnavailable)
	}

	domain = addressDomain(identity.Email)
	if domain == "" {
		domain = addressDomain(creds.User)
	}
	if domain == "" {
		return "", jmap.Identity{}, "", "", "", fmt.Errorf("%w: the account has no usable mail domain", ErrRejected)
	}
	return account, identity, drafts, sent, domain, nil
}

// senderAddress is the identity as a message sender. The pinned release names
// the identity after its address; a display name equal to the address is noise,
// so it is omitted rather than duplicated.
func senderAddress(identity jmap.Identity) jmap.Address {
	from := jmap.Address{Email: identity.Email}
	if identity.Name != "" && !strings.EqualFold(identity.Name, identity.Email) {
		from.Name = identity.Name
	}
	return from
}

// CreateDraft saves a plain-text draft as the caller. Every field is optional;
// the server accepts an empty draft, which is what lets a client start one
// before it is complete. Recipients stay inside the caller's domain for the
// same reason a send does: the service must not become a relay, and a draft
// authored here should be sendable unchanged.
func (s *Service) CreateDraft(ctx context.Context, creds Credentials, draft Draft) (string, error) {
	account, identity, drafts, _, domain, err := s.sender(ctx, creds)
	if err != nil {
		return "", err
	}
	for _, recipient := range append(append([]Address{}, draft.To...), draft.Cc...) {
		if !strings.EqualFold(addressDomain(recipient.Email), domain) {
			return "", fmt.Errorf("%w: a recipient is outside the local mail domain", ErrInvalidInput)
		}
	}

	vendorID, err := s.upstream.CreateDraft(ctx, toJMAP(creds), account, jmap.DraftContent{
		DraftsMailboxID: drafts,
		From:            senderAddress(identity),
		To:              toJMAPAddresses(draft.To),
		Cc:              toJMAPAddresses(draft.Cc),
		Subject:         draft.Subject,
		Text:            draft.Text,
	})
	if err != nil {
		return "", mapError(err)
	}
	return ids.Message(account, vendorID), nil
}

// SendDraft submits an existing draft. The draft's own recipients, including a
// Bcc another client set, are read and checked before the mail server is asked
// to submit, so the local-domain rule also covers a draft the service did not
// create. The message keeps its identifier: it leaves drafts for sent rather
// than being re-created.
func (s *Service) SendDraft(ctx context.Context, creds Credentials, draftID string) (string, error) {
	draftAccount, vendorDraft, err := ids.ParseMessage(draftID)
	if err != nil {
		return "", ErrNotFound
	}

	account, identity, drafts, sent, domain, err := s.sender(ctx, creds)
	if err != nil {
		return "", err
	}
	if draftAccount != account {
		return "", ErrNotFound
	}

	draft, err := s.upstream.GetDraft(ctx, toJMAP(creds), account, vendorDraft, s.bodyMaxBytes)
	if err != nil {
		return "", mapError(err)
	}
	if !draft.Keywords["$draft"] || !draft.MailboxIDs[drafts] {
		return "", fmt.Errorf("%w: the message is not a draft", ErrInvalidInput)
	}

	recipients := make([]jmap.Address, 0, len(draft.To)+len(draft.Cc)+len(draft.Bcc))
	recipients = append(recipients, draft.To...)
	recipients = append(recipients, draft.Cc...)
	recipients = append(recipients, draft.Bcc...)
	if len(recipients) == 0 {
		return "", fmt.Errorf("%w: the draft has no recipient", ErrInvalidInput)
	}
	for _, recipient := range recipients {
		if !strings.EqualFold(addressDomain(recipient.Email), domain) {
			return "", fmt.Errorf("%w: a recipient is outside the local mail domain", ErrInvalidInput)
		}
	}

	if err := s.upstream.SendDraft(ctx, toJMAP(creds), account, drafts, sent, identity.ID, vendorDraft, senderAddress(identity), recipients); err != nil {
		return "", mapError(err)
	}
	return ids.Message(account, vendorDraft), nil
}

// Reply composes a draft answer to a message the caller can read. The
// recipient is the original sender, the subject gains one prefix, and the
// threading fields are carried so a standard client groups the reply with the
// conversation. The body is exactly the caller's text: quoting is a product
// decision that stays with the client.
func (s *Service) Reply(ctx context.Context, creds Credentials, messageID, text string) (string, error) {
	messageAccount, vendorMessage, err := ids.ParseMessage(messageID)
	if err != nil {
		return "", ErrNotFound
	}

	account, identity, drafts, _, domain, err := s.sender(ctx, creds)
	if err != nil {
		return "", err
	}
	if messageAccount != account {
		return "", ErrNotFound
	}

	source, err := s.upstream.GetReplySource(ctx, toJMAP(creds), account, vendorMessage)
	if err != nil {
		return "", mapError(err)
	}
	if len(source.From) == 0 || source.From[0].Email == "" {
		return "", fmt.Errorf("%w: the message has no sender to reply to", ErrInvalidInput)
	}
	recipient := source.From[0]
	if !strings.EqualFold(addressDomain(recipient.Email), domain) {
		return "", fmt.Errorf("%w: a recipient is outside the local mail domain", ErrInvalidInput)
	}

	vendorID, err := s.upstream.CreateDraft(ctx, toJMAP(creds), account, jmap.DraftContent{
		DraftsMailboxID: drafts,
		From:            senderAddress(identity),
		To:              []jmap.Address{recipient},
		Subject:         replySubject(source.Subject),
		Text:            text,
		InReplyTo:       source.MessageID,
		References:      replyReferences(source.References, source.MessageID),
	})
	if err != nil {
		return "", mapError(err)
	}
	return ids.Message(account, vendorID), nil
}

// replySubject prefixes a subject once. A subject that already carries the
// prefix is left alone, whatever the case of "re", so a second reply in a
// conversation does not accumulate prefixes.
func replySubject(subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), "re: ") {
		return subject
	}
	return "Re: " + subject
}

// replyReferences appends the original message's identifier to its references
// and removes duplicates. An original with no references still produces one
// reference, which is the minimum a client needs to group the thread.
func replyReferences(references, messageID []string) []string {
	out := make([]string, 0, len(references)+len(messageID))
	seen := make(map[string]bool, len(references)+len(messageID))
	for _, id := range append(append([]string{}, references...), messageID...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// CreateMailbox creates a top-level custom mailbox. The name is validated and
// checked for a case-insensitive collision first, because the public list is
// flat; the server's own duplicate check is the race backstop.
func (s *Service) CreateMailbox(ctx context.Context, creds Credentials, name string) (string, error) {
	name, err := validateMailboxName(name)
	if err != nil {
		return "", err
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return "", err
	}
	boxes, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return "", mapError(err)
	}
	if mailboxNameTaken(boxes, name, "") {
		return "", ErrAlreadyExists
	}

	vendorID, err := s.upstream.CreateMailbox(ctx, toJMAP(creds), account, name)
	if err != nil {
		return "", mapError(err)
	}
	return ids.Mailbox(account, vendorID), nil
}

// RenameMailbox renames a custom mailbox. A standard mailbox is refused even
// though the mail server permits it: clients find inbox, sent, drafts, trash,
// and junk by role, and renaming one would break that.
func (s *Service) RenameMailbox(ctx context.Context, creds Credentials, mailboxID, name string) error {
	mailboxAccount, vendorMailbox, err := ids.ParseMailbox(mailboxID)
	if err != nil {
		return ErrNotFound
	}
	name, err = validateMailboxName(name)
	if err != nil {
		return err
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return err
	}
	if mailboxAccount != account {
		return ErrNotFound
	}

	boxes, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return mapError(err)
	}
	target, ok := findMailbox(boxes, vendorMailbox)
	if !ok {
		return ErrNotFound
	}
	if !isCustomRole(target.Role) {
		return fmt.Errorf("%w: a standard mailbox cannot be renamed", ErrInvalidInput)
	}
	if target.Name == name {
		return nil
	}
	if mailboxNameTaken(boxes, name, vendorMailbox) {
		return ErrAlreadyExists
	}

	if err := s.upstream.RenameMailbox(ctx, toJMAP(creds), account, vendorMailbox, name); err != nil {
		return mapError(err)
	}
	return nil
}

// DeleteMailbox removes an empty custom mailbox. It refuses a standard mailbox,
// a mailbox that holds messages, and a mailbox with children, so nothing is
// removed as a side effect.
func (s *Service) DeleteMailbox(ctx context.Context, creds Credentials, mailboxID string) error {
	mailboxAccount, vendorMailbox, err := ids.ParseMailbox(mailboxID)
	if err != nil {
		return ErrNotFound
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return err
	}
	if mailboxAccount != account {
		return ErrNotFound
	}

	boxes, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return mapError(err)
	}
	target, ok := findMailbox(boxes, vendorMailbox)
	if !ok {
		return ErrNotFound
	}
	if !isCustomRole(target.Role) {
		return fmt.Errorf("%w: a standard mailbox cannot be deleted", ErrInvalidInput)
	}
	if target.TotalEmails > 0 {
		return ErrMailboxNotEmpty
	}
	for _, box := range boxes {
		if box.ParentID == vendorMailbox {
			return ErrMailboxNotEmpty
		}
	}

	if err := s.upstream.DestroyMailbox(ctx, toJMAP(creds), account, vendorMailbox); err != nil {
		return mapError(err)
	}
	return nil
}

// UpdateMessage applies flags and an optional move in one upstream update, then
// returns the message's new summary. The target mailbox must belong to the
// caller; a foreign or unknown identifier is refused before the update.
func (s *Service) UpdateMessage(ctx context.Context, creds Credentials, messageID string, patch MessagePatch) (MessageSummary, error) {
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
	if patch.Read == nil && patch.Starred == nil && patch.MailboxID == "" {
		return MessageSummary{}, fmt.Errorf("%w: at least one of read, starred, or mailboxId is required", ErrInvalidInput)
	}

	var vendorMailbox string
	if patch.MailboxID != "" {
		mailboxAccount, target, err := ids.ParseMailbox(patch.MailboxID)
		if err != nil {
			return MessageSummary{}, ErrNotFound
		}
		if mailboxAccount != account {
			return MessageSummary{}, ErrNotFound
		}
		exists, err := s.upstream.MailboxExists(ctx, toJMAP(creds), account, target)
		if err != nil {
			return MessageSummary{}, mapError(err)
		}
		if !exists {
			return MessageSummary{}, ErrNotFound
		}
		vendorMailbox = target
	}

	if err := s.upstream.UpdateEmail(ctx, toJMAP(creds), account, vendorMessage, jmap.EmailPatch{
		Read:      patch.Read,
		Starred:   patch.Starred,
		MailboxID: vendorMailbox,
	}); err != nil {
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

// DeleteMessage moves a message to the account's trash mailbox. It never
// destroys the message, so a delete is recoverable by moving it back out.
func (s *Service) DeleteMessage(ctx context.Context, creds Credentials, messageID string) error {
	messageAccount, vendorMessage, err := ids.ParseMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	account, err := s.account(ctx, creds)
	if err != nil {
		return err
	}
	if messageAccount != account {
		return ErrNotFound
	}

	boxes, err := s.upstream.Mailboxes(ctx, toJMAP(creds), account)
	if err != nil {
		return mapError(err)
	}
	var trash string
	for _, box := range boxes {
		if box.Role == "trash" {
			trash = box.ID
			break
		}
	}
	if trash == "" {
		return fmt.Errorf("%w: the account has no trash mailbox", ErrUnavailable)
	}

	if err := s.upstream.UpdateEmail(ctx, toJMAP(creds), account, vendorMessage, jmap.EmailPatch{MailboxID: trash}); err != nil {
		return mapError(err)
	}
	return nil
}

// maxMailboxNameBytes is the mail server's advertised limit and the contract's.
const maxMailboxNameBytes = 255

// validateMailboxName trims and checks a caller-supplied name. The rules are
// stricter than the server's: the public list is flat, so a name containing
// the IMAP hierarchy delimiter would be ambiguous to a standard client.
func validateMailboxName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: a mailbox name is required", ErrInvalidInput)
	}
	if len(name) > maxMailboxNameBytes {
		return "", fmt.Errorf("%w: a mailbox name may be at most %d bytes", ErrInvalidInput, maxMailboxNameBytes)
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: a mailbox name must be valid UTF-8", ErrInvalidInput)
	}
	for _, r := range name {
		if r == '/' || unicode.IsControl(r) {
			return "", fmt.Errorf("%w: a mailbox name must not contain '/' or control characters", ErrInvalidInput)
		}
	}
	return name, nil
}

// mailboxNameTaken reports a case-insensitive collision, ignoring the mailbox
// being renamed. The server only rejects an exact duplicate at the same parent,
// so this is the service's own stricter rule.
func mailboxNameTaken(boxes []jmap.Mailbox, name, exceptID string) bool {
	for _, box := range boxes {
		if box.ID == exceptID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(box.Name), name) {
			return true
		}
	}
	return false
}

func findMailbox(boxes []jmap.Mailbox, vendorID string) (jmap.Mailbox, bool) {
	for _, box := range boxes {
		if box.ID == vendorID {
			return box, true
		}
	}
	return jmap.Mailbox{}, false
}

// isCustomRole reports whether a mailbox may be renamed or deleted through the
// service. A role the public model calls custom, including an absent one, is
// allowed; the five standard roles are not.
func isCustomRole(role string) bool {
	switch role {
	case "inbox", "sent", "drafts", "trash", "junk":
		return false
	default:
		return true
	}
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
	case errors.Is(err, jmap.ErrAlreadyExists):
		return ErrAlreadyExists
	case errors.Is(err, jmap.ErrMailboxNotEmpty):
		return ErrMailboxNotEmpty
	case errors.Is(err, jmap.ErrInvalidInput):
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
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
	if !isCustomRole(m.Role) {
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
