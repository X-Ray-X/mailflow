package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/mail"
)

// fakeReader is a scripted Reader. Handlers are tested against it so no test
// depends on a mail server.
type fakeReader struct {
	mu sync.Mutex

	creds     mail.Credentials
	mailboxID string
	pageReq   mail.PageRequest
	messageID string
	calls     int

	mailboxesFn func(context.Context) ([]mail.Mailbox, error)
	messagesFn  func(context.Context) (mail.Page, error)
	messageFn   func(context.Context) (mail.Message, error)
}

func (f *fakeReader) record(creds mail.Credentials) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creds = creds
	f.calls++
}

func (f *fakeReader) Mailboxes(ctx context.Context, creds mail.Credentials) ([]mail.Mailbox, error) {
	f.record(creds)
	if f.mailboxesFn != nil {
		return f.mailboxesFn(ctx)
	}
	return nil, nil
}

func (f *fakeReader) Messages(ctx context.Context, creds mail.Credentials, mailboxID string, page mail.PageRequest) (mail.Page, error) {
	f.record(creds)
	f.mu.Lock()
	f.mailboxID = mailboxID
	f.pageReq = page
	f.mu.Unlock()
	if f.messagesFn != nil {
		return f.messagesFn(ctx)
	}
	return mail.Page{}, nil
}

func (f *fakeReader) Message(ctx context.Context, creds mail.Credentials, messageID string) (mail.Message, error) {
	f.record(creds)
	f.mu.Lock()
	f.messageID = messageID
	f.mu.Unlock()
	if f.messageFn != nil {
		return f.messageFn(ctx)
	}
	return mail.Message{}, nil
}

