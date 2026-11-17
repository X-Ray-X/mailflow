package jmap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testHostname = "mail.test"

// testCA is a freshly generated certificate authority. Each test server gets
// its own, so a client trusting a different CA must fail.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mailflow test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}
	return &testCA{
		cert: cert,
		key:  key,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (ca *testCA) writeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, ca.pem, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	return path
}

func (ca *testCA) serverCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: testHostname},
		DNSNames:     []string{testHostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}
	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	return pair
}

// startServer runs an HTTPS server whose certificate covers testHostname only.
// The URL names 127.0.0.1, so a successful call proves the client uses the
// configured server name rather than the URL host.
func startServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *testCA) {
	t.Helper()
	ca := newTestCA(t)
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{ca.serverCert(t)}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, ca
}

func clientFor(t *testing.T, srvURL, caFile string, timeout time.Duration) *Client {
	t.Helper()
	u, err := url.Parse(srvURL + "/jmap/")
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	c, err := New(Config{URL: u, Hostname: testHostname, CAFile: caFile, Timeout: timeout})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// capture records the last request the test server saw.
type capture struct {
	mu     sync.Mutex
	method string
	path   string
	auth   string
	body   []byte
}

func (c *capture) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.method = r.Method
	c.path = r.URL.Path
	c.auth = r.Header.Get("Authorization")
	c.body = body
}

func (c *capture) get() (method, path, auth string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.path, c.auth, c.body
}

func TestNewRejectsCAWithoutCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	u, _ := url.Parse("https://127.0.0.1/jmap/")
	if _, err := New(Config{URL: u, Hostname: testHostname, CAFile: path, Timeout: time.Second}); err == nil {
		t.Fatal("New accepted a CA file with no certificates")
	}
}

func TestFetchSessionResolvesMailAccount(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"apiUrl":"https://mail.test/jmap/","primaryAccounts":{"urn:ietf:params:jmap:mail":"acct-1"}}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	session, err := c.FetchSession(context.Background(), Credentials{User: "u@example.test", Password: "p"})
	if err != nil {
		t.Fatalf("FetchSession: %v", err)
	}
	account, err := session.MailAccount()
	if err != nil {
		t.Fatalf("MailAccount: %v", err)
	}
	if account != "acct-1" {
		t.Fatalf("account = %q, want acct-1", account)
	}
}

func TestFetchSessionWithoutMailAccount(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"apiUrl":"https://mail.test/jmap/","primaryAccounts":{}}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	session, err := c.FetchSession(context.Background(), Credentials{User: "u", Password: "p"})
	if err != nil {
		t.Fatalf("FetchSession: %v", err)
	}
	if _, err := session.MailAccount(); err == nil {
		t.Fatal("MailAccount succeeded without a mail account")
	}
}

func TestFetchSessionUnauthorized(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("vendor says no"))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	_, err := c.FetchSession(context.Background(), Credentials{User: "u", Password: "wrong"})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "vendor says no") {
		t.Fatalf("error leaks the response body: %v", err)
	}
}

func TestCallForwardsCredentialsAndMethod(t *testing.T) {
	var cap capture
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/query",{"ids":["e1"]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	raw, err := c.Call(context.Background(), Credentials{User: "u@example.test", Password: "secret"}, "Email/query", map[string]any{"accountId": "a"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(raw) != `{"ids":["e1"]}` {
		t.Fatalf("raw = %s", raw)
	}

	method, path, auth, body := cap.get()
	if method != http.MethodPost || path != "/jmap/" {
		t.Fatalf("request = %s %s, want POST /jmap/", method, path)
	}
	if !strings.HasPrefix(auth, "Basic ") {
		t.Fatalf("Authorization = %q, want Basic", auth)
	}
	var sent struct {
		Using       []string            `json:"using"`
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if len(sent.Using) != 3 || sent.Using[1] != CapabilityMail || sent.Using[2] != CapabilitySubmission {
		t.Fatalf("using = %v", sent.Using)
	}
	if len(sent.MethodCalls) != 1 || len(sent.MethodCalls[0]) != 3 {
		t.Fatalf("methodCalls = %s", body)
	}
	var name string
	_ = json.Unmarshal(sent.MethodCalls[0][0], &name)
	if name != "Email/query" {
		t.Fatalf("method = %q", name)
	}
}

