package mail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/ids"
	"github.com/X-Ray-X/mailflow/app/internal/jmap"
)

var testCreds = Credentials{User: "bob@example.test", Password: "secret"}

// testAccount is the account the fake session reports, and therefore the
// account every identifier in these tests is minted for.
const testAccount = "acct-1"

// fakeUpstream is a scripted mail server. It records what the service asked
// for so the translation can be asserted without a network.
type fakeUpstream struct {
	session    jmap.Session
	sessionErr error

	mailboxes    []jmap.Mailbox
	mailboxesErr error

	mailboxExists    bool
	mailboxExistsErr error

	query    jmap.QueryResult
	queryErr error

	emails    []jmap.Email
	emailsErr error

	identities    []jmap.Identity
	identitiesErr error

	sendID    string
	sendErr   error
	sent      jmap.OutgoingEmail
	sendCalls int

	draftID    string
	draftErr   error
	draft      jmap.DraftContent
	draftCalls int

	getDraft    jmap.Email
	getDraftErr error
	getDraftID  string

	sendDraftErr        error
	sendDraftCalls      int
	sendDraftID         string
	sendDraftDrafts     string
	sendDraftSent       string
	sendDraftIdentity   string
	sendDraftFrom       jmap.Address
	sendDraftRecipients []jmap.Address

	replySource    jmap.Email
	replySourceErr error
	replySourceID  string

	attachments    []jmap.Attachment
	attachmentsErr error
	attachmentID   string

	downloadBody       io.ReadCloser
	downloadErr        error
	downloadAttachment jmap.Attachment
	downloadAccount    string
	downloadCalls      int

	destroyed  []string
	destroyErr error

	updateID    string
	updatePatch jmap.EmailPatch
	updateErr   error

	createdMailboxName string
	createdMailboxID   string
	createMailboxErr   error

	renamedMailboxID   string
	renamedMailboxName string
	renameMailboxErr   error

	destroyedMailboxID string
	destroyMailboxErr  error

	gotAccount     string
	gotMailboxID   string
	gotQuery       jmap.Query
	gotGet         jmap.GetEmailsOptions
	sessionCalls   int
	mailboxQueries int
	queryCalls     int
	getCalls       int
}

func (f *fakeUpstream) FetchSession(context.Context, jmap.Credentials) (jmap.Session, error) {
	f.sessionCalls++
	if f.sessionErr != nil {
		return jmap.Session{}, f.sessionErr
	}
	if f.session.PrimaryAccounts == nil {
		return jmap.Session{PrimaryAccounts: map[string]string{jmap.CapabilityMail: "acct-1"}}, nil
	}
	return f.session, nil
}

func (f *fakeUpstream) Mailboxes(_ context.Context, _ jmap.Credentials, accountID string) ([]jmap.Mailbox, error) {
	f.gotAccount = accountID
	return f.mailboxes, f.mailboxesErr
}

func (f *fakeUpstream) MailboxExists(_ context.Context, _ jmap.Credentials, accountID, mailboxID string) (bool, error) {
	f.gotAccount = accountID
	f.gotMailboxID = mailboxID
	f.mailboxQueries++
	return f.mailboxExists, f.mailboxExistsErr
}

func (f *fakeUpstream) QueryEmails(_ context.Context, _ jmap.Credentials, accountID string, q jmap.Query) (jmap.QueryResult, error) {
	f.gotAccount = accountID
	f.gotQuery = q
	f.queryCalls++
	return f.query, f.queryErr
}

func (f *fakeUpstream) GetEmails(_ context.Context, _ jmap.Credentials, accountID string, opts jmap.GetEmailsOptions) ([]jmap.Email, error) {
	f.gotAccount = accountID
	f.gotGet = opts
	f.getCalls++
	return f.emails, f.emailsErr
}

func (f *fakeUpstream) Identities(_ context.Context, _ jmap.Credentials, accountID string) ([]jmap.Identity, error) {
	f.gotAccount = accountID
	return f.identities, f.identitiesErr
}

func (f *fakeUpstream) SendMessage(_ context.Context, _ jmap.Credentials, accountID string, msg jmap.OutgoingEmail) (string, error) {
	f.gotAccount = accountID
	f.sent = msg
	f.sendCalls++
	return f.sendID, f.sendErr
}

func (f *fakeUpstream) CreateDraft(_ context.Context, _ jmap.Credentials, accountID string, draft jmap.DraftContent) (string, error) {
	f.gotAccount = accountID
	f.draft = draft
	f.draftCalls++
	return f.draftID, f.draftErr
}

func (f *fakeUpstream) SendDraft(_ context.Context, _ jmap.Credentials, accountID, draftsMailboxID, sentMailboxID, identityID, emailID string, from jmap.Address, recipients []jmap.Address) error {
	f.gotAccount = accountID
	f.sendDraftCalls++
	f.sendDraftID = emailID
	f.sendDraftDrafts = draftsMailboxID
	f.sendDraftSent = sentMailboxID
	f.sendDraftIdentity = identityID
	f.sendDraftFrom = from
	f.sendDraftRecipients = recipients
	return f.sendDraftErr
}

func (f *fakeUpstream) GetDraft(_ context.Context, _ jmap.Credentials, accountID, emailID string, _ int64) (jmap.Email, error) {
	f.gotAccount = accountID
	f.getDraftID = emailID
	return f.getDraft, f.getDraftErr
}

func (f *fakeUpstream) GetReplySource(_ context.Context, _ jmap.Credentials, accountID, emailID string) (jmap.Email, error) {
	f.gotAccount = accountID
	f.replySourceID = emailID
	return f.replySource, f.replySourceErr
}

func (f *fakeUpstream) GetAttachments(_ context.Context, _ jmap.Credentials, accountID, emailID string) ([]jmap.Attachment, error) {
	f.gotAccount = accountID
	f.attachmentID = emailID
	return f.attachments, f.attachmentsErr
}

func (f *fakeUpstream) DownloadAttachment(_ context.Context, _ jmap.Credentials, accountID string, att jmap.Attachment) (io.ReadCloser, error) {
	f.downloadAccount = accountID
	f.downloadAttachment = att
	f.downloadCalls++
	return f.downloadBody, f.downloadErr
}

func (f *fakeUpstream) DestroyEmails(_ context.Context, _ jmap.Credentials, accountID string, ids ...string) error {
	f.gotAccount = accountID
	f.destroyed = append(f.destroyed, ids...)
	return f.destroyErr
}

func (f *fakeUpstream) UpdateEmail(_ context.Context, _ jmap.Credentials, accountID, emailID string, patch jmap.EmailPatch) error {
	f.gotAccount = accountID
	f.updateID = emailID
	f.updatePatch = patch
	return f.updateErr
}

func (f *fakeUpstream) CreateMailbox(_ context.Context, _ jmap.Credentials, accountID, name string) (string, error) {
	f.gotAccount = accountID
	f.createdMailboxName = name
	return f.createdMailboxID, f.createMailboxErr
}

func (f *fakeUpstream) RenameMailbox(_ context.Context, _ jmap.Credentials, accountID, mailboxID, name string) error {
	f.gotAccount = accountID
	f.renamedMailboxID = mailboxID
	f.renamedMailboxName = name
	return f.renameMailboxErr
}

func (f *fakeUpstream) DestroyMailbox(_ context.Context, _ jmap.Credentials, accountID, mailboxID string) error {
	f.gotAccount = accountID
	f.destroyedMailboxID = mailboxID
	return f.destroyMailboxErr
}

func newTestService(f *fakeUpstream) *Service {
	return NewService(f, 25, 100, 1024, nil)
}

