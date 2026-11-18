package jmap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCreateDraftWritesHeadersAndRecipients(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","created":{"mf-create":{"id":"e1"}}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	id, err := c.CreateDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct", DraftContent{
		DraftsMailboxID: "d",
		From:            Address{Name: "Alice", Email: "alice@example.test"},
		To:              []Address{{Email: "bob@example.test"}},
		Cc:              []Address{{Email: "carol@example.test"}},
		Bcc:             []Address{{Email: "dave@example.test"}},
		Subject:         "Re: Lunch",
		Text:            "Sounds good.",
		InReplyTo:       []string{"orig@example.test"},
		References:      []string{"first@example.test", "orig@example.test"},
	})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if id != "e1" {
		t.Fatalf("id = %q", id)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var method string
	var args struct {
		AccountID string                     `json:"accountId"`
		Create    map[string]json.RawMessage `json:"create"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][0], &method)
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if method != "Email/set" || args.AccountID != "acct" {
		t.Fatalf("call = %s %+v", method, args)
	}

	var draft struct {
		MailboxIDs map[string]bool     `json:"mailboxIds"`
		Keywords   map[string]bool     `json:"keywords"`
		From       []map[string]string `json:"from"`
		To         []map[string]string `json:"to"`
		Cc         []map[string]string `json:"cc"`
		Bcc        []map[string]string `json:"bcc"`
		Subject    string              `json:"subject"`
		TextBody   []map[string]string `json:"textBody"`
		BodyValues map[string]struct {
			Value string `json:"value"`
		} `json:"bodyValues"`
		InReplyTo  string `json:"header:In-Reply-To:asText"`
		References string `json:"header:References:asText"`
	}
	if err := json.Unmarshal(args.Create["mf-create"], &draft); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !draft.MailboxIDs["d"] || !draft.Keywords["$draft"] || len(draft.MailboxIDs) != 1 {
		t.Fatalf("placement = %+v", draft)
	}
	if len(draft.From) != 1 || draft.From[0]["email"] != "alice@example.test" || draft.From[0]["name"] != "Alice" {
		t.Fatalf("from = %+v", draft.From)
	}
	if len(draft.To) != 1 || draft.To[0]["email"] != "bob@example.test" {
		t.Fatalf("to = %+v", draft.To)
	}
	if len(draft.Cc) != 1 || draft.Cc[0]["email"] != "carol@example.test" {
		t.Fatalf("cc = %+v", draft.Cc)
	}
	if len(draft.Bcc) != 1 || draft.Bcc[0]["email"] != "dave@example.test" {
		t.Fatalf("bcc = %+v", draft.Bcc)
	}
	if draft.Subject != "Re: Lunch" || len(draft.TextBody) != 1 || draft.TextBody[0]["type"] != "text/plain" {
		t.Fatalf("content = %+v", draft)
	}
	if draft.BodyValues["text"].Value != "Sounds good." {
		t.Fatalf("body = %+v", draft.BodyValues)
	}
	if draft.InReplyTo != "<orig@example.test>" {
		t.Fatalf("inReplyTo header = %q", draft.InReplyTo)
	}
	if draft.References != "<first@example.test> <orig@example.test>" {
		t.Fatalf("references header = %q", draft.References)
	}
}

func TestCreateDraftOmitsEmptyRecipientsAndThreading(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","created":{"mf-create":{"id":"e1"}}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	if _, err := c.CreateDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct", DraftContent{
		DraftsMailboxID: "d",
		From:            Address{Email: "alice@example.test"},
	}); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	_ = json.Unmarshal(body, &sent)
	var args struct {
		Create map[string]json.RawMessage `json:"create"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][1], &args)
	var draft map[string]any
	if err := json.Unmarshal(args.Create["mf-create"], &draft); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	for _, key := range []string{"to", "cc", "bcc", "header:In-Reply-To:asText", "header:References:asText"} {
		if _, ok := draft[key]; ok {
			t.Fatalf("empty draft carries %q: %+v", key, draft)
		}
	}
}

