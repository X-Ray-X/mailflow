package api

import (
	"bytes"
	"context"
	"crypto/sha256"
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

	"github.com/X-Ray-X/mailflow/app/internal/idempotency"
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

	sendFn     func(context.Context, mail.OutgoingMessage) (string, error)
	setFlagsFn func(context.Context, string, mail.FlagPatch) (mail.MessageSummary, error)

	gotSend    mail.OutgoingMessage
	gotFlagID  string
	gotPatch   mail.FlagPatch
	writeCalls int
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

func (f *fakeReader) Send(ctx context.Context, creds mail.Credentials, msg mail.OutgoingMessage) (string, error) {
	f.record(creds)
	f.mu.Lock()
	f.gotSend = msg
	f.writeCalls++
	f.mu.Unlock()
	if f.sendFn != nil {
		return f.sendFn(ctx, msg)
	}
	return "", nil
}

func (f *fakeReader) SetFlags(ctx context.Context, creds mail.Credentials, messageID string, patch mail.FlagPatch) (mail.MessageSummary, error) {
	f.record(creds)
	f.mu.Lock()
	f.gotFlagID = messageID
	f.gotPatch = patch
	f.writeCalls++
	f.mu.Unlock()
	if f.setFlagsFn != nil {
		return f.setFlagsFn(ctx, messageID, patch)
	}
	return mail.MessageSummary{}, nil
}

func (f *fakeReader) snapshot() (mail.Credentials, string, mail.PageRequest, string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creds, f.mailboxID, f.pageReq, f.messageID, f.calls
}

func (f *fakeReader) writeSnapshot() (mail.OutgoingMessage, string, mail.FlagPatch, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotSend, f.gotFlagID, f.gotPatch, f.writeCalls
}

// fakeRecorder is a scripted idempotency store. The handler's decision is what
// these tests assert; the durable implementation is tested against PostgreSQL.
type fakeRecorder struct {
	mu sync.Mutex

	claim    idempotency.Claim
	claimErr error

	claims     int
	gotAccount string
	gotKey     string
	gotHash    []byte
	gotTTL     time.Duration
	gotLease   time.Duration

	completed   []string
	completeErr error
	released    []string
	releaseErr  error
}

func (f *fakeRecorder) Claim(_ context.Context, account, key string, hash []byte, ttl, lease time.Duration) (idempotency.Claim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	f.gotAccount = account
	f.gotKey = key
	f.gotHash = append([]byte(nil), hash...)
	f.gotTTL = ttl
	f.gotLease = lease
	return f.claim, f.claimErr
}

func (f *fakeRecorder) Complete(_ context.Context, _, _, messageID string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, messageID)
	return f.completeErr
}

func (f *fakeRecorder) Release(_ context.Context, _, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, key)
	return f.releaseErr
}