func TestMapMailboxRoles(t *testing.T) {
	cases := map[string]string{
		"inbox":   "inbox",
		"sent":    "sent",
		"drafts":  "drafts",
		"trash":   "trash",
		"junk":    "junk",
		"none":    "custom",
		"":        "custom",
		"archive": "custom",
	}
	for vendorRole, want := range cases {
		got := MapMailbox(testAccount, jmap.Mailbox{ID: "m1", Role: vendorRole, Name: "Box"}).Role
		if got != want {
			t.Errorf("role %q mapped to %q, want %q", vendorRole, got, want)
		}
	}
}

func TestMapMailboxUsesMailflowIdentifierAndCounts(t *testing.T) {
	got := MapMailbox(testAccount, jmap.Mailbox{ID: "bmaaaaal", Name: "Inbox", Role: "inbox", UnreadEmails: 3, TotalEmails: 9})
	if got.ID == "bmaaaaal" {
		t.Fatal("identifier is the mail server's")
	}
	if !strings.HasPrefix(got.ID, "mb1_") {
		t.Fatalf("identifier %q lacks the mailbox prefix", got.ID)
	}
	if account, back, err := ids.ParseMailbox(got.ID); err != nil || account != testAccount || back != "bmaaaaal" {
		t.Fatalf("identifier does not round trip: account %q object %q error %v", account, back, err)
	}
	if got.Unread != 3 || got.Total != 9 {
		t.Fatalf("counts = %d/%d", got.Unread, got.Total)
	}
}

func TestMapSummaryTranslatesFlags(t *testing.T) {
	received := time.Date(2026, 11, 15, 21, 0, 0, 0, time.UTC)
	got := MapSummary(testAccount, jmap.Email{
		ID:            "e1",
		Subject:       "Hello",
		From:          []jmap.Address{{Name: "Alice", Email: "alice@example.test"}, {Name: "Other", Email: "o@example.test"}},
		ReceivedAt:    received,
		Keywords:      map[string]bool{"$seen": true, "$flagged": true, "$draft": false},
		HasAttachment: true,
	})
	if !got.Read || !got.Starred {
		t.Fatalf("flags = read %v starred %v", got.Read, got.Starred)
	}
	if !got.HasAttachment {
		t.Fatal("attachment flag lost")
	}
	if got.From.Email != "alice@example.test" || got.From.Name != "Alice" {
		t.Fatalf("from = %+v", got.From)
	}
	if !got.Received.Equal(received) {
		t.Fatalf("received = %v", got.Received)
	}
	if got.ID == "e1" || !strings.HasPrefix(got.ID, "msg1_") {
		t.Fatalf("identifier = %q", got.ID)
	}
}

func TestMapSummaryWithoutKeywords(t *testing.T) {
	got := MapSummary(testAccount, jmap.Email{ID: "e1"})
	if got.Read || got.Starred || got.HasAttachment {
		t.Fatalf("empty keywords produced %+v", got)
	}
	if got.From != (Address{}) {
		t.Fatalf("from = %+v, want zero", got.From)
	}
}

func TestMapMessageBody(t *testing.T) {
	e := jmap.Email{
		ID: "e1",
		TextBody: []jmap.BodyPart{
			{PartID: "html", Type: "text/html"},
			{PartID: "plain", Type: "text/plain"},
		},
		BodyValues: map[string]jmap.BodyValue{
			"html":  {Value: "<p>hi</p>"},
			"plain": {Value: "hi"},
		},
		To: []jmap.Address{{Name: "Bob", Email: "bob@example.test"}},
	}
	got := MapMessage(testAccount, e, 1024)
	if got.Text != "hi" {
		t.Fatalf("text = %q, want the text/plain part", got.Text)
	}
	if got.TextTruncated {
		t.Fatal("short body reported as truncated")
	}
	if len(got.To) != 1 || got.To[0].Email != "bob@example.test" {
		t.Fatalf("to = %+v", got.To)
	}
}

func TestMapMessageCapsBody(t *testing.T) {
	e := jmap.Email{
		ID:         "e1",
		TextBody:   []jmap.BodyPart{{PartID: "1", Type: "text/plain"}},
		BodyValues: map[string]jmap.BodyValue{"1": {Value: strings.Repeat("a", 100)}},
	}
	got := MapMessage(testAccount, e, 10)
	if got.Text != strings.Repeat("a", 10) {
		t.Fatalf("text length = %d, want 10", len(got.Text))
	}
	if !got.TextTruncated {
		t.Fatal("capped body did not report truncation")
	}
}

func TestMapMessageKeepsServerTruncation(t *testing.T) {
	e := jmap.Email{
		ID:         "e1",
		TextBody:   []jmap.BodyPart{{PartID: "1", Type: "text/plain"}},
		BodyValues: map[string]jmap.BodyValue{"1": {Value: "short", IsTruncated: true}},
	}
	if got := MapMessage(testAccount, e, 1024); !got.TextTruncated {
		t.Fatal("server-side truncation was dropped")
	}
}

func TestMapMessageWithoutTextPart(t *testing.T) {
	e := jmap.Email{ID: "e1", TextBody: []jmap.BodyPart{{PartID: "1", Type: "text/html"}},
		BodyValues: map[string]jmap.BodyValue{"1": {Value: "<p>x</p>"}}}
	got := MapMessage(testAccount, e, 1024)
	if got.Text != "" || got.TextTruncated {
		t.Fatalf("got %q truncated=%v, want an empty body", got.Text, got.TextTruncated)
	}
}

func TestTruncateUTF8DoesNotSplitRunes(t *testing.T) {
	// "é" is two bytes; a cap of 3 lands in the middle of the second rune.
	got := truncateUTF8("aéé", 3)
	if got != "aé" {
		t.Fatalf("truncated = %q, want %q", got, "aé")
	}
	if !strings.HasPrefix("aéé", got) {
		t.Fatalf("%q is not a prefix of the original", got)
	}
}

func TestTruncateUTF8LeavesFittingStringsAlone(t *testing.T) {
	const s = "aé"
	if got := truncateUTF8(s, 1024); got != s {
		t.Fatalf("got %q, want the whole string", got)
	}
	// A non-positive cap means "no limit"; the configured value is always
	// positive, so this only guards the helper.
	if got := truncateUTF8(s, 0); got != s {
		t.Fatalf("zero cap returned %q, want the whole string", got)
	}
}

func TestServiceMailboxesOrdersAndMaps(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{
		{ID: "m2", Name: "Receipts", Role: "none", SortOrder: 20, TotalEmails: 2},
		{ID: "m1", Name: "Inbox", Role: "inbox", SortOrder: 10, UnreadEmails: 4},
	}}
	got, err := newTestService(f).Mailboxes(context.Background(), testCreds)
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d mailboxes", len(got))
	}
	if got[0].Name != "Inbox" || got[1].Name != "Receipts" {
		t.Fatalf("order = %q, %q", got[0].Name, got[1].Name)
	}
	if got[0].Role != "inbox" || got[1].Role != "custom" {
		t.Fatalf("roles = %q, %q", got[0].Role, got[1].Role)
	}
	if f.gotAccount != "acct-1" {
		t.Fatalf("account = %q, want the session's account", f.gotAccount)
	}
}

func TestServiceMapsUnauthorized(t *testing.T) {
	f := &fakeUpstream{sessionErr: jmap.ErrUnauthorized}
	_, err := newTestService(f).Mailboxes(context.Background(), testCreds)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
}

