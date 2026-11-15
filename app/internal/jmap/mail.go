package jmap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Mailbox is the wire shape of a JMAP mailbox, reduced to the properties the
// service asks for.
type Mailbox struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	TotalEmails  int    `json:"totalEmails"`
	UnreadEmails int    `json:"unreadEmails"`
	SortOrder    int    `json:"sortOrder"`
}

// Address is one JMAP address object.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// BodyPart points at one part of a message body.
type BodyPart struct {
	PartID      string `json:"partId"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Disposition string `json:"disposition"`
}

// BodyValue holds one fetched body part.
type BodyValue struct {
	Value             string `json:"value"`
	IsTruncated       bool   `json:"isTruncated"`
	IsEncodingProblem bool   `json:"isEncodingProblem"`
}

// Email is the wire shape of a JMAP email.
type Email struct {
	ID            string               `json:"id"`
	Subject       string               `json:"subject"`
	From          []Address            `json:"from"`
	To            []Address            `json:"to"`
	Cc            []Address            `json:"cc"`
	ReceivedAt    time.Time            `json:"receivedAt"`
	Keywords      map[string]bool      `json:"keywords"`
	HasAttachment bool                 `json:"hasAttachment"`
	TextBody      []BodyPart           `json:"textBody"`
	BodyValues    map[string]BodyValue `json:"bodyValues"`
}

// Query describes one page request. Anchor is a mail server identifier; the
// service only ever passes one back that it received from the server.
type Query struct {
	MailboxID string
	Limit     int
	Anchor    string
}

// QueryResult is one page of query results. Total and Position let the caller
// decide whether another page exists without guessing from the page length.
type QueryResult struct {
	IDs      []string `json:"ids"`
	Total    int      `json:"total"`
	Position int      `json:"position"`
}

// Mailboxes lists every mailbox of the account.
func (c *Client) Mailboxes(ctx context.Context, creds Credentials, accountID string) ([]Mailbox, error) {
	raw, err := c.Call(ctx, creds, "Mailbox/get", map[string]any{
		"accountId":  accountID,
		"properties": []string{"id", "name", "role", "totalEmails", "unreadEmails", "sortOrder"},
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		List []Mailbox `json:"list"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: decode Mailbox/get: %v", ErrUpstream, err)
	}
	return out.List, nil
}

// MailboxExists reports whether the account owns this mailbox. The mail server
// answers a query for an unknown mailbox with an empty result rather than an
// error, so existence has to be asked for directly.
func (c *Client) MailboxExists(ctx context.Context, creds Credentials, accountID, mailboxID string) (bool, error) {
	raw, err := c.Call(ctx, creds, "Mailbox/get", map[string]any{
		"accountId":  accountID,
		"ids":        []string{mailboxID},
		"properties": []string{"id"},
	})
	if err != nil {
		return false, err
	}
	var out struct {
		List     []Mailbox `json:"list"`
		NotFound []string  `json:"notFound"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, fmt.Errorf("%w: decode Mailbox/get: %v", ErrUpstream, err)
	}
	return len(out.List) == 1, nil
}

// QueryEmails returns one page of message identifiers, newest first.
func (c *Client) QueryEmails(ctx context.Context, creds Credentials, accountID string, q Query) (QueryResult, error) {
	args := map[string]any{
		"accountId":      accountID,
		"filter":         map[string]any{"inMailbox": q.MailboxID},
		"sort":           []map[string]any{{"property": "receivedAt", "isAscending": false}},
		"limit":          q.Limit,
		"calculateTotal": true,
	}
	if q.Anchor != "" {
		// anchorOffset 1 starts after the anchor, so a page never repeats the
		// message the cursor was built from.
		args["anchor"] = q.Anchor
		args["anchorOffset"] = 1
	}

	raw, err := c.Call(ctx, creds, "Email/query", args)
	if err != nil {
		return QueryResult{}, err
	}
	var out QueryResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return QueryResult{}, fmt.Errorf("%w: decode Email/query: %v", ErrUpstream, err)
	}
	return out, nil
}

// GetEmailsOptions selects what to fetch. The property list is always
// explicit: the default response carries identifiers and vendor detail the
// public contract must not see.
type GetEmailsOptions struct {
	IDs          []string
	IncludeBody  bool
	MaxBodyBytes int64
}

// GetEmails fetches messages by identifier, in the order the server returns
// them. Unknown identifiers are omitted rather than reported.
func (c *Client) GetEmails(ctx context.Context, creds Credentials, accountID string, opts GetEmailsOptions) ([]Email, error) {
	properties := []string{"id", "subject", "from", "receivedAt", "keywords", "hasAttachment"}
	args := map[string]any{
		"accountId":  accountID,
		"ids":        opts.IDs,
		"properties": properties,
	}
	if opts.IncludeBody {
		args["properties"] = append(properties, "to", "cc", "textBody", "bodyValues")
		args["bodyProperties"] = []string{"partId", "type", "name", "disposition"}
		args["fetchTextBodyValues"] = true
		args["maxBodyValueBytes"] = opts.MaxBodyBytes
	}

	raw, err := c.Call(ctx, creds, "Email/get", args)
	if err != nil {
		return nil, err
	}
	var out struct {
		List     []Email  `json:"list"`
		NotFound []string `json:"notFound"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: decode Email/get: %v", ErrUpstream, err)
	}
	return out.List, nil
}