func TestCallMapsAnchorNotFound(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["error",{"type":"anchorNotFound","description":"gone"},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrAnchorNotFound) {
		t.Fatalf("error = %v, want ErrAnchorNotFound", err)
	}
}

func TestCallMethodErrorKeepsDescriptionOut(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["error",{"type":"invalidArguments","description":"internal vendor detail"},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if strings.Contains(err.Error(), "internal vendor detail") {
		t.Fatalf("error carries the vendor description: %v", err)
	}
}

func TestCallTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })
	c := clientFor(t, srv.URL, ca.writeFile(t), 100*time.Millisecond)

	start := time.Now()
	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("call took %v, want it bounded by the timeout", elapsed)
	}
}

func TestCallUnreachable(t *testing.T) {
	ca := newTestCA(t)
	c := clientFor(t, "https://127.0.0.1:1", ca.writeFile(t), 500*time.Millisecond)

	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

// A malformed upstream answer must be an error, not an index out of range.
func TestCallRejectsShortMethodResponse(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/query"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

// A response past the bound must fail loudly rather than be read as truncated
// JSON.
func TestCallRejectsOversizedResponse(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for i := 0; i < 9; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 10*time.Second)

	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want it to name the size bound", err)
	}
}

func TestCallRejectsUntrustedCA(t *testing.T) {
	srv, _ := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[]}`))
	})
	otherCA := newTestCA(t)
	c := clientFor(t, srv.URL, otherCA.writeFile(t), 2*time.Second)

	_, err := c.Call(context.Background(), Credentials{User: "u", Password: "p"}, "Email/query", map[string]any{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable for an untrusted certificate", err)
	}
}

func TestMailboxesParsesWireShape(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/get",{"list":[
			{"id":"mb-inbox","name":"Inbox","role":"inbox","totalEmails":4,"unreadEmails":3},
			{"id":"mb-custom","name":"Receipts","role":null,"totalEmails":0,"unreadEmails":0}
		]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	list, err := c.Mailboxes(context.Background(), Credentials{User: "u", Password: "p"}, "acct")
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d mailboxes, want 2", len(list))
	}
	if list[0].Role != "inbox" || list[0].UnreadEmails != 3 {
		t.Fatalf("first mailbox = %+v", list[0])
	}
	if list[1].Role != "" {
		t.Fatalf("custom mailbox role = %q, want empty", list[1].Role)
	}
}

func TestMailboxExists(t *testing.T) {
	var present atomic.Bool
	var cap capture
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		if present.Load() {
			_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/get",{"list":[{"id":"mb1"}],"notFound":[]},"c"]]}`))
			return
		}
		_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/get",{"list":[],"notFound":["mb1"]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	exists, err := c.MailboxExists(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "mb1")
	if err != nil {
		t.Fatalf("MailboxExists: %v", err)
	}
	if exists {
		t.Fatal("MailboxExists reported a mailbox the server put in notFound")
	}

	present.Store(true)
	exists, err = c.MailboxExists(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "mb1")
	if err != nil {
		t.Fatalf("MailboxExists: %v", err)
	}
	if !exists {
		t.Fatal("MailboxExists missed a mailbox the server returned")
	}
}