func TestServiceMapsTimeoutToUnavailable(t *testing.T) {
	f := &fakeUpstream{sessionErr: jmap.ErrTimeout}
	_, err := newTestService(f).Mailboxes(context.Background(), testCreds)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

// An error the adapter cannot classify is a defect here, not an outage. It must
// not be dressed up as service-unavailable, which would tell a client to retry a
// request that will keep failing.
func TestServiceDoesNotDisguiseUnknownErrorsAsUnavailable(t *testing.T) {
	f := &fakeUpstream{sessionErr: errors.New("unexpected adapter state")}
	_, err := newTestService(f).Mailboxes(context.Background(), testCreds)
	if err == nil {
		t.Fatal("Mailboxes swallowed the error")
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to keep its own class", err)
	}
	if !strings.Contains(err.Error(), "unexpected adapter state") {
		t.Fatalf("error = %v, want the cause preserved for the log", err)
	}
}

func TestServiceMessagesUnknownMailbox(t *testing.T) {
	f := &fakeUpstream{mailboxExists: false}
	_, err := newTestService(f).Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.queryCalls != 0 {
		t.Fatal("queried a mailbox that does not exist")
	}
}

func TestServiceMessagesRejectsMalformedMailbox(t *testing.T) {
	f := &fakeUpstream{}
	_, err := newTestService(f).Messages(context.Background(), testCreds, "not-an-id", PageRequest{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.sessionCalls != 0 {
		t.Fatal("touched the mail server for an identifier that cannot exist")
	}
}

func TestServiceMessagesClampsPageSize(t *testing.T) {
	f := &fakeUpstream{mailboxExists: true, query: jmap.QueryResult{}}
	svc := newTestService(f)

	if _, err := svc.Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{Limit: 500}); err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if f.gotQuery.Limit != 100 {
		t.Fatalf("limit = %d, want the maximum 100", f.gotQuery.Limit)
	}

	if _, err := svc.Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{}); err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if f.gotQuery.Limit != 25 {
		t.Fatalf("limit = %d, want the default 25", f.gotQuery.Limit)
	}
}

func TestServiceMessagesPaginates(t *testing.T) {
	f := &fakeUpstream{
		mailboxExists: true,
		query:         jmap.QueryResult{IDs: []string{"e3", "e2"}, Total: 4, Position: 0},
		emails: []jmap.Email{
			{ID: "e2", Subject: "two", ReceivedAt: time.Unix(2, 0)},
			{ID: "e3", Subject: "three", ReceivedAt: time.Unix(3, 0)},
		},
	}
	svc := newTestService(f)

	page, err := svc.Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{Limit: 2})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if !page.HasMore {
		t.Fatal("page one reports no more messages")
	}
	if page.NextCursor == "" {
		t.Fatal("page one has no cursor")
	}
	if f.gotQuery.Anchor != "" {
		t.Fatalf("first page sent anchor %q", f.gotQuery.Anchor)
	}
	if len(page.Messages) != 2 || page.Messages[0].Subject != "three" || page.Messages[1].Subject != "two" {
		t.Fatalf("order = %+v, want query order", page.Messages)
	}

	cursorAccount, anchor, err := ids.ParseCursor(page.NextCursor)
	if err != nil {
		t.Fatalf("cursor does not parse: %v", err)
	}
	if cursorAccount != testAccount {
		t.Fatalf("cursor account = %q, want %q", cursorAccount, testAccount)
	}
	if anchor != "e2" {
		t.Fatalf("cursor points at %q, want the last returned message e2", anchor)
	}

	f.query = jmap.QueryResult{IDs: []string{"e1"}, Total: 4, Position: 3}
	page2, err := svc.Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if f.gotQuery.Anchor != "e2" {
		t.Fatalf("second page anchor = %q", f.gotQuery.Anchor)
	}
	if page2.HasMore {
		t.Fatal("last page reports more messages")
	}
	if page2.NextCursor != "" {
		t.Fatalf("last page has cursor %q", page2.NextCursor)
	}
}

func TestServiceMessagesRejectsInvalidCursor(t *testing.T) {
	f := &fakeUpstream{}
	_, err := newTestService(f).Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{Cursor: "nonsense"})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("error = %v, want ErrInvalidCursor", err)
	}
	if f.sessionCalls != 0 {
		t.Fatal("touched the mail server for a malformed cursor")
	}
}

// The mail server reuses raw identifiers across accounts. An identifier minted
// for another account must be refused before it can be resolved against this
// caller's objects, which would otherwise return a different object.
func TestServiceMessagesRejectsIdentifierFromAnotherAccount(t *testing.T) {
	f := &fakeUpstream{mailboxExists: true}
	_, err := newTestService(f).Messages(context.Background(), testCreds, ids.Mailbox("other-account", "m1"), PageRequest{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.mailboxQueries != 0 {
		t.Fatal("asked the mail server about a mailbox identifier from another account")
	}
	if f.queryCalls != 0 {
		t.Fatal("queried a mailbox identifier from another account")
	}
}

// A message deleted between the query and the fetch must not leave the caller
// with hasMore set and no cursor to follow.
func TestServiceMessagesKeepsCursorWhenTheLastMessageVanishes(t *testing.T) {
	f := &fakeUpstream{
		mailboxExists: true,
		query:         jmap.QueryResult{IDs: []string{"e3", "e2"}, Total: 4, Position: 0},
		emails:        []jmap.Email{{ID: "e3", Subject: "three"}},
	}
	page, err := newTestService(f).Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{Limit: 2})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if !page.HasMore {
		t.Fatal("page reports no more messages")
	}
	if len(page.Messages) != 1 || page.Messages[0].Subject != "three" {
		t.Fatalf("messages = %+v, want only the message that could be fetched", page.Messages)
	}
	if page.NextCursor == "" {
		t.Fatal("hasMore is set but no cursor was issued")
	}
	_, anchor, err := ids.ParseCursor(page.NextCursor)
	if err != nil {
		t.Fatalf("cursor does not parse: %v", err)
	}
	if anchor != "e2" {
		t.Fatalf("cursor anchors on %q, want the query's last identifier e2", anchor)
	}
}

func TestServiceMessagesRejectsCursorFromAnotherAccount(t *testing.T) {
	f := &fakeUpstream{mailboxExists: true}
	_, err := newTestService(f).Messages(context.Background(), testCreds,
		ids.Mailbox(testAccount, "m1"), PageRequest{Cursor: ids.Cursor("other-account", "e1")})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("error = %v, want ErrInvalidCursor", err)
	}
	if f.queryCalls != 0 {
		t.Fatal("queried with a cursor from another account")
	}
}

func TestServiceMessageRejectsIdentifierFromAnotherAccount(t *testing.T) {
	f := &fakeUpstream{emails: []jmap.Email{{ID: "e1"}}}
	_, err := newTestService(f).Message(context.Background(), testCreds, ids.Message("other-account", "e1"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.getCalls != 0 {
		t.Fatal("asked the mail server for a message identifier from another account")
	}
}

func TestServiceMessagesDeletedAnchor(t *testing.T) {
	f := &fakeUpstream{mailboxExists: true, queryErr: jmap.ErrAnchorNotFound}
	_, err := newTestService(f).Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"),
		PageRequest{Cursor: ids.Cursor(testAccount, "gone")})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("error = %v, want ErrInvalidCursor", err)
	}
}

func TestServiceMessagesEmptyMailbox(t *testing.T) {
	f := &fakeUpstream{mailboxExists: true, query: jmap.QueryResult{IDs: nil, Total: 0}}
	page, err := newTestService(f).Messages(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"), PageRequest{})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if page.Messages == nil {
		t.Fatal("messages is nil; the contract always returns an array")
	}
	if len(page.Messages) != 0 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("page = %+v", page)
	}
}

func TestServiceMessageCapsAndMaps(t *testing.T) {
	f := &fakeUpstream{emails: []jmap.Email{{
		ID:         "e1",
		Subject:    "Hello",
		TextBody:   []jmap.BodyPart{{PartID: "1", Type: "text/plain"}},
		BodyValues: map[string]jmap.BodyValue{"1": {Value: strings.Repeat("x", 50)}},
	}}}
	svc := NewService(f, 25, 100, 8, nil)

	got, err := svc.Message(context.Background(), testCreds, ids.Message(testAccount, "e1"))
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if len(got.Text) != 8 || !got.TextTruncated {
		t.Fatalf("text = %q truncated=%v", got.Text, got.TextTruncated)
	}
	if !f.gotGet.IncludeBody || f.gotGet.MaxBodyBytes != 8 {
		t.Fatalf("get options = %+v", f.gotGet)
	}
	if got.ID == "e1" {
		t.Fatal("message identifier is the mail server's")
	}
}

