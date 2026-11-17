package jmap

import (
	"context"
	"encoding/json"
	"errors"
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

// Identity is one sending identity. The service only needs the identifier and
// the address the mail server will accept as the sender.
type Identity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Identities lists the account's sending identities. Identity/get requires the
// submission capability, which every batch carries.
func (c *Client) Identities(ctx context.Context, creds Credentials, accountID string) ([]Identity, error) {
	raw, err := c.Call(ctx, creds, "Identity/get", map[string]any{"accountId": accountID})
	if err != nil {
		return nil, err
	}
	var out struct {
		List []Identity `json:"list"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: decode Identity/get: %v", ErrUpstream, err)
	}
	return out.List, nil
}

// SetEmailKeywords patches the named keywords and leaves every other keyword
// untouched. The patch is per-keyword on purpose: replacing the map would drop
// $draft, $hasattachment, or a keyword another client set. Setting a keyword to
// false removes it; that is how the server represents an unset flag.
func (c *Client) SetEmailKeywords(ctx context.Context, creds Credentials, accountID, emailID string, read, starred *bool) error {
	update := map[string]any{}
	if read != nil {
		update["keywords/$seen"] = *read
	}
	if starred != nil {
		update["keywords/$flagged"] = *starred
	}
	if len(update) == 0 {
		return nil
	}

	raw, err := c.Call(ctx, creds, "Email/set", map[string]any{
		"accountId": accountID,
		"update":    map[string]any{emailID: update},
	})
	if err != nil {
		return err
	}
	var out struct {
		NotUpdated map[string]struct {
			Type string `json:"type"`
		} `json:"notUpdated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("%w: decode Email/set: %v", ErrUpstream, err)
	}
	if entry, ok := out.NotUpdated[emailID]; ok {
		if entry.Type == "notFound" {
			return ErrNotFound
		}
		// The type is vendor vocabulary; it stays in the error chain for logs
		// and never reaches a response.
		return fmt.Errorf("%w: keyword update refused: %s", ErrUpstream, entry.Type)
	}
	return nil
}

// OutgoingEmail is a plain-text message to create and submit. The mailbox
// identifiers and identity are server identifiers resolved by the caller; the
// sender is the identity's address, never a request value.
type OutgoingEmail struct {
	DraftsMailboxID string
	SentMailboxID   string
	IdentityID      string
	From            Address
	To              []Address
	Cc              []Address
	Subject         string
	Text            string
}

// SendMessage creates the message in the drafts mailbox and submits it in one
// envelope, using onSuccessUpdateEmail to move it to Sent and clear the draft
// keyword. It returns the created email's server identifier. If the message was
// created but the submission failed, it returns that identifier together with
// the error so the caller can compensate; the message is then still a draft and
// has not been sent.
func (c *Client) SendMessage(ctx context.Context, creds Credentials, accountID string, msg OutgoingEmail) (string, error) {
	const (
		createID = "mf-create"
		submitID = "mf-submit"
	)

	create := map[string]any{
		"mailboxIds": map[string]bool{msg.DraftsMailboxID: true},
		// The draft is created read so the sent copy needs no second patch.
		"keywords":   map[string]bool{"$draft": true, "$seen": true},
		"from":       addressObjects([]Address{msg.From}),
		"to":         addressObjects(msg.To),
		"subject":    msg.Subject,
		"textBody":   []map[string]string{{"partId": "text", "type": "text/plain"}},
		"bodyValues": map[string]any{"text": map[string]string{"value": msg.Text}},
	}
	if len(msg.Cc) > 0 {
		create["cc"] = addressObjects(msg.Cc)
	}

	recipients := make([]Address, 0, len(msg.To)+len(msg.Cc))
	recipients = append(recipients, msg.To...)
	recipients = append(recipients, msg.Cc...)
	rcptTo := make([]map[string]string, 0, len(recipients))
	for _, a := range recipients {
		rcptTo = append(rcptTo, map[string]string{"email": a.Email})
	}

	results, err := c.Batch(ctx, creds, []Call{
		{
			Method: "Email/set",
			Args: map[string]any{
				"accountId": accountID,
				"create":    map[string]any{createID: create},
			},
			ClientID: createID,
		},
		{
			Method: "EmailSubmission/set",
			Args: map[string]any{
				"accountId": accountID,
				"create": map[string]any{submitID: map[string]any{
					"identityId": msg.IdentityID,
					"emailId":    "#" + createID,
					"envelope": map[string]any{
						"mailFrom": map[string]string{"email": msg.From.Email},
						"rcptTo":   rcptTo,
					},
				}},
				// Without this the submitted message stays in Drafts. The
				// server applies it only when the submission succeeds.
				"onSuccessUpdateEmail": map[string]any{
					"#" + submitID: map[string]any{
						"mailboxIds/" + msg.DraftsMailboxID: nil,
						"mailboxIds/" + msg.SentMailboxID:   true,
						"keywords/$seen":                    true,
						"keywords/$draft":                   nil,
					},
				},
			},
			ClientID: submitID,
		},
	})
	if err != nil {
		return "", err
	}

	createdID, err := createdEmailID(results, createID)
	if err != nil {
		return "", err
	}

	submission := findResult(results, "EmailSubmission/set", submitID)
	if submission == nil {
		return createdID, fmt.Errorf("%w: submission produced no response", ErrUpstream)
	}
	if submission.Err != nil {
		// A method error is not the observed refusal shape, but it still means
		// no submission happened. Keep the authentication and outage classes so
		// the caller can distinguish them from a refusal.
		if errors.Is(submission.Err, ErrUnauthorized) || errors.Is(submission.Err, ErrUnavailable) || errors.Is(submission.Err, ErrTimeout) {
			return createdID, submission.Err
		}
		return createdID, fmt.Errorf("%w: %v", ErrRejected, submission.Err)
	}

	var out struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]struct {
			Type string `json:"type"`
		} `json:"notCreated"`
	}
	if err := json.Unmarshal(submission.Args, &out); err != nil {
		return createdID, fmt.Errorf("%w: decode EmailSubmission/set: %v", ErrUpstream, err)
	}
	if entry, ok := out.NotCreated[submitID]; ok {
		// The refusal description names the address and is vendor text; only
		// the type is kept, and only for the log.
		return createdID, fmt.Errorf("%w: submission refused (%s)", ErrRejected, entry.Type)
	}
	if _, ok := out.Created[submitID]; !ok {
		return createdID, fmt.Errorf("%w: submission returned no identifier", ErrUpstream)
	}
	return createdID, nil
}