func TestQueryEmailsSendsCursorAndTotals(t *testing.T) {
	var cap capture
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/query",{"ids":["e1","e2"],"total":4,"position":2},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	result, err := c.QueryEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		Query{MailboxID: "mb1", Limit: 2, Anchor: "e0"})
	if err != nil {
		t.Fatalf("QueryEmails: %v", err)
	}
	if result.Total != 4 || result.Position != 2 || len(result.IDs) != 2 {
		t.Fatalf("result = %+v", result)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var args struct {
		Limit          int               `json:"limit"`
		Anchor         string            `json:"anchor"`
		AnchorOffset   *int              `json:"anchorOffset"`
		CalculateTotal bool              `json:"calculateTotal"`
		Filter         map[string]string `json:"filter"`
		Sort           []struct {
			Property    string `json:"property"`
			IsAscending bool   `json:"isAscending"`
		} `json:"sort"`
	}
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if args.Anchor != "e0" || args.AnchorOffset == nil || *args.AnchorOffset != 1 {
		t.Fatalf("cursor args = anchor %q offset %v, want e0/1", args.Anchor, args.AnchorOffset)
	}
	if !args.CalculateTotal {
		t.Fatal("calculateTotal is off, so hasMore cannot be computed")
	}
	if args.Filter["inMailbox"] != "mb1" {
		t.Fatalf("filter = %v", args.Filter)
	}
	if len(args.Sort) != 1 || args.Sort[0].Property != "receivedAt" || args.Sort[0].IsAscending {
		t.Fatalf("sort = %+v, want receivedAt descending", args.Sort)
	}
}

func TestQueryEmailsFirstPageHasNoAnchor(t *testing.T) {
	var cap capture
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/query",{"ids":[],"total":0,"position":0},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	if _, err := c.QueryEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		Query{MailboxID: "mb1", Limit: 2}); err != nil {
		t.Fatalf("QueryEmails: %v", err)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if _, ok := args["anchor"]; ok {
		t.Fatal("first page sent an anchor")
	}
}

func TestGetEmailsRequestsBodyExplicitly(t *testing.T) {
	var cap capture
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"list":[{
			"id":"e1","subject":"Hi","from":[{"name":"Alice","email":"alice@example.test"}],
			"to":[{"name":"Bob","email":"bob@example.test"}],"receivedAt":"2026-11-15T21:00:00Z",
			"keywords":{"$seen":true},"hasAttachment":false,
			"textBody":[{"partId":"1","type":"text/plain"}],
			"bodyValues":{"1":{"value":"hello","isTruncated":false}}
		}],"notFound":[]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	list, err := c.GetEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		GetEmailsOptions{IDs: []string{"e1"}, IncludeBody: true, MaxBodyBytes: 1024})
	if err != nil {
		t.Fatalf("GetEmails: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d messages", len(list))
	}
	if list[0].BodyValues["1"].Value != "hello" {
		t.Fatalf("body = %+v", list[0].BodyValues)
	}
	if !list[0].ReceivedAt.Equal(time.Date(2026, 11, 15, 21, 0, 0, 0, time.UTC)) {
		t.Fatalf("receivedAt = %v", list[0].ReceivedAt)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var args struct {
		Properties          []string `json:"properties"`
		FetchTextBodyValues bool     `json:"fetchTextBodyValues"`
		MaxBodyValueBytes   int64    `json:"maxBodyValueBytes"`
	}
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	want := map[string]bool{"bodyValues": false, "textBody": false}
	for _, p := range args.Properties {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for property, found := range want {
		if !found {
			t.Errorf("properties missing %q: %v", property, args.Properties)
		}
	}
	if !args.FetchTextBodyValues || args.MaxBodyValueBytes != 1024 {
		t.Fatalf("body args = %+v", args)
	}
}

func TestGetEmailsOmitsBodyForList(t *testing.T) {
	var cap capture
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"list":[],"notFound":[]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	if _, err := c.GetEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		GetEmailsOptions{IDs: []string{"e1"}}); err != nil {
		t.Fatalf("GetEmails: %v", err)
	}

	_, _, _, body := cap.get()
	if strings.Contains(string(body), "bodyValues") {
		t.Fatalf("list request asked for bodies: %s", body)
	}
}