func TestServiceMessageNotFound(t *testing.T) {
	f := &fakeUpstream{emails: nil}
	if _, err := newTestService(f).Message(context.Background(), testCreds, ids.Message(testAccount, "gone")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	f2 := &fakeUpstream{}
	if _, err := newTestService(f2).Message(context.Background(), testCreds, "msg1_!!"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed identifier error = %v, want ErrNotFound", err)
	}
	if f2.sessionCalls != 0 {
		t.Fatal("touched the mail server for a malformed identifier")
	}
}

func boolPtr(v bool) *bool { return &v }

func sendFixture() *fakeUpstream {
	return &fakeUpstream{
		mailboxes: []jmap.Mailbox{
			{ID: "d", Role: "drafts", Name: "Drafts"},
			{ID: "e", Role: "sent", Name: "Sent"},
		},
		identities: []jmap.Identity{{ID: "id1", Name: "Bob", Email: testCreds.User}},
		sendID:     "e1",
	}
}

func TestServiceSendUsesIdentityAndMailboxes(t *testing.T) {
	f := sendFixture()
	got, err := newTestService(f).Send(context.Background(), testCreds, OutgoingMessage{
		To:      []Address{{Name: "Alice", Email: "alice@example.test"}},
		Cc:      []Address{{Email: "carol@example.test"}},
		Subject: "Lunch",
		Text:    "Noon?",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	account, vendor, err := ids.ParseMessage(got)
	if err != nil || account != testAccount || vendor != "e1" {
		t.Fatalf("identifier = %q (account %q vendor %q error %v)", got, account, vendor, err)
	}
	if f.sent.DraftsMailboxID != "d" || f.sent.SentMailboxID != "e" {
		t.Fatalf("mailboxes = %+v", f.sent)
	}
	if f.sent.IdentityID != "id1" || f.sent.From.Email != testCreds.User {
		t.Fatalf("sender = %+v", f.sent)
	}
	if len(f.sent.To) != 1 || f.sent.To[0].Email != "alice@example.test" {
		t.Fatalf("to = %+v", f.sent.To)
	}
	if len(f.sent.Cc) != 1 || f.sent.Cc[0].Email != "carol@example.test" {
		t.Fatalf("cc = %+v", f.sent.Cc)
	}
	if f.sent.Subject != "Lunch" || f.sent.Text != "Noon?" {
		t.Fatalf("message = %+v", f.sent)
	}
}

func TestServiceSendRefusesRecipientOutsideTheDomain(t *testing.T) {
	f := sendFixture()
	_, err := newTestService(f).Send(context.Background(), testCreds, OutgoingMessage{
		To:   []Address{{Email: "someone@elsewhere.test"}},
		Text: "hi",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.sendCalls != 0 {
		t.Fatal("submitted a message to a foreign domain")
	}
}

func TestServiceSendWithoutIdentityIsRejected(t *testing.T) {
	f := sendFixture()
	f.identities = nil
	_, err := newTestService(f).Send(context.Background(), testCreds, OutgoingMessage{
		To: []Address{{Email: "alice@example.test"}},
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error = %v, want ErrRejected", err)
	}
	if f.sendCalls != 0 {
		t.Fatal("submitted without a sending identity")
	}
}

func TestServiceSendCompensatesAFailedSubmission(t *testing.T) {
	f := sendFixture()
	f.sendErr = fmt.Errorf("%w: submission refused", jmap.ErrRejected)
	_, err := newTestService(f).Send(context.Background(), testCreds, OutgoingMessage{
		To:   []Address{{Email: "alice@example.test"}},
		Text: "hi",
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error = %v, want ErrRejected", err)
	}
	if len(f.destroyed) != 1 || f.destroyed[0] != "e1" {
		t.Fatalf("destroyed = %v, want the created draft", f.destroyed)
	}
}

func TestServiceSendKeepsTheDraftWhenCompensationFails(t *testing.T) {
	f := sendFixture()
	f.sendErr = fmt.Errorf("%w: submission refused", jmap.ErrRejected)
	f.destroyErr = errors.New("destroy failed")
	_, err := newTestService(f).Send(context.Background(), testCreds, OutgoingMessage{
		To:   []Address{{Email: "alice@example.test"}},
		Text: "hi",
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error = %v, want ErrRejected", err)
	}
	if len(f.destroyed) != 1 {
		t.Fatalf("compensation was not attempted: %v", f.destroyed)
	}
}

func TestServiceSendChoosesTheMatchingIdentity(t *testing.T) {
	f := sendFixture()
	f.identities = []jmap.Identity{
		{ID: "other", Email: "someone@example.test"},
		{ID: "id1", Email: testCreds.User},
	}
	if _, err := newTestService(f).Send(context.Background(), testCreds, OutgoingMessage{
		To: []Address{{Email: "alice@example.test"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if f.sent.IdentityID != "id1" {
		t.Fatalf("identity = %q, want the caller's", f.sent.IdentityID)
	}
}

func TestServiceCreateDraftUsesIdentityAndMailbox(t *testing.T) {
	f := sendFixture()
	f.draftID = "d1"
	got, err := newTestService(f).CreateDraft(context.Background(), testCreds, Draft{
		To:      []Address{{Name: "Alice", Email: "alice@example.test"}},
		Cc:      []Address{{Email: "carol@example.test"}},
		Subject: "Lunch",
		Text:    "Noon?",
	})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	account, vendor, err := ids.ParseMessage(got)
	if err != nil || account != testAccount || vendor != "d1" {
		t.Fatalf("identifier = %q (account %q vendor %q error %v)", got, account, vendor, err)
	}
	if f.draft.DraftsMailboxID != "d" {
		t.Fatalf("drafts mailbox = %q", f.draft.DraftsMailboxID)
	}
	if f.draft.From.Email != testCreds.User || f.draft.From.Name != "Bob" {
		t.Fatalf("sender = %+v", f.draft.From)
	}
	if len(f.draft.To) != 1 || f.draft.To[0].Email != "alice@example.test" {
		t.Fatalf("to = %+v", f.draft.To)
	}
	if len(f.draft.Cc) != 1 || f.draft.Cc[0].Email != "carol@example.test" {
		t.Fatalf("cc = %+v", f.draft.Cc)
	}
	if f.draft.Subject != "Lunch" || f.draft.Text != "Noon?" {
		t.Fatalf("content = %+v", f.draft)
	}
	if len(f.draft.InReplyTo) != 0 || len(f.draft.References) != 0 {
		t.Fatalf("a plain draft carries threading: %+v", f.draft)
	}
}

func TestServiceCreateDraftAllowsAnEmptyDraft(t *testing.T) {
	f := sendFixture()
	f.draftID = "d1"
	if _, err := newTestService(f).CreateDraft(context.Background(), testCreds, Draft{}); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if f.draftCalls != 1 {
		t.Fatal("an empty draft was not saved")
	}
	if len(f.draft.To) != 0 || len(f.draft.Cc) != 0 || f.draft.Subject != "" || f.draft.Text != "" {
		t.Fatalf("empty draft = %+v", f.draft)
	}
}

func TestServiceCreateDraftRefusesRecipientOutsideTheDomain(t *testing.T) {
	f := sendFixture()
	_, err := newTestService(f).CreateDraft(context.Background(), testCreds, Draft{
		To: []Address{{Email: "someone@elsewhere.test"}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.draftCalls != 0 {
		t.Fatal("saved a draft with a foreign recipient")
	}
}

func TestServiceCreateDraftRefusesCcOutsideTheDomain(t *testing.T) {
	f := sendFixture()
	_, err := newTestService(f).CreateDraft(context.Background(), testCreds, Draft{
		Cc: []Address{{Email: "someone@elsewhere.test"}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.draftCalls != 0 {
		t.Fatal("saved a draft with a foreign recipient")
	}
}

func TestServiceSendDraftSubmitsTheExistingMessage(t *testing.T) {
	f := sendFixture()
	f.getDraft = jmap.Email{
		ID:         "d1",
		Keywords:   map[string]bool{"$draft": true},
		MailboxIDs: map[string]bool{"d": true},
		To:         []jmap.Address{{Email: "alice@example.test"}},
		Cc:         []jmap.Address{{Email: "carol@example.test"}},
		Bcc:        []jmap.Address{{Email: "dave@example.test"}},
	}
	got, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message(testAccount, "d1"))
	if err != nil {
		t.Fatalf("SendDraft: %v", err)
	}
	account, vendor, err := ids.ParseMessage(got)
	if err != nil || account != testAccount || vendor != "d1" {
		t.Fatalf("identifier = %q (account %q vendor %q error %v), want it unchanged", got, account, vendor, err)
	}
	if f.getDraftID != "d1" || f.sendDraftID != "d1" {
		t.Fatalf("read %q, submitted %q", f.getDraftID, f.sendDraftID)
	}
	if f.sendDraftDrafts != "d" || f.sendDraftSent != "e" || f.sendDraftIdentity != "id1" {
		t.Fatalf("submission = %+v", f)
	}
	if f.sendDraftFrom.Email != testCreds.User {
		t.Fatalf("envelope sender = %+v", f.sendDraftFrom)
	}
	emails := make([]string, 0, len(f.sendDraftRecipients))
	for _, recipient := range f.sendDraftRecipients {
		emails = append(emails, recipient.Email)
	}
	want := []string{"alice@example.test", "carol@example.test", "dave@example.test"}
	if strings.Join(emails, ",") != strings.Join(want, ",") {
		t.Fatalf("envelope recipients = %v, want %v", emails, want)
	}
	if len(f.destroyed) != 0 {
		t.Fatalf("a submitted draft was destroyed: %v", f.destroyed)
	}
}

func TestServiceSendDraftRefusesAMessageThatIsNotADraft(t *testing.T) {
	f := sendFixture()
	f.getDraft = jmap.Email{ID: "d1", MailboxIDs: map[string]bool{"e": true}}
	_, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message(testAccount, "d1"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.sendDraftCalls != 0 {
		t.Fatal("submitted a message that is not a draft")
	}
}

func TestServiceSendDraftRefusesADraftWithoutRecipients(t *testing.T) {
	f := sendFixture()
	f.getDraft = jmap.Email{
		ID:         "d1",
		Keywords:   map[string]bool{"$draft": true},
		MailboxIDs: map[string]bool{"d": true},
	}
	_, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message(testAccount, "d1"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.sendDraftCalls != 0 {
		t.Fatal("submitted a draft with no recipient")
	}
}

func TestServiceSendDraftRefusesRecipientOutsideTheDomain(t *testing.T) {
	f := sendFixture()
	f.getDraft = jmap.Email{
		ID:         "d1",
		Keywords:   map[string]bool{"$draft": true},
		MailboxIDs: map[string]bool{"d": true},
		To:         []jmap.Address{{Email: "someone@elsewhere.test"}},
	}
	_, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message(testAccount, "d1"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.sendDraftCalls != 0 {
		t.Fatal("submitted a draft to a foreign domain")
	}
}

func TestServiceSendDraftKeepsTheDraftWhenSubmissionIsDeclined(t *testing.T) {
	f := sendFixture()
	f.getDraft = jmap.Email{
		ID:         "d1",
		Keywords:   map[string]bool{"$draft": true},
		MailboxIDs: map[string]bool{"d": true},
		To:         []jmap.Address{{Email: "alice@example.test"}},
	}
	f.sendDraftErr = fmt.Errorf("%w: submission refused", jmap.ErrRejected)
	_, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message(testAccount, "d1"))
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error = %v, want ErrRejected", err)
	}
	if len(f.destroyed) != 0 {
		t.Fatalf("a declined draft was destroyed: %v", f.destroyed)
	}
}

func TestServiceSendDraftRefusesAForeignDraft(t *testing.T) {
	f := sendFixture()
	_, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message("another-account", "d1"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.getDraftID != "" || f.sendDraftCalls != 0 {
		t.Fatal("a foreign identifier reached the mail server")
	}
}

func TestServiceSendDraftRefusesAMalformedIdentifier(t *testing.T) {
	f := sendFixture()
	if _, err := newTestService(f).SendDraft(context.Background(), testCreds, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.sessionCalls != 0 {
		t.Fatal("touched the mail server for a malformed identifier")
	}
}

func TestServiceSendDraftReportsAnUnknownDraft(t *testing.T) {
	f := sendFixture()
	f.getDraftErr = jmap.ErrNotFound
	if _, err := newTestService(f).SendDraft(context.Background(), testCreds, ids.Message(testAccount, "gone")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceReplyCarriesThreadingAndPrefix(t *testing.T) {
	f := sendFixture()
	f.draftID = "r1"
	f.replySource = jmap.Email{
		ID:         "e1",
		Subject:    "Lunch",
		From:       []jmap.Address{{Name: "Alice", Email: "alice@example.test"}},
		MessageID:  []string{"orig@example.test"},
		References: []string{"first@example.test"},
	}
	got, err := newTestService(f).Reply(context.Background(), testCreds, ids.Message(testAccount, "e1"), "Sounds good.")
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	account, vendor, err := ids.ParseMessage(got)
	if err != nil || account != testAccount || vendor != "r1" {
		t.Fatalf("identifier = %q (account %q vendor %q error %v)", got, account, vendor, err)
	}
	if f.replySourceID != "e1" {
		t.Fatalf("read message %q", f.replySourceID)
	}
	if f.draft.DraftsMailboxID != "d" || f.draft.From.Email != testCreds.User {
		t.Fatalf("draft = %+v", f.draft)
	}
	if len(f.draft.To) != 1 || f.draft.To[0].Email != "alice@example.test" {
		t.Fatalf("recipient = %+v, want the original sender", f.draft.To)
	}
	if f.draft.Subject != "Re: Lunch" {
		t.Fatalf("subject = %q", f.draft.Subject)
	}
	if f.draft.Text != "Sounds good." {
		t.Fatalf("body = %q", f.draft.Text)
	}
	if len(f.draft.InReplyTo) != 1 || f.draft.InReplyTo[0] != "orig@example.test" {
		t.Fatalf("inReplyTo = %v", f.draft.InReplyTo)
	}
	if len(f.draft.References) != 2 ||
		f.draft.References[0] != "first@example.test" ||
		f.draft.References[1] != "orig@example.test" {
		t.Fatalf("references = %v", f.draft.References)
	}
}

func TestServiceReplyKeepsAnExistingPrefix(t *testing.T) {
	for _, subject := range []string{"Re: Lunch", "RE: Lunch"} {
		f := sendFixture()
		f.draftID = "r1"
		f.replySource = jmap.Email{
			Subject:   subject,
			From:      []jmap.Address{{Email: "alice@example.test"}},
			MessageID: []string{"orig@example.test"},
		}
		if _, err := newTestService(f).Reply(context.Background(), testCreds, ids.Message(testAccount, "e1"), "hi"); err != nil {
			t.Fatalf("Reply: %v", err)
		}
		if f.draft.Subject != subject {
			t.Fatalf("subject = %q, want %q unchanged", f.draft.Subject, subject)
		}
	}
}

func TestServiceReplyRefusesAForeignMessage(t *testing.T) {
	f := sendFixture()
	_, err := newTestService(f).Reply(context.Background(), testCreds, ids.Message("another-account", "e1"), "hi")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.replySourceID != "" || f.draftCalls != 0 {
		t.Fatal("a foreign identifier reached the mail server")
	}
}

func TestServiceReplyReportsAnUnknownMessage(t *testing.T) {
	f := sendFixture()
	f.replySourceErr = jmap.ErrNotFound
	if _, err := newTestService(f).Reply(context.Background(), testCreds, ids.Message(testAccount, "gone"), "hi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceReplyRefusesAMessageWithoutASender(t *testing.T) {
	f := sendFixture()
	f.replySource = jmap.Email{ID: "e1", Subject: "Lunch"}
	_, err := newTestService(f).Reply(context.Background(), testCreds, ids.Message(testAccount, "e1"), "hi")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.draftCalls != 0 {
		t.Fatal("created a reply to a message with no sender")
	}
}

func TestServiceReplyRefusesASenderOutsideTheDomain(t *testing.T) {
	f := sendFixture()
	f.replySource = jmap.Email{
		ID:        "e1",
		Subject:   "Lunch",
		From:      []jmap.Address{{Email: "someone@elsewhere.test"}},
		MessageID: []string{"orig@example.test"},
	}
	_, err := newTestService(f).Reply(context.Background(), testCreds, ids.Message(testAccount, "e1"), "hi")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.draftCalls != 0 {
		t.Fatal("created a reply to a foreign sender")
	}
}

func TestReplyReferencesDeduplicates(t *testing.T) {
	got := replyReferences(
		[]string{"first@example.test", "orig@example.test", " "},
		[]string{"orig@example.test"},
	)
	if strings.Join(got, ",") != "first@example.test,orig@example.test" {
		t.Fatalf("references = %v", got)
	}
}

func TestServiceUpdateMessagePatchesOnlyNamedKeywords(t *testing.T) {
	f := &fakeUpstream{emails: []jmap.Email{{
		ID:            "e1",
		Subject:       "Hello",
		Keywords:      map[string]bool{"$seen": true, "$hasattachment": true},
		HasAttachment: true,
	}}}
	got, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "e1"), MessagePatch{Read: boolPtr(true)})
	if err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if f.updateID != "e1" || f.updatePatch.Read == nil || !*f.updatePatch.Read ||
		f.updatePatch.Starred != nil || f.updatePatch.MailboxID != "" {
		t.Fatalf("patch = id %q %+v", f.updateID, f.updatePatch)
	}
	if !got.Read || !got.HasAttachment {
		t.Fatalf("summary = %+v, want read and the attachment preserved", got)
	}
}

func TestServiceUpdateMessageMovesAndFlagsInOneUpdate(t *testing.T) {
	f := &fakeUpstream{
		mailboxExists: true,
		emails: []jmap.Email{{
			ID:            "e1",
			Keywords:      map[string]bool{"$seen": true, "$flagged": true},
			HasAttachment: true,
		}},
	}
	got, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "e1"), MessagePatch{Starred: boolPtr(true), MailboxID: ids.Mailbox(testAccount, "m2")})
	if err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if f.updatePatch.MailboxID != "m2" {
		t.Fatalf("target = %q, want the server identifier", f.updatePatch.MailboxID)
	}
	if f.updatePatch.Starred == nil || !*f.updatePatch.Starred {
		t.Fatalf("flags were not sent with the move: %+v", f.updatePatch)
	}
	if !got.Starred || !got.HasAttachment {
		t.Fatalf("summary = %+v, want flags and attachment preserved", got)
	}
}

func TestServiceUpdateMessageRejectsAnotherAccountsMessage(t *testing.T) {
	f := &fakeUpstream{}
	_, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message("other-account", "e1"), MessagePatch{Read: boolPtr(true)})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.updateID != "" {
		t.Fatal("updated a message identifier from another account")
	}
}

func TestServiceUpdateMessageRejectsAnotherAccountsMailbox(t *testing.T) {
	f := &fakeUpstream{}
	_, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "e1"), MessagePatch{MailboxID: ids.Mailbox("other-account", "m2")})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.updateID != "" {
		t.Fatal("moved a message into a mailbox from another account")
	}
	if f.mailboxQueries != 0 {
		t.Fatal("asked the mail server about a foreign mailbox identifier")
	}
}

func TestServiceUpdateMessageRejectsUnknownTargetMailbox(t *testing.T) {
	f := &fakeUpstream{mailboxExists: false}
	_, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "e1"), MessagePatch{MailboxID: ids.Mailbox(testAccount, "gone")})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.updateID != "" {
		t.Fatal("moved a message into a mailbox that does not exist")
	}
}

func TestServiceUpdateMessageRejectsAnEmptyPatch(t *testing.T) {
	f := &fakeUpstream{}
	_, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "e1"), MessagePatch{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.updateID != "" {
		t.Fatal("sent an empty patch to the mail server")
	}
}

func TestServiceUpdateMessageMapsNotFound(t *testing.T) {
	f := &fakeUpstream{updateErr: jmap.ErrNotFound}
	_, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "gone"), MessagePatch{Starred: boolPtr(true)})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceUpdateMessageMapsInvalidInput(t *testing.T) {
	f := &fakeUpstream{updateErr: jmap.ErrInvalidInput}
	_, err := newTestService(f).UpdateMessage(context.Background(), testCreds,
		ids.Message(testAccount, "e1"), MessagePatch{Starred: boolPtr(true)})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func trashFixture() *fakeUpstream {
	return &fakeUpstream{mailboxes: []jmap.Mailbox{
		{ID: "a", Name: "Inbox", Role: "inbox"},
		{ID: "t", Name: "Trash", Role: "trash"},
	}}
}

func TestServiceDeleteMessageMovesToTrash(t *testing.T) {
	f := trashFixture()
	if err := newTestService(f).DeleteMessage(context.Background(), testCreds, ids.Message(testAccount, "e1")); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if f.updateID != "e1" || f.updatePatch.MailboxID != "t" {
		t.Fatalf("update = id %q patch %+v, want a move to trash", f.updateID, f.updatePatch)
	}
	if f.destroyed != nil {
		t.Fatalf("destroyed %v; delete must not erase", f.destroyed)
	}
}

func TestServiceDeleteMessageInTrashIsARepeatedNoOp(t *testing.T) {
	f := trashFixture()
	svc := newTestService(f)
	for i := 0; i < 2; i++ {
		if err := svc.DeleteMessage(context.Background(), testCreds, ids.Message(testAccount, "e1")); err != nil {
			t.Fatalf("DeleteMessage %d: %v", i, err)
		}
	}
	if f.updatePatch.MailboxID != "t" {
		t.Fatalf("patch = %+v", f.updatePatch)
	}
}

func TestServiceDeleteMessageWithoutTrashIsUnavailable(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "a", Name: "Inbox", Role: "inbox"}}}
	err := newTestService(f).DeleteMessage(context.Background(), testCreds, ids.Message(testAccount, "e1"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if f.updateID != "" {
		t.Fatal("moved a message with no trash mailbox to move it to")
	}
}

func TestServiceDeleteMessageRejectsAnotherAccountsMessage(t *testing.T) {
	f := trashFixture()
	err := newTestService(f).DeleteMessage(context.Background(), testCreds, ids.Message("other-account", "e1"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.updateID != "" {
		t.Fatal("deleted a message identifier from another account")
	}
}

func TestServiceDeleteMessageMapsNotFound(t *testing.T) {
	f := trashFixture()
	f.updateErr = jmap.ErrNotFound
	err := newTestService(f).DeleteMessage(context.Background(), testCreds, ids.Message(testAccount, "gone"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceCreateMailboxValidatesNames(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		strings.Repeat("a", maxMailboxNameBytes+1),
		strings.Repeat("é", maxMailboxNameBytes/2+1),
		"a/b",
		"a\nb",
		"a\x00b",
		"\xff",
	}
	for _, name := range invalid {
		f := &fakeUpstream{}
		_, err := newTestService(f).CreateMailbox(context.Background(), testCreds, name)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("name %q: error = %v, want ErrInvalidInput", name, err)
		}
		if f.createdMailboxName != "" {
			t.Errorf("name %q reached the mail server", name)
		}
	}
}

func TestServiceCreateMailboxTrimsAndMapsTheIdentifier(t *testing.T) {
	f := &fakeUpstream{createdMailboxID: "m9"}
	got, err := newTestService(f).CreateMailbox(context.Background(), testCreds, "  Projects  ")
	if err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	if f.createdMailboxName != "Projects" {
		t.Fatalf("name sent = %q, want it trimmed", f.createdMailboxName)
	}
	account, vendor, err := ids.ParseMailbox(got)
	if err != nil || account != testAccount || vendor != "m9" {
		t.Fatalf("identifier = %q (account %q vendor %q error %v)", got, account, vendor, err)
	}
}

func TestServiceCreateMailboxRefusesADuplicateIgnoringCase(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "m1", Name: "Projects"}}}
	_, err := newTestService(f).CreateMailbox(context.Background(), testCreds, "projects")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("error = %v, want ErrAlreadyExists", err)
	}
	if f.createdMailboxName != "" {
		t.Fatal("created a second mailbox with the same name")
	}
}

func TestServiceCreateMailboxMapsAVendorRace(t *testing.T) {
	f := &fakeUpstream{createMailboxErr: jmap.ErrAlreadyExists}
	_, err := newTestService(f).CreateMailbox(context.Background(), testCreds, "Projects")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("error = %v, want ErrAlreadyExists", err)
	}
}

func TestServiceRenameMailbox(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{
		{ID: "m1", Name: "Projects"},
		{ID: "a", Name: "Inbox", Role: "inbox"},
	}}
	if err := newTestService(f).RenameMailbox(context.Background(), testCreds,
		ids.Mailbox(testAccount, "m1"), "Work"); err != nil {
		t.Fatalf("RenameMailbox: %v", err)
	}
	if f.renamedMailboxID != "m1" || f.renamedMailboxName != "Work" {
		t.Fatalf("rename = id %q name %q", f.renamedMailboxID, f.renamedMailboxName)
	}
}

func TestServiceRenameMailboxRefusesStandardMailboxes(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "a", Name: "Inbox", Role: "inbox"}}}
	err := newTestService(f).RenameMailbox(context.Background(), testCreds,
		ids.Mailbox(testAccount, "a"), "Something")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.renamedMailboxID != "" {
		t.Fatal("renamed a standard mailbox")
	}
}

func TestServiceRenameMailboxRefusesADuplicate(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{
		{ID: "m1", Name: "Projects"},
		{ID: "m2", Name: "Work"},
	}}
	err := newTestService(f).RenameMailbox(context.Background(), testCreds,
		ids.Mailbox(testAccount, "m1"), "work")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("error = %v, want ErrAlreadyExists", err)
	}
	if f.renamedMailboxID != "" {
		t.Fatal("renamed into a duplicate")
	}
}

func TestServiceRenameMailboxToTheSameNameIsANoOp(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "m1", Name: "Projects"}}}
	if err := newTestService(f).RenameMailbox(context.Background(), testCreds,
		ids.Mailbox(testAccount, "m1"), "Projects"); err != nil {
		t.Fatalf("RenameMailbox: %v", err)
	}
	if f.renamedMailboxID != "" {
		t.Fatal("sent a rename that changes nothing")
	}
}

func TestServiceRenameMailboxRejectsForeignAndUnknown(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "m1", Name: "Projects"}}}
	svc := newTestService(f)

	if err := svc.RenameMailbox(context.Background(), testCreds, ids.Mailbox("other-account", "m1"), "Work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign error = %v, want ErrNotFound", err)
	}
	if err := svc.RenameMailbox(context.Background(), testCreds, ids.Mailbox(testAccount, "gone"), "Work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown error = %v, want ErrNotFound", err)
	}
	if f.renamedMailboxID != "" {
		t.Fatal("renamed a mailbox the caller does not own")
	}
}

