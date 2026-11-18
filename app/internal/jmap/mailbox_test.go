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

func TestCreateMailboxSubscribesAndReturnsTheIdentifier(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/set",{"accountId":"a","created":{"mf-create":{"id":"m1"}}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	id, err := c.CreateMailbox(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "Projects")
	if err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	if id != "m1" {
		t.Fatalf("id = %q", id)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(sent.MethodCalls) != 1 {
		t.Fatalf("methodCalls = %d", len(sent.MethodCalls))
	}
	var method string
	var args struct {
		AccountID string `json:"accountId"`
		Create    map[string]struct {
			Name         string `json:"name"`
			IsSubscribed bool   `json:"isSubscribed"`
			ParentID     string `json:"parentId"`
		} `json:"create"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][0], &method)
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if method != "Mailbox/set" || args.AccountID != "acct" {
		t.Fatalf("call = %s %+v", method, args)
	}
	create := args.Create["mf-create"]
	if create.Name != "Projects" || !create.IsSubscribed || create.ParentID != "" {
		t.Fatalf("create = %+v, want a subscribed top-level mailbox", create)
	}
}

func TestCreateMailboxMapsRefusals(t *testing.T) {
	cases := map[string]error{
		"alreadyExists":     ErrAlreadyExists,
		"invalidProperties": ErrInvalidInput,
		"notFound":          ErrNotFound,
		"serverBug":         ErrUpstream,
	}
	for vendorType, want := range cases {
		t.Run(vendorType, func(t *testing.T) {
			srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/set",{"accountId":"a","notCreated":{"mf-create":{"type":"` + vendorType + `"}}},"c"]]}`))
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			_, err := c.CreateMailbox(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "Projects")
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestRenameMailboxMapsRefusals(t *testing.T) {
	cases := map[string]error{
		"alreadyExists":     ErrAlreadyExists,
		"invalidProperties": ErrInvalidInput,
		"notFound":          ErrNotFound,
		"serverBug":         ErrUpstream,
	}
	for vendorType, want := range cases {
		t.Run(vendorType, func(t *testing.T) {
			srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/set",{"accountId":"a","notUpdated":{"m1":{"type":"` + vendorType + `"}}},"c"]]}`))
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			err := c.RenameMailbox(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "m1", "Work")
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestDestroyMailboxMapsRefusals(t *testing.T) {
	cases := map[string]error{
		"mailboxHasEmail": ErrMailboxNotEmpty,
		"mailboxHasChild": ErrMailboxNotEmpty,
		"notFound":        ErrNotFound,
		"serverBug":       ErrUpstream,
	}
	for vendorType, want := range cases {
		t.Run(vendorType, func(t *testing.T) {
			srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"methodResponses":[["Mailbox/set",{"accountId":"a","notDestroyed":{"m1":{"type":"` + vendorType + `"}}},"c"]]}`))
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			err := c.DestroyMailbox(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "m1")
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestUpdateEmailSendsFlagsAndFullMembership(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","updated":{"e1":null}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	read, starred := true, false
	if err := c.UpdateEmail(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1", EmailPatch{
		Read:      &read,
		Starred:   &starred,
		MailboxID: "m2",
	}); err != nil {
		t.Fatalf("UpdateEmail: %v", err)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var args struct {
		AccountID string                    `json:"accountId"`
		Update    map[string]map[string]any `json:"update"`
	}
	if err := json.Unmarshal(sent.MethodCalls[0][1], &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	update := args.Update["e1"]
	if update["keywords/$seen"] != true || update["keywords/$flagged"] != false {
		t.Fatalf("keyword patches = %+v", update)
	}
	mailboxes, ok := update["mailboxIds"].(map[string]any)
	if !ok || len(mailboxes) != 1 || mailboxes["m2"] != true {
		t.Fatalf("mailboxIds = %+v, want exactly the target", update["mailboxIds"])
	}
}

func TestUpdateEmailOmitsMembershipForAFlagOnlyPatch(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","updated":{"e1":null}},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	read := true
	if err := c.UpdateEmail(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1", EmailPatch{Read: &read}); err != nil {
		t.Fatalf("UpdateEmail: %v", err)
	}
	_, _, _, body := cap.get()
	if got := string(body); strings.Contains(got, "mailboxIds") {
		t.Fatalf("flag-only update carries membership: %s", body)
	}
}

func TestUpdateEmailMapsNotUpdated(t *testing.T) {
	cases := map[string]error{
		"notFound":          ErrNotFound,
		"invalidProperties": ErrInvalidInput,
		"serverBug":         ErrUpstream,
	}
	for vendorType, want := range cases {
		t.Run(vendorType, func(t *testing.T) {
			srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"methodResponses":[["Email/set",{"accountId":"a","notUpdated":{"e1":{"type":"` + vendorType + `"}}},"c"]]}`))
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			read := true
			err := c.UpdateEmail(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1", EmailPatch{Read: &read})
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestUpdateEmailWithNothingToChangeDoesNotCallTheServer(t *testing.T) {
	srv, ca := startServer(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("an empty patch reached the mail server")
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	if err := c.UpdateEmail(context.Background(), Credentials{User: "u", Password: "p"}, "acct", "e1", EmailPatch{}); err != nil {
		t.Fatalf("UpdateEmail: %v", err)
	}
}