func (f *fakeRecorder) snapshot() (int, string, string, []byte, []string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims, f.gotAccount, f.gotKey, f.gotHash, append([]string(nil), f.completed...), append([]string(nil), f.released...)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T, service Service, opts Options) *httptest.Server {
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
	if opts.IdempotencyTTL == 0 {
		opts.IdempotencyTTL = time.Hour
	}
	if opts.IdempotencyLease == 0 {
		opts.IdempotencyLease = time.Minute
	}
	srv := httptest.NewServer(New(service, opts).Handler())
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

func requestJSON(t *testing.T, srv *httptest.Server, method, path, key, payload string, creds *mail.Credentials) *http.Response {
	t.Helper()
	var reader io.Reader
	if payload != "" {
		reader = strings.NewReader(payload)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
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
		sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
			return "msg1_sent", nil
		},
		setFlagsFn: func(_ context.Context, id string, _ mail.FlagPatch) (mail.MessageSummary, error) {
			return mail.MessageSummary{ID: id, Subject: "Hi", Read: true}, nil
		},
	}
	rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Claimed}}
	srv := newTestServer(t, f, Options{Records: rec})

	payloads := [][]byte{
		body(t, request(t, srv, http.MethodGet, "/v1/mailboxes", &testCreds)),
		body(t, request(t, srv, http.MethodGet, "/v1/mailboxes/mb1_a/messages", &testCreds)),
		body(t, request(t, srv, http.MethodGet, "/v1/messages/msg1_a", &testCreds)),
		body(t, request(t, srv, http.MethodGet, "/v1/messages/msg1_gone", &testCreds)),
		body(t, requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)),
		body(t, requestJSON(t, srv, http.MethodPatch, "/v1/messages/msg1_a", "", `{"read":true}`, &testCreds)),
	}
	forbidden := []string{
		"urn:", "$seen", "$flagged", "$hasattachment", "keywords", "blobId", "threadId",
		"mailboxIds", "accountId", "bodyValues", "textBody", "notFound", "anchor",
		"Email/", "Mailbox/", "queryState", "sessionState", "mail.mailflow.test",
		"EmailSubmission", "Identity/", "invalidProperties", "onSuccessUpdateEmail",
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

const validSendBody = `{"to":[{"name":"Alice","email":"alice@example.test"}],"subject":"Lunch","text":"Noon?"}`

func TestSendCreatesAndRecords(t *testing.T) {
	rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Claimed}}
	f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
		return "msg1_sent", nil
	}}
	srv := newTestServer(t, f, Options{Records: rec, SendMaxRecipients: 50, SendMaxBytes: 1024})

	resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", resp.StatusCode, body(t, resp))
	}
	if replay := resp.Header.Get("Idempotency-Replayed"); replay != "" {
		t.Fatalf("Idempotency-Replayed = %q on a first send", replay)
	}
	var got sendResponse
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "msg1_sent" {
		t.Fatalf("id = %q", got.ID)
	}

	claims, account, key, hash, completed, released := rec.snapshot()
	if claims != 1 || account != testCreds.User || key != "key-1" {
		t.Fatalf("claim = %d account %q key %q", claims, account, key)
	}
	if len(hash) != sha256.Size {
		t.Fatalf("hash length = %d, want %d", len(hash), sha256.Size)
	}
	if len(completed) != 1 || completed[0] != "msg1_sent" {
		t.Fatalf("completed = %v", completed)
	}
	if len(released) != 0 {
		t.Fatalf("released = %v after a successful send", released)
	}

	sent, _, _, calls := f.writeSnapshot()
	if calls != 1 || sent.Subject != "Lunch" || sent.Text != "Noon?" {
		t.Fatalf("service call = %d message %+v", calls, sent)
	}
	if len(sent.To) != 1 || sent.To[0].Email != "alice@example.test" {
		t.Fatalf("recipients = %+v", sent.To)
	}
}

func TestSendReplayReturnsTheOriginalResult(t *testing.T) {
	rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Replay, MessageID: "msg1_old"}}
	f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
		t.Fatal("a replay reached the mail service")
		return "", nil
	}}
	srv := newTestServer(t, f, Options{Records: rec})

	resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if replay := resp.Header.Get("Idempotency-Replayed"); replay != "true" {
		t.Fatalf("Idempotency-Replayed = %q, want true", replay)
	}
	var got sendResponse
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "msg1_old" {
		t.Fatalf("id = %q, want the recorded identifier", got.ID)
	}
	_, _, _, _, completed, _ := rec.snapshot()
	if len(completed) != 0 {
		t.Fatalf("a replay recorded a completion: %v", completed)
	}
}

func TestSendRefusesReusedAndInFlightKeys(t *testing.T) {
	cases := []struct {
		name  string
		state idempotency.State
		code  string
	}{
		{"different request", idempotency.Mismatch, codeConflict},
		{"still in flight", idempotency.InFlight, codeInProgress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeRecorder{claim: idempotency.Claim{State: tc.state}}
			f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
				t.Fatal("a refused duplicate reached the mail service")
				return "", nil
			}}
			srv := newTestServer(t, f, Options{Records: rec})

			resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("status = %d, want 409", resp.StatusCode)
			}
			if got := decodeError(t, resp); got.Code != tc.code {
				t.Fatalf("error = %+v, want %q", got, tc.code)
			}
		})
	}
}