func TestServiceRenameMailboxMapsAVendorRace(t *testing.T) {
	f := &fakeUpstream{
		mailboxes:        []jmap.Mailbox{{ID: "m1", Name: "Projects"}},
		renameMailboxErr: jmap.ErrAlreadyExists,
	}
	err := newTestService(f).RenameMailbox(context.Background(), testCreds,
		ids.Mailbox(testAccount, "m1"), "Work")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("error = %v, want ErrAlreadyExists", err)
	}
}

func TestServiceDeleteMailbox(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "m1", Name: "Projects"}}}
	if err := newTestService(f).DeleteMailbox(context.Background(), testCreds, ids.Mailbox(testAccount, "m1")); err != nil {
		t.Fatalf("DeleteMailbox: %v", err)
	}
	if f.destroyedMailboxID != "m1" {
		t.Fatalf("destroyed = %q", f.destroyedMailboxID)
	}
}

func TestServiceDeleteMailboxRefusesStandardMailboxes(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "t", Name: "Trash", Role: "trash"}}}
	err := newTestService(f).DeleteMailbox(context.Background(), testCreds, ids.Mailbox(testAccount, "t"))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if f.destroyedMailboxID != "" {
		t.Fatal("destroyed a standard mailbox")
	}
}

func TestServiceDeleteMailboxRefusesMessagesAndChildren(t *testing.T) {
	cases := map[string][]jmap.Mailbox{
		"holds messages": {{ID: "m1", Name: "Projects", TotalEmails: 1}},
		"has children": {
			{ID: "m1", Name: "Projects"},
			{ID: "m2", Name: "Child", ParentID: "m1"},
		},
	}
	for name, boxes := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeUpstream{mailboxes: boxes}
			err := newTestService(f).DeleteMailbox(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"))
			if !errors.Is(err, ErrMailboxNotEmpty) {
				t.Fatalf("error = %v, want ErrMailboxNotEmpty", err)
			}
			if f.destroyedMailboxID != "" {
				t.Fatal("destroyed a mailbox that is not empty")
			}
		})
	}
}

