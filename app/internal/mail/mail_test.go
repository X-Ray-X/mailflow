package mail

import (
	"context"
	"errors"
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

func newTestService(f *fakeUpstream) *Service {
	return NewService(f, 25, 100, 1024)
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
	svc := NewService(f, 25, 100, 8)

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