func TestSendRequiresAKey(t *testing.T) {
	rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Claimed}}
	f := &fakeReader{}
	srv := newTestServer(t, f, Options{Records: rec})

	resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "", validSendBody, &testCreds)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeInvalidRequest {
		t.Fatalf("error = %+v", got)
	}
	if claims, _, _, _, _, _ := rec.snapshot(); claims != 0 {
		t.Fatalf("claimed a key without one: %d", claims)
	}
	if _, _, _, calls := f.writeSnapshot(); calls != 0 {
		t.Fatal("sent without an idempotency key")
	}
}

func TestSendRejectsBadInputBeforeClaiming(t *testing.T) {
	cases := []struct {
		name    string
		options Options
		payload string
	}{
		{
			name:    "unknown field",
			payload: `{"to":[{"email":"alice@example.test"}],"subject":"Hi","text":"x","seen":true}`,
		},
		{
			name:    "no recipients",
			payload: `{"subject":"Hi","text":"x"}`,
		},
		{
			name:    "invalid address",
			payload: `{"to":[{"email":"not-an-address"}],"subject":"Hi","text":"x"}`,
		},
		{
			name:    "too many recipients",
			options: Options{SendMaxRecipients: 1},
			payload: `{"to":[{"email":"a@example.test"},{"email":"b@example.test"}],"subject":"Hi","text":"x"}`,
		},
		{
			name:    "body too large",
			options: Options{SendMaxBytes: 2},
			payload: `{"to":[{"email":"a@example.test"}],"subject":"Hi","text":"three"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Claimed}}
			f := &fakeReader{}
			tc.options.Records = rec
			srv := newTestServer(t, f, tc.options)

			resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", tc.payload, &testCreds)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body(t, resp))
			}
			if got := decodeError(t, resp); got.Code != codeInvalidRequest {
				t.Fatalf("error = %+v", got)
			}
			if claims, _, _, _, _, _ := rec.snapshot(); claims != 0 {
				t.Fatalf("claimed a key for invalid input: %d", claims)
			}
			if _, _, _, calls := f.writeSnapshot(); calls != 0 {
				t.Fatal("invalid input reached the mail service")
			}
		})
	}
}

func TestSendReleasesTheClaimWhenNothingWasSubmitted(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"server refused", mail.ErrRejected, codeSendRejected},
		{"foreign recipient", mail.ErrInvalidInput, codeInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Claimed}}
			f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
				return "", tc.err
			}}
			srv := newTestServer(t, f, Options{Records: rec})

			resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
			if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			if got := decodeError(t, resp); got.Code != tc.code {
				t.Fatalf("error = %+v, want %q", got, tc.code)
			}
			_, _, key, _, completed, released := rec.snapshot()
			if len(completed) != 0 {
				t.Fatalf("recorded a completion for a refused send: %v", completed)
			}
			if len(released) != 1 || released[0] != key {
				t.Fatalf("released = %v, want the claim for %q", released, key)
			}
		})
	}
}

func TestSendKeepsTheClaimWhenTheOutcomeIsUnknown(t *testing.T) {
	rec := &fakeRecorder{claim: idempotency.Claim{State: idempotency.Claimed}}
	f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
		return "", mail.ErrUnavailable
	}}
	srv := newTestServer(t, f, Options{Records: rec})

	resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if _, _, _, _, _, released := rec.snapshot(); len(released) != 0 {
		t.Fatalf("released = %v; the send may have happened", released)
	}
}

func TestSendFailsClosedWithoutARecordStore(t *testing.T) {
	f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
		t.Fatal("sent without an idempotency record store")
		return "", nil
	}}
	srv := newTestServer(t, f, Options{})

	resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeServiceUnavailable {
		t.Fatalf("error = %+v", got)
	}
}

func TestSendStillSucceedsWhenRecordingFails(t *testing.T) {
	rec := &fakeRecorder{
		claim:       idempotency.Claim{State: idempotency.Claimed},
		completeErr: errors.New("database went away"),
	}
	f := &fakeReader{sendFn: func(context.Context, mail.OutgoingMessage) (string, error) {
		return "msg1_sent", nil
	}}
	srv := newTestServer(t, f, Options{Records: rec})

	resp := requestJSON(t, srv, http.MethodPost, "/v1/messages", "key-1", validSendBody, &testCreds)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201; the message was accepted", resp.StatusCode)
	}
}

func TestSendRequiresPost(t *testing.T) {
	srv := newTestServer(t, &fakeReader{}, Options{})
	resp := request(t, srv, http.MethodGet, "/v1/messages", &testCreds)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != http.MethodPost {
		t.Fatalf("Allow = %q", allow)
	}
}

func TestSetFlagsReturnsTheUpdatedSummary(t *testing.T) {
	f := &fakeReader{setFlagsFn: func(_ context.Context, messageID string, patch mail.FlagPatch) (mail.MessageSummary, error) {
		return mail.MessageSummary{
			ID:            messageID,
			Subject:       "Hello",
			Read:          patch.Read != nil && *patch.Read,
			Starred:       patch.Starred != nil && *patch.Starred,
			HasAttachment: true,
		}, nil
	}}
	srv := newTestServer(t, f, Options{})

	resp := requestJSON(t, srv, http.MethodPatch, "/v1/messages/msg1_a", "", `{"read":true,"starred":false}`, &testCreds)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body(t, resp))
	}
	var got map[string]any
	if err := json.Unmarshal(body(t, resp), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["read"] != true || got["starred"] != false || got["hasAttachment"] != true {
		t.Fatalf("summary = %v", got)
	}
	if got["id"] != "msg1_a" {
		t.Fatalf("id = %v", got["id"])
	}

	_, id, patch, calls := f.writeSnapshot()
	if calls != 1 || id != "msg1_a" {
		t.Fatalf("service call = %d id %q", calls, id)
	}
	if patch.Read == nil || !*patch.Read || patch.Starred == nil || *patch.Starred {
		t.Fatalf("patch = %+v", patch)
	}
}

func TestSetFlagsRejectsEmptyAndUnknownRequests(t *testing.T) {
	for _, payload := range []string{`{}`, `{"seen":true}`, `{"read":true,"seen":false}`} {
		f := &fakeReader{}
		srv := newTestServer(t, f, Options{})
		resp := requestJSON(t, srv, http.MethodPatch, "/v1/messages/msg1_a", "", payload, &testCreds)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("payload %s: status = %d, want 400", payload, resp.StatusCode)
		}
		if got := decodeError(t, resp); got.Code != codeInvalidRequest {
			t.Fatalf("payload %s: error = %+v", payload, got)
		}
		if _, _, _, calls := f.writeSnapshot(); calls != 0 {
			t.Fatalf("payload %s reached the mail service", payload)
		}
	}
}

func TestSetFlagsNotFound(t *testing.T) {
	f := &fakeReader{setFlagsFn: func(context.Context, string, mail.FlagPatch) (mail.MessageSummary, error) {
		return mail.MessageSummary{}, mail.ErrNotFound
	}}
	srv := newTestServer(t, f, Options{})

	resp := requestJSON(t, srv, http.MethodPatch, "/v1/messages/msg1_missing", "", `{"read":true}`, &testCreds)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if got := decodeError(t, resp); got.Code != codeNotFound {
		t.Fatalf("error = %+v", got)
	}
}

func TestMessagePathRejectsUnsupportedMethods(t *testing.T) {
	srv := newTestServer(t, &fakeReader{}, Options{})
	resp := request(t, srv, http.MethodPost, "/v1/messages/msg1_a", &testCreds)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	allow := resp.Header.Get("Allow")
	if !strings.Contains(allow, "PATCH") || !strings.Contains(allow, "GET") {
		t.Fatalf("Allow = %q", allow)
	}
}