func TestServiceDeleteMailboxMapsAVendorRefusal(t *testing.T) {
	f := &fakeUpstream{
		mailboxes:         []jmap.Mailbox{{ID: "m1", Name: "Projects"}},
		destroyMailboxErr: jmap.ErrMailboxNotEmpty,
	}
	err := newTestService(f).DeleteMailbox(context.Background(), testCreds, ids.Mailbox(testAccount, "m1"))
	if !errors.Is(err, ErrMailboxNotEmpty) {
		t.Fatalf("error = %v, want ErrMailboxNotEmpty", err)
	}
}

func TestServiceDeleteMailboxRejectsForeignAndUnknown(t *testing.T) {
	f := &fakeUpstream{mailboxes: []jmap.Mailbox{{ID: "m1", Name: "Projects"}}}
	svc := newTestService(f)

	if err := svc.DeleteMailbox(context.Background(), testCreds, ids.Mailbox("other-account", "m1")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign error = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteMailbox(context.Background(), testCreds, ids.Mailbox(testAccount, "gone")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown error = %v, want ErrNotFound", err)
	}
	if f.destroyedMailboxID != "" {
		t.Fatal("destroyed a mailbox the caller does not own")
	}
}

func attachmentFixture() *fakeUpstream {
	return &fakeUpstream{
		attachments: []jmap.Attachment{
			{BlobID: "b1", Name: "first.txt", Type: "text/plain", Size: 16, Disposition: "attachment"},
			{BlobID: "b2", Name: "second.bin", Type: "application/octet-stream", Size: 17, Disposition: "attachment"},
			{BlobID: "b3", Name: "", Type: "text/plain", Size: 3, Disposition: "inline"},
		},
		downloadBody: io.NopCloser(strings.NewReader("SECOND-FILE-BYTES")),
	}
}

func TestServiceAttachmentsListsNamedPartsOnly(t *testing.T) {
	f := attachmentFixture()
	got, err := newTestService(f).Attachments(context.Background(), testCreds, ids.Message(testAccount, "e1"))
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("attachments = %+v, want two named parts", got)
	}
	if got[0].Position != 0 || got[0].Filename != "first.txt" || got[0].ContentType != "text/plain" || got[0].Size != 16 {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Position != 1 || got[1].Filename != "second.bin" || got[1].ContentType != "application/octet-stream" || got[1].Size != 17 {
		t.Fatalf("second = %+v", got[1])
	}
	if f.attachmentID != "e1" || f.gotAccount != testAccount {
		t.Fatalf("read account %q message %q", f.gotAccount, f.attachmentID)
	}
}

func TestServiceAttachmentsOnAPlainMessage(t *testing.T) {
	f := &fakeUpstream{}
	got, err := newTestService(f).Attachments(context.Background(), testCreds, ids.Message(testAccount, "e1"))
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("attachments = %+v, want empty", got)
	}
}