// DestroyEmails removes messages by server identifier. It is used to
// compensate a message that was created but never submitted.
func (c *Client) DestroyEmails(ctx context.Context, creds Credentials, accountID string, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	raw, err := c.Call(ctx, creds, "Email/set", map[string]any{
		"accountId": accountID,
		"destroy":   ids,
	})
	if err != nil {
		return err
	}
	var out struct {
		NotDestroyed map[string]struct {
			Type string `json:"type"`
		} `json:"notDestroyed"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("%w: decode Email/set: %v", ErrUpstream, err)
	}
	for _, id := range ids {
		if entry, ok := out.NotDestroyed[id]; ok {
			return fmt.Errorf("%w: message not removed: %s", ErrUpstream, entry.Type)
		}
	}
	return nil
}

// createdEmailID finds the create result by method and client id. The batch can
// carry an extra Email/set response for onSuccessUpdateEmail, so counting
// responses is not enough.
func createdEmailID(results []Result, clientID string) (string, error) {
	result := findResult(results, "Email/set", clientID)
	if result == nil {
		return "", fmt.Errorf("%w: message create produced no response", ErrUpstream)
	}
	if result.Err != nil {
		return "", result.Err
	}
	var out struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]struct {
			Type string `json:"type"`
		} `json:"notCreated"`
	}
	if err := json.Unmarshal(result.Args, &out); err != nil {
		return "", fmt.Errorf("%w: decode Email/set: %v", ErrUpstream, err)
	}
	if entry, ok := out.NotCreated[clientID]; ok {
		return "", fmt.Errorf("%w: message refused (%s)", ErrRejected, entry.Type)
	}
	created, ok := out.Created[clientID]
	if !ok || created.ID == "" {
		return "", fmt.Errorf("%w: message create returned no identifier", ErrUpstream)
	}
	return created.ID, nil
}

// findResult matches by client id and either the expected method or a method
// error. Matching on both matters because a successful submission produces an
// extra Email/set response with the submission's client id.
func findResult(results []Result, method, clientID string) *Result {
	for i := range results {
		if results[i].ClientID != clientID {
			continue
		}
		if results[i].Method == method || results[i].Method == "error" {
			return &results[i]
		}
	}
	return nil
}

func addressObjects(list []Address) []map[string]string {
	out := make([]map[string]string, 0, len(list))
	for _, a := range list {
		obj := map[string]string{"email": a.Email}
		if a.Name != "" {
			obj["name"] = a.Name
		}
		out = append(out, obj)
	}
	return out
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
