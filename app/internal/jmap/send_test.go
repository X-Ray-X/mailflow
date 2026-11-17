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

func TestSendMessageUsesOneEnvelope(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[
			["Email/set",{"accountId":"a","created":{"mf-create":{"id":"e1"}}},"mf-create"],
			["EmailSubmission/set",{"accountId":"a","created":{"mf-submit":{"id":"s1"}}},"mf-submit"],
			["Email/set",{"accountId":"a","updated":{"e1":null}},"mf-submit"]
		]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	id, err := c.SendMessage(context.Background(), Credentials{User: "alice@example.test", Password: "p"}, "acct", OutgoingEmail{
		DraftsMailboxID: "d",
		SentMailboxID:   "s",
		IdentityID:      "ident",
		From:            Address{Name: "Alice", Email: "alice@example.test"},
		To:              []Address{{Name: "Bob", Email: "bob@example.test"}},
		Cc:              []Address{{Email: "carol@example.test"}},
		Subject:         "Lunch",
		Text:            "Noon?",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != "e1" {
		t.Fatalf("id = %q, want the created email", id)
	}

	_, _, _, body := cap.get()
	var sent struct {
		Using       []string            `json:"using"`
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(sent.Using) != 3 || sent.Using[2] != CapabilitySubmission {
		t.Fatalf("using = %v", sent.Using)
	}
	if len(sent.MethodCalls) != 2 {
		t.Fatalf("methodCalls = %d, want create and submission", len(sent.MethodCalls))
	}

	var createMethod string
	var createArgs struct {
		AccountID string                     `json:"accountId"`
		Create    map[string]json.RawMessage `json:"create"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][0], &createMethod)
	_ = json.Unmarshal(sent.MethodCalls[0][1], &createArgs)
	if createMethod != "Email/set" || createArgs.AccountID != "acct" {
		t.Fatalf("first call = %s %+v", createMethod, createArgs)
	}
	var email struct {
		MailboxIDs map[string]bool     `json:"mailboxIds"`
		Keywords   map[string]bool     `json:"keywords"`
		From       []map[string]string `json:"from"`
		To         []map[string]string `json:"to"`
		Cc         []map[string]string `json:"cc"`
		Subject    string              `json:"subject"`
		TextBody   []map[string]string `json:"textBody"`
		BodyValues map[string]struct {
			Value string `json:"value"`
		} `json:"bodyValues"`
	}
	if err := json.Unmarshal(createArgs.Create["mf-create"], &email); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !email.MailboxIDs["d"] || !email.Keywords["$draft"] || !email.Keywords["$seen"] {
		t.Fatalf("create placement = %+v", email)
	}
	if len(email.From) != 1 || email.From[0]["email"] != "alice@example.test" || email.From[0]["name"] != "Alice" {
		t.Fatalf("from = %+v", email.From)
	}
	if len(email.To) != 1 || email.To[0]["email"] != "bob@example.test" {
		t.Fatalf("to = %+v", email.To)
	}
	if len(email.Cc) != 1 || email.Cc[0]["email"] != "carol@example.test" {
		t.Fatalf("cc = %+v", email.Cc)
	}
	if email.Subject != "Lunch" || len(email.TextBody) != 1 || email.TextBody[0]["type"] != "text/plain" {
		t.Fatalf("message = %+v", email)
	}
	if email.BodyValues["text"].Value != "Noon?" {
		t.Fatalf("body = %+v", email.BodyValues)
	}

	var submitMethod string
	var submitArgs struct {
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
	_ = json.Unmarshal(sent.MethodCalls[1][0], &submitMethod)
	_ = json.Unmarshal(sent.MethodCalls[1][1], &submitArgs)
	if submitMethod != "EmailSubmission/set" {
		t.Fatalf("second call = %s", submitMethod)
	}
	submission := submitArgs.Create["mf-submit"]
	if submission.IdentityID != "ident" || submission.EmailID != "#mf-create" {
		t.Fatalf("submission = %+v", submission)
	}
	if submission.Envelope.MailFrom["email"] != "alice@example.test" {
		t.Fatalf("mailFrom = %+v", submission.Envelope.MailFrom)
	}
	if len(submission.Envelope.RcptTo) != 2 ||
		submission.Envelope.RcptTo[0]["email"] != "bob@example.test" ||
		submission.Envelope.RcptTo[1]["email"] != "carol@example.test" {
		t.Fatalf("rcptTo = %+v", submission.Envelope.RcptTo)
	}
	update := submitArgs.OnSuccessUpdateEmail["#mf-submit"]
	if update == nil {
		t.Fatalf("no success update: %+v", submitArgs.OnSuccessUpdateEmail)
	}
	if _, ok := update["mailboxIds/d"]; !ok {
		t.Fatalf("draft mailbox is not cleared: %+v", update)
	}
	if update["mailboxIds/s"] != true || update["keywords/$draft"] != nil {
		t.Fatalf("success update = %+v", update)
	}
}

func TestSendMessageReportsARefusalWithTheCreatedDraft(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[
			["Email/set",{"accountId":"a","created":{"mf-create":{"id":"e1"}}},"mf-create"],
			["EmailSubmission/set",{"accountId":"a","notCreated":{"mf-submit":{
				"type":"invalidProperties",
				"description":"Invalid e-mail address \"nobody@example.invalid\".",
				"properties":["envelope"]}}},"mf-submit"]
		]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	id, err := c.SendMessage(context.Background(), Credentials{User: "u", Password: "p"}, "acct", OutgoingEmail{
		DraftsMailboxID: "d",
		SentMailboxID:   "s",
		IdentityID:      "ident",
		From:            Address{Email: "alice@example.test"},
		To:              []Address{{Email: "nobody@example.invalid"}},
		Text:            "hi",
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error = %v, want ErrRejected", err)
	}
	if id != "e1" {
		t.Fatalf("created id = %q, want it returned for compensation", id)
	}
	if strings.Contains(err.Error(), "nobody@example.invalid") {
		t.Fatalf("error carries the vendor description: %v", err)
	}
}

func TestSendMessageTreatsAnUnexpectedMethodErrorAsARefusal(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[
			["Email/set",{"accountId":"a","created":{"mf-create":{"id":"e1"}}},"mf-create"],
			["error",{"type":"invalidArguments","description":"bad"}, "mf-submit"]
		]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	id, err := c.SendMessage(context.Background(), Credentials{User: "u", Password: "p"}, "acct", OutgoingEmail{
		DraftsMailboxID: "d",
		SentMailboxID:   "s",
		IdentityID:      "ident",
		From:            Address{Email: "alice@example.test"},
		To:              []Address{{Email: "bob@example.test"}},
		Text:            "hi",
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error = %v, want ErrRejected", err)
	}
	if id != "e1" {
		t.Fatalf("created id = %q", id)
	}
}

func TestDestroyEmailsReportsANotDestroyedEntry(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","notDestroyed":{"e1":{"type":"notFound"}}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	err := c.DestroyEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}