func TestServiceAttachmentsRefusesAForeignMessage(t *testing.T) {
	f := attachmentFixture()
	if _, err := newTestService(f).Attachments(context.Background(), testCreds, ids.Message("another-account", "e1")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.attachmentID != "" {
		t.Fatal("a foreign identifier reached the mail server")
	}
}

func TestServiceAttachmentsReportsAnUnknownMessage(t *testing.T) {
	f := attachmentFixture()
	f.attachmentsErr = jmap.ErrNotFound
	if _, err := newTestService(f).Attachments(context.Background(), testCreds, ids.Message(testAccount, "gone")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceDownloadAttachmentOpensTheSelectedPart(t *testing.T) {
	f := attachmentFixture()
	got, err := newTestService(f).DownloadAttachment(context.Background(), testCreds, ids.Message(testAccount, "e1"), 1)
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	defer func() { _ = got.Body.Close() }()
	if got.Filename != "second.bin" || got.ContentType != "application/octet-stream" {
		t.Fatalf("download = %+v", got)
	}
	if f.downloadAttachment.BlobID != "b2" || f.downloadAccount != testAccount || f.downloadCalls != 1 {
		t.Fatalf("upstream download = %+v", f)
	}
	data, err := io.ReadAll(got.Body)
	if err != nil || string(data) != "SECOND-FILE-BYTES" {
		t.Fatalf("bytes = %q error %v", data, err)
	}
}

func TestServiceDownloadAttachmentRefusesBadPositions(t *testing.T) {
	for _, position := range []int{-1, 2, 99} {
		f := attachmentFixture()
		_, err := newTestService(f).DownloadAttachment(context.Background(), testCreds, ids.Message(testAccount, "e1"), position)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("position %d: error = %v, want ErrNotFound", position, err)
		}
		if f.downloadCalls != 0 {
			t.Fatalf("position %d fetched a blob", position)
		}
	}
}

func TestServiceDownloadAttachmentRefusesAForeignMessage(t *testing.T) {
	f := attachmentFixture()
	_, err := newTestService(f).DownloadAttachment(context.Background(), testCreds, ids.Message("another-account", "e1"), 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.attachmentID != "" || f.downloadCalls != 0 {
		t.Fatal("a foreign identifier reached the mail server")
	}
}

func TestServiceDownloadAttachmentRejectsAnOversizePart(t *testing.T) {
	f := attachmentFixture()
	f.attachments[0].Size = maxAttachmentBytes + 1
	_, err := newTestService(f).DownloadAttachment(context.Background(), testCreds, ids.Message(testAccount, "e1"), 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if f.downloadCalls != 0 {
		t.Fatal("fetched an oversize attachment")
	}
}

func TestServiceDownloadAttachmentMapsUpstreamFailure(t *testing.T) {
	f := attachmentFixture()
	f.downloadErr = fmt.Errorf("%w: status 500", jmap.ErrUnavailable)
	_, err := newTestService(f).DownloadAttachment(context.Background(), testCreds, ids.Message(testAccount, "e1"), 0)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func searchFixture() *fakeUpstream {
	return &fakeUpstream{
		query: jmap.QueryResult{IDs: []string{"e1"}, Total: 1, Position: 0},
		emails: []jmap.Email{{
			ID: "e1", Subject: "Lunch", From: []jmap.Address{{Email: "alice@example.test"}},
		}},
		mailboxExists: true,
	}
}

func TestServiceSearchReturnsSummariesWithATextFilter(t *testing.T) {
	f := searchFixture()
	page, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{
		Text: "Lunch", Page: PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != ids.Message(testAccount, "e1") {
		t.Fatalf("page = %+v", page)
	}
	if page.HasMore || page.NextCursor != "" {
		t.Fatalf("single page reports more: %+v", page)
	}
	if f.gotQuery.Text != "Lunch" || f.gotQuery.MailboxID != "" || f.gotQuery.Limit != 10 {
		t.Fatalf("query = %+v", f.gotQuery)
	}
	if f.queryCalls != 1 {
		t.Fatalf("query calls = %d", f.queryCalls)
	}
}

func TestServiceSearchWithNoMatches(t *testing.T) {
	f := searchFixture()
	f.query = jmap.QueryResult{Total: 0}
	page, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{Text: "nothing"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Messages) != 0 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("page = %+v", page)
	}
}

func TestServiceSearchRestrictsToOneMailbox(t *testing.T) {
	f := searchFixture()
	page, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{
		Text: "Lunch", MailboxID: ids.Mailbox(testAccount, "m1"),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Messages) != 1 || f.gotQuery.MailboxID != "m1" || f.gotMailboxID != "m1" {
		t.Fatalf("page = %+v query = %+v mailbox %q", page, f.gotQuery, f.gotMailboxID)
	}
}

func TestServiceSearchRefusesAForeignMailbox(t *testing.T) {
	f := searchFixture()
	_, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{
		Text: "Lunch", MailboxID: ids.Mailbox("another-account", "m1"),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.queryCalls != 0 || f.mailboxQueries != 0 {
		t.Fatal("a foreign mailbox reached the mail server")
	}
}

func TestServiceSearchRefusesAnUnknownMailbox(t *testing.T) {
	f := searchFixture()
	f.mailboxExists = false
	_, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{
		Text: "Lunch", MailboxID: ids.Mailbox(testAccount, "gone"),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if f.queryCalls != 0 {
		t.Fatal("queried an unknown mailbox")
	}
}

func TestServiceSearchRefusesAForeignOrMalformedCursor(t *testing.T) {
	for _, cursor := range []string{"not-a-cursor", ids.Cursor("another-account", "e0")} {
		f := searchFixture()
		_, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{
			Text: "Lunch", Page: PageRequest{Cursor: cursor},
		})
		if !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("cursor %q: error = %v, want ErrInvalidCursor", cursor, err)
		}
		if f.queryCalls != 0 {
			t.Fatalf("cursor %q reached the mail server", cursor)
		}
	}
}

func TestServiceSearchPagesWithTheSameCursor(t *testing.T) {
	f := searchFixture()
	f.query = jmap.QueryResult{IDs: []string{"e2"}, Total: 3, Position: 1}
	f.emails = []jmap.Email{{ID: "e2", Subject: "Lunch again"}}
	page, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{
		Text: "Lunch", Page: PageRequest{Cursor: ids.Cursor(testAccount, "e1")},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !page.HasMore || page.NextCursor != ids.Cursor(testAccount, "e2") {
		t.Fatalf("page = %+v", page)
	}
	if f.gotQuery.Anchor != "e1" {
		t.Fatalf("anchor = %q", f.gotQuery.Anchor)
	}
}

func TestServiceSearchMapsAnUpstreamFailure(t *testing.T) {
	f := searchFixture()
	f.queryErr = fmt.Errorf("%w: status 500", jmap.ErrUnavailable)
	if _, err := newTestService(f).Search(context.Background(), testCreds, SearchRequest{Text: "Lunch"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}
