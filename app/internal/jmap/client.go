// Package jmap is the only place that knows the mail server's protocol.
//
// It speaks JMAP to the configured endpoint, forwards the caller's
// credentials, and returns wire types. It does not translate anything into
// Mailflow vocabulary and it does not know about HTTP handlers; that boundary
// is deliberate so vendor detail cannot leak past this package.
package jmap

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// JMAP capabilities the service uses.
const (
	CapabilityCore = "urn:ietf:params:jmap:core"
	CapabilityMail = "urn:ietf:params:jmap:mail"
)

// Errors are the only failure classes this package exposes. Callers map them
// to their own vocabulary; the underlying transport error stays attached for
// logs but never for responses.
var (
	ErrUnauthorized   = errors.New("mail server rejected the credentials")
	ErrUnavailable    = errors.New("mail server unavailable")
	ErrTimeout        = errors.New("mail server did not answer in time")
	ErrUpstream       = errors.New("mail server rejected the request")
	ErrAnchorNotFound = errors.New("anchor not found in query results")
)

// Credentials are forwarded from the caller. They are never stored.
type Credentials struct {
	User     string
	Password string
}

// Config is the validated connection configuration.
type Config struct {
	URL      *url.URL
	Hostname string
	CAFile   string
	Timeout  time.Duration
}

// Client is a JMAP connection pool bound to one mail server.
type Client struct {
	apiURL     *url.URL
	sessionURL *url.URL
	http       *http.Client
}

// New builds a client that trusts only the configured CA and verifies the
// mail server's certificate against the configured hostname, even when the
// URL names a container or an address.
func New(cfg Config) (*Client, error) {
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("CA file %s contains no certificates", cfg.CAFile)
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: cfg.Timeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: cfg.Hostname,
			MinVersion: tls.VersionTLS12,
		},
	}

	sessionURL := *cfg.URL
	sessionURL.Path = strings.TrimSuffix(cfg.URL.Path, "/") + "/session"
	sessionURL.RawQuery = ""
	sessionURL.Fragment = ""

	return &Client{
		apiURL:     cfg.URL,
		sessionURL: &sessionURL,
		http: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}, nil
}

// Session is the part of the JMAP session document the service uses.
type Session struct {
	APIURL          string            `json:"apiUrl"`
	PrimaryAccounts map[string]string `json:"primaryAccounts"`
}

// MailAccount returns the account the credentials may read mail from. It is
// server-assigned and opaque; the service never derives it from an address.
func (s Session) MailAccount() (string, error) {
	account := s.PrimaryAccounts[CapabilityMail]
	if account == "" {
		return "", errors.New("session does not name a mail account")
	}
	return account, nil
}

// FetchSession reads the session document for these credentials.
func (c *Client) FetchSession(ctx context.Context, creds Credentials) (Session, error) {
	var session Session
	status, body, err := c.do(ctx, creds, http.MethodGet, c.sessionURL, nil)
	if err != nil {
		return session, err
	}
	if status != http.StatusOK {
		return session, statusError(status)
	}
	if err := json.Unmarshal(body, &session); err != nil {
		return session, fmt.Errorf("%w: decode session: %v", ErrUpstream, err)
	}
	return session, nil
}

// Call sends one method call and returns its arguments.
func (c *Client) Call(ctx context.Context, creds Credentials, method string, args any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"using":       []string{CapabilityCore, CapabilityMail},
		"methodCalls": [][]any{{method, args, "c"}},
	})
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", method, err)
	}

	status, body, err := c.do(ctx, creds, http.MethodPost, c.apiURL, payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, statusError(status)
	}

	var envelope struct {
		MethodResponses [][]json.RawMessage `json:"methodResponses"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decode %s response: %v", ErrUpstream, method, err)
	}
	if len(envelope.MethodResponses) != 1 {
		return nil, fmt.Errorf("%w: %s returned %d responses", ErrUpstream, method, len(envelope.MethodResponses))
	}
	// A method response is a [name, arguments, clientId] triple. A short one is a
	// malformed upstream answer, not something to index into and panic on.
	response := envelope.MethodResponses[0]
	if len(response) < 2 {
		return nil, fmt.Errorf("%w: %s returned a malformed response", ErrUpstream, method)
	}

	var name string
	if err := json.Unmarshal(response[0], &name); err != nil {
		return nil, fmt.Errorf("%w: decode %s response name: %v", ErrUpstream, method, err)
	}
	if name == "error" {
		return nil, methodError(response[1])
	}
	if name != method {
		return nil, fmt.Errorf("%w: asked for %s, got %s", ErrUpstream, method, name)
	}
	return response[1], nil
}

// methodError maps a JMAP method error without keeping its description.
func methodError(raw json.RawMessage) error {
	var e struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &e)
	switch e.Type {
	case "anchorNotFound":
		return ErrAnchorNotFound
	case "forbidden", "accountNotFound":
		return ErrUnauthorized
	default:
		return fmt.Errorf("%w: %s", ErrUpstream, e.Type)
	}
}

func statusError(status int) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrUnauthorized
	case status == http.StatusTooManyRequests || status >= 500:
		return fmt.Errorf("%w: status %d", ErrUnavailable, status)
	default:
		return fmt.Errorf("%w: status %d", ErrUpstream, status)
	}
}

// maxResponseBytes bounds one upstream response. A body larger than this is
// treated as an upstream failure rather than silently truncated into invalid
// JSON.
const maxResponseBytes = 8 << 20

// do performs one HTTP exchange with the caller's credentials and a bounded
// read. It classifies transport failures so callers never see raw detail.
func (c *Client) do(ctx context.Context, creds Credentials, method string, u *url.URL, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(creds.User, creds.Password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, classify(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read one byte past the limit so an oversized body is detected instead of
	// looking like a short, unparsable response.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, classify(err)
	}
	if len(data) > maxResponseBytes {
		return 0, nil, fmt.Errorf("%w: response exceeds %d bytes", ErrUpstream, maxResponseBytes)
	}
	return resp.StatusCode, data, nil
}

func classify(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