func (f *fakeReader) snapshot() (mail.Credentials, string, mail.PageRequest, string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creds, f.mailboxID, f.pageReq, f.messageID, f.calls
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T, reader mail.Reader, opts Options) *httptest.Server {
	t.Helper()
	if opts.Logger == nil {
		opts.Logger = discardLogger()
	}
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Second
	}
	if opts.PageSizeMax == 0 {
		opts.PageSizeMax = 100
	}
	srv := httptest.NewServer(New(reader, opts).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func request(t *testing.T, srv *httptest.Server, method, path string, creds *mail.Credentials) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if creds != nil {
		req.SetBasicAuth(creds.User, creds.Password)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func body(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return data
}

func decodeError(t *testing.T, resp *http.Response) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(body(t, resp), &e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

var testCreds = mail.Credentials{User: "bob@example.test", Password: "secret"}

func TestUnknownPathIsJSONNotFound(t *testing.T) {
	srv := newTestServer(t, &fakeReader{}, Options{})
	resp := request(t, srv, http.MethodGet, "/nope", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if got := decodeError(t, resp); got.Code != codeNotFound || got.Message == "" {
		t.Fatalf("error body = %+v", got)
	}
}

func TestWrongMethodIsJSONWithAllow(t *testing.T) {
	srv := newTestServer(t, &fakeReader{}, Options{})
	resp := request(t, srv, http.MethodPost, "/v1/mailboxes", &testCreds)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Fatalf("Allow = %q", allow)
	}
	if got := decodeError(t, resp); got.Code != codeMethodNotAllowed {
		t.Fatalf("error body = %+v", got)
	}
}

func TestMissingCredentialsAreRefused(t *testing.T) {
	f := &fakeReader{}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
	if got := decodeError(t, resp); got.Code != codeUnauthorized {
		t.Fatalf("error body = %+v", got)
	}
	if _, _, _, _, calls := f.snapshot(); calls != 0 {
		t.Fatalf("reader was called %d times without credentials", calls)
	}
}

func TestCredentialsAreForwarded(t *testing.T) {
	f := &fakeReader{}
	srv := newTestServer(t, f, Options{})

	if resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	creds, _, _, _, _ := f.snapshot()
	if creds != testCreds {
		t.Fatalf("forwarded credentials = %+v", creds)
	}
}

func TestMailboxesResponse(t *testing.T) {
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		return []mail.Mailbox{
			{ID: "mb1_abc", Name: "Inbox", Role: "inbox", Unread: 3, Total: 9},
			{ID: "mb1_def", Name: "Receipts", Role: "custom", Unread: 0, Total: 2},
		}, nil
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	var got struct {
		Mailboxes []map[string]any `json:"mailboxes"`
	}
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Mailboxes) != 2 {
		t.Fatalf("got %d mailboxes", len(got.Mailboxes))
	}
	first := got.Mailboxes[0]
	if first["id"] != "mb1_abc" || first["name"] != "Inbox" || first["role"] != "inbox" {
		t.Fatalf("first mailbox = %v", first)
	}
	if first["unread"] != float64(3) || first["total"] != float64(9) {
		t.Fatalf("counts = %v", first)
	}
	for _, key := range []string{"role", "name", "id", "unread", "total"} {
		if _, ok := first[key]; !ok {
			t.Errorf("mailbox is missing %q: %v", key, first)
		}
	}
}

func TestMailboxesUnauthorized(t *testing.T) {
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		return nil, mail.ErrUnauthorized
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeUnauthorized {
		t.Fatalf("error body = %+v", got)
	}
}

func TestMessagesPageAndBounds(t *testing.T) {
	received := time.Date(2026, 11, 15, 21, 0, 0, 0, time.UTC)
	f := &fakeReader{messagesFn: func(context.Context) (mail.Page, error) {
		return mail.Page{
			Messages: []mail.MessageSummary{
				{ID: "msg1_a", Subject: "Newest", From: mail.Address{Name: "Alice", Email: "alice@example.test"},
					Received: received, Read: true, Starred: false, HasAttachment: true},
			},
			NextCursor: "cur1_b",
			HasMore:    true,
		}, nil
	}}
	srv := newTestServer(t, f, Options{PageSizeMax: 100})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes/mb1_x/messages?limit=5000&cursor=cur1_prev", &testCreds)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_, mailboxID, pageReq, _, _ := f.snapshot()
	if mailboxID != "mb1_x" {
		t.Fatalf("mailbox id = %q", mailboxID)
	}
	if pageReq.Limit != 100 {
		t.Fatalf("limit = %d, want it clamped to 100", pageReq.Limit)
	}
	if pageReq.Cursor != "cur1_prev" {
		t.Fatalf("cursor = %q", pageReq.Cursor)
	}

	var got struct {
		Messages   []map[string]any `json:"messages"`
		NextCursor string           `json:"nextCursor"`
		HasMore    bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.HasMore || got.NextCursor != "cur1_b" || len(got.Messages) != 1 {
		t.Fatalf("response = %+v", got)
	}
	if got.Messages[0]["read"] != true || got.Messages[0]["hasAttachment"] != true {
		t.Fatalf("message = %v", got.Messages[0])
	}
	if _, ok := got.Messages[0]["receivedAt"]; !ok {
		t.Fatalf("message has no receivedAt: %v", got.Messages[0])
	}
}

func TestMessagesRejectsBadLimit(t *testing.T) {
	for _, limit := range []string{"abc", "0", "-3"} {
		f := &fakeReader{}
		srv := newTestServer(t, f, Options{})
		resp := request(t, srv, http.MethodGet, "/v1/mailboxes/mb1_x/messages?limit="+limit, &testCreds)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("limit=%s status = %d, want 400", limit, resp.StatusCode)
		}
		if got := decodeError(t, resp); got.Code != codeInvalidRequest {
			t.Fatalf("limit=%s error = %+v", limit, got)
		}
		if _, _, _, _, calls := f.snapshot(); calls != 0 {
			t.Fatalf("limit=%s reached the reader", limit)
		}
	}
}

func TestMessagesInvalidCursor(t *testing.T) {
	f := &fakeReader{messagesFn: func(context.Context) (mail.Page, error) {
		return mail.Page{}, mail.ErrInvalidCursor
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes/mb1_x/messages?cursor=gone", &testCreds)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeInvalidCursor {
		t.Fatalf("error body = %+v", got)
	}
}

func TestMessageDetail(t *testing.T) {
	f := &fakeReader{messageFn: func(context.Context) (mail.Message, error) {
		return mail.Message{
			MessageSummary: mail.MessageSummary{
				ID:      "msg1_a",
				Subject: "Hello",
				From:    mail.Address{Name: "Alice", Email: "alice@example.test"},
				Read:    true,
			},
			To:            []mail.Address{{Name: "Bob", Email: "bob@example.test"}},
			Text:          "body text",
			TextTruncated: true,
		}, nil
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/messages/msg1_a", &testCreds)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["text"] != "body text" || got["textTruncated"] != true {
		t.Fatalf("body = %v", got)
	}
	if _, ok := got["cc"].([]any); !ok {
		t.Fatalf("cc = %v, want an array", got["cc"])
	}
	if _, ok := got["to"].([]any); !ok {
		t.Fatalf("to = %v, want an array", got["to"])
	}
	if _, ok := got["read"]; !ok {
		t.Fatalf("read flag missing: %v", got)
	}
}

func TestMessageNotFound(t *testing.T) {
	f := &fakeReader{messageFn: func(context.Context) (mail.Message, error) {
		return mail.Message{}, mail.ErrNotFound
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/messages/msg1_missing", &testCreds)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeNotFound {
		t.Fatalf("error body = %+v", got)
	}
}

func TestUnavailableIsReported(t *testing.T) {
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		return nil, mail.ErrUnavailable
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeServiceUnavailable {
		t.Fatalf("error body = %+v", got)
	}
}

func TestInternalErrorIsOpaque(t *testing.T) {
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		return nil, errors.New("boom: vendor detail")
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	data := body(t, resp)
	if strings.Contains(string(data), "boom") {
		t.Fatalf("error body carries the cause: %s", data)
	}
	var got errorBody
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Code != codeInternal {
		t.Fatalf("error body = %+v", got)
	}
}

func TestPanicBecomesInternalError(t *testing.T) {
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		panic("unexpected")
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeInternal {
		t.Fatalf("error body = %+v", got)
	}
}

func TestHealthNeedsNoCredentialsAndNoMail(t *testing.T) {
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		return nil, mail.ErrUnavailable
	}}
	srv := newTestServer(t, f, Options{})

	resp := request(t, srv, http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got healthResponse
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" {
		t.Fatalf("health = %+v", got)
	}
	if _, _, _, _, calls := f.snapshot(); calls != 0 {
		t.Fatal("health touched the mail server")
	}
}

func TestRequestTimeoutIsEnforced(t *testing.T) {
	f := &fakeReader{messagesFn: func(ctx context.Context) (mail.Page, error) {
		<-ctx.Done()
		return mail.Page{}, errors.Join(mail.ErrUnavailable, ctx.Err())
	}}
	srv := newTestServer(t, f, Options{Timeout: 100 * time.Millisecond})

	start := time.Now()
	resp := request(t, srv, http.MethodGet, "/v1/mailboxes/mb1_x/messages", &testCreds)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("request took %v", elapsed)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestLogsHoldNoCredentials(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	f := &fakeReader{mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
		return []mail.Mailbox{{ID: "mb1_a", Name: "Inbox", Role: "inbox"}}, nil
	}}
	srv := newTestServer(t, f, Options{Logger: logger})

	creds := mail.Credentials{User: "bob@example.test", Password: "hunter2-should-not-appear"}
	if resp := request(t, srv, http.MethodGet, "/v1/mailboxes", &creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	out := logs.String()
	if out == "" {
		t.Fatal("no request was logged")
	}
	for _, secret := range []string{"hunter2-should-not-appear", "Basic ", "Authorization"} {
		if strings.Contains(out, secret) {
			t.Fatalf("logs contain %q: %s", secret, out)
		}
	}
}

// TestPayloadsCarryNoVendorVocabulary applies the AGENTS.md replaceability
// checklist to every response body this change publishes.
func TestPayloadsCarryNoVendorVocabulary(t *testing.T) {
	f := &fakeReader{
		mailboxesFn: func(context.Context) ([]mail.Mailbox, error) {
			return []mail.Mailbox{{ID: "mb1_a", Name: "Inbox", Role: "inbox", Unread: 1, Total: 2}}, nil
		},
		messagesFn: func(context.Context) (mail.Page, error) {
			return mail.Page{
				Messages:   []mail.MessageSummary{{ID: "msg1_a", Subject: "Hi", From: mail.Address{Email: "alice@example.test"}}},
				NextCursor: "cur1_b",
				HasMore:    true,
			}, nil
		},
		messageFn: func(context.Context) (mail.Message, error) {
			return mail.Message{MessageSummary: mail.MessageSummary{ID: "msg1_a", Subject: "Hi"}, Text: "hello"}, nil
		},
	}
	srv := newTestServer(t, f, Options{})

	payloads := [][]byte{
		body(t, request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)),
		body(t, request(t, srv, http.MethodGet, "/v1/mailboxes/mb1_a/messages", &testCreds)),
		body(t, request(t, srv, http.MethodGet, "/v1/messages/msg1_a", &testCreds)),
		body(t, request(t, srv, http.MethodGet, "/v1/messages/msg1_gone", &testCreds)),
	}
	forbidden := []string{
		"urn:", "$seen", "$flagged", "$hasattachment", "keywords", "blobId", "threadId",
		"mailboxIds", "accountId", "bodyValues", "textBody", "notFound", "anchor",
		"Email/", "Mailbox/", "queryState", "sessionState", "mail.mailflow.test",
	}
	for _, payload := range payloads {
		lower := strings.ToLower(string(payload))
		for _, token := range forbidden {
			if strings.Contains(lower, strings.ToLower(token)) {
				t.Errorf("payload contains vendor token %q: %s", token, payload)
			}
		}
	}
}