func TestCreateDraftMapsRefusals(t *testing.T) {
	cases := map[string]error{
		"invalidProperties": ErrInvalidInput,
		"notFound":          ErrNotFound,
		"serverBug":         ErrUpstream,
	}
	for vendorType, want := range cases {
		t.Run(vendorType, func(t *testing.T) {
			srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","notCreated":{"mf-create":{"type":"` + vendorType + `"}}},"c"]]}`))
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			_, err := c.CreateDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct", DraftContent{DraftsMailboxID: "d"})
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestCreateDraftRequiresAnIdentifier(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","created":{}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	if _, err := c.CreateDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct", DraftContent{DraftsMailboxID: "d"}); !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestSendDraftSubmitsTheExistingMessage(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[
			["EmailSubmission/set",{"accountId":"a","created":{"mf-submit":{"id":"s1"}}},"mf-submit"],
			["Email/set",{"accountId":"a","updated":{"e1":null}},"mf-submit"]
		]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	err := c.SendDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		"d", "s", "ident", "e1",
		Address{Email: "alice@example.test"},
		[]Address{{Email: "bob@example.test"}, {Email: "dave@example.test"}},
	)
	if err != nil {
		t.Fatalf("SendDraft: %v", err)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(sent.MethodCalls) != 1 {
		t.Fatalf("methodCalls = %d, want one submission", len(sent.MethodCalls))
	}
	var method string
	var args struct {
		Create map[string]struct {
			IdentityID string `json:"identityId"`
			EmailID    string `json:"emailId"`
			Envelope   struct {
				MailFrom map[string]string   `json:"mailFrom"`
				RcptTo   []map[string]string `json:"rcptTo"`
			} `json:"envelope"`
		} `json:"create"`
		OnSuccessUpdateEmail map[string]map[string]any `json:"onSuccessUpdateEmail"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][0], &method)
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if method != "EmailSubmission/set" {
		t.Fatalf("call = %s", method)
	}
	submission := args.Create["mf-submit"]
	if submission.IdentityID != "ident" || submission.EmailID != "e1" {
		t.Fatalf("submission = %+v, want the existing identifier", submission)
	}
	if submission.Envelope.MailFrom["email"] != "alice@example.test" {
		t.Fatalf("mailFrom = %+v", submission.Envelope.MailFrom)
	}
	if len(submission.Envelope.RcptTo) != 2 ||
		submission.Envelope.RcptTo[0]["email"] != "bob@example.test" ||
		submission.Envelope.RcptTo[1]["email"] != "dave@example.test" {
		t.Fatalf("rcptTo = %+v", submission.Envelope.RcptTo)
	}
	update := args.OnSuccessUpdateEmail["#mf-submit"]
	if update["mailboxIds/d"] != nil || update["mailboxIds/s"] != true ||
		update["keywords/$draft"] != nil || update["keywords/$seen"] != true {
		t.Fatalf("success update = %+v", update)
	}
}

func TestSendDraftMapsRefusals(t *testing.T) {
	cases := map[string]error{
		"noRecipients":      ErrInvalidInput,
		"invalidProperties": ErrRejected,
		"forbiddenFrom":     ErrRejected,
	}
	for vendorType, want := range cases {
		t.Run(vendorType, func(t *testing.T) {
			srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"methodResponses":[["EmailSubmission/set",{"accountId":"a","notCreated":{"mf-submit":{
					"type":"` + vendorType + `","description":"refused for bob@example.test"}}},"mf-submit"]]}`))
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			err := c.SendDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
				"d", "s", "ident", "e1", Address{Email: "alice@example.test"}, nil)
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if strings.Contains(err.Error(), "bob@example.test") {
				t.Fatalf("error carries the vendor description: %v", err)
			}
		})
	}
}

func TestSendDraftRequiresACreatedSubmission(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["EmailSubmission/set",{"accountId":"a","created":{}},"mf-submit"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	err := c.SendDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		"d", "s", "ident", "e1", Address{Email: "alice@example.test"}, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestGetDraftReadsRecipientsAndBody(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"accountId":"a","list":[{
			"id":"e1","subject":"Re: Lunch","from":[{"email":"alice@example.test"}],
			"to":[{"email":"bob@example.test"}],"cc":[],"bcc":[{"email":"dave@example.test"}],
			"keywords":{"$draft":true},"mailboxIds":{"d":true},
			"textBody":[{"partId":"text","type":"text/plain"}],
			"bodyValues":{"text":{"value":"Sounds good."}}
		}]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	draft, err := c.GetDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1", 4096)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if draft.ID != "e1" || !draft.Keywords["$draft"] || !draft.MailboxIDs["d"] {
		t.Fatalf("draft = %+v", draft)
	}
	if len(draft.Bcc) != 1 || draft.Bcc[0].Email != "dave@example.test" {
		t.Fatalf("bcc = %+v", draft.Bcc)
	}
	if got := draft.BodyValues["text"].Value; got != "Sounds good." {
		t.Fatalf("body = %q", got)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	_ = json.Unmarshal(body, &sent)
	var args struct {
		IDs                 []string `json:"ids"`
		Properties          []string `json:"properties"`
		FetchTextBodyValues bool     `json:"fetchTextBodyValues"`
		MaxBodyValueBytes   int64    `json:"maxBodyValueBytes"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][1], &args)
	if len(args.IDs) != 1 || args.IDs[0] != "e1" {
		t.Fatalf("ids = %v", args.IDs)
	}
	for _, want := range []string{"bcc", "keywords", "mailboxIds", "textBody", "bodyValues"} {
		if !containsString(args.Properties, want) {
			t.Fatalf("properties = %v, missing %q", args.Properties, want)
		}
	}
	if !args.FetchTextBodyValues || args.MaxBodyValueBytes != 4096 {
		t.Fatalf("body fetch = %+v", args)
	}
}

func TestGetDraftReportsAnUnknownIdentifier(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"accountId":"a","list":[]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	if _, err := c.GetDraft(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "gone", 4096); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestGetReplySourceReadsThreadingWithoutABody(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"accountId":"a","list":[{
			"id":"e1","subject":"Lunch","from":[{"email":"bob@example.test"}],
			"messageId":["orig@example.test"],
			"inReplyTo":["first@example.test"],
			"references":["first@example.test","orig@example.test"]
		}]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	source, err := c.GetReplySource(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1")
	if err != nil {
		t.Fatalf("GetReplySource: %v", err)
	}
	if len(source.MessageID) != 1 || source.MessageID[0] != "orig@example.test" {
		t.Fatalf("messageId = %v", source.MessageID)
	}
	if len(source.InReplyTo) != 1 || source.InReplyTo[0] != "first@example.test" {
		t.Fatalf("inReplyTo = %v", source.InReplyTo)
	}
	if len(source.References) != 2 || source.References[1] != "orig@example.test" {
		t.Fatalf("references = %v", source.References)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	_ = json.Unmarshal(body, &sent)
	var args map[string]any
	_ = json.Unmarshal(sent.MethodCalls[0][1], &args)
	if _, ok := args["fetchTextBodyValues"]; ok {
		t.Fatalf("reply source fetched a body: %+v", args)
	}
	properties, _ := args["properties"].([]any)
	for _, want := range []string{"messageId", "inReplyTo", "references"} {
		if !containsAny(properties, want) {
			t.Fatalf("properties = %v, missing %q", properties, want)
		}
	}
}

func TestGetReplySourceReportsAnUnknownIdentifier(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"accountId":"a","list":[]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	if _, err := c.GetReplySource(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestMessageIDListBracketsAndDropsBlanks(t *testing.T) {
	got := messageIDList([]string{" a@example.test ", "", "b@example.test"})
	if want := "<a@example.test> <b@example.test>"; got != want {
		t.Fatalf("messageIDList = %q, want %q", got, want)
	}
}

func containsString(list []string, want string) bool {
	for _, value := range list {
		if value == want {
			return true
		}
	}
	return false
}

func containsAny(list []any, want string) bool {
	for _, value := range list {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}
