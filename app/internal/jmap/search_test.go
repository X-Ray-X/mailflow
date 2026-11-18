package jmap

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestQueryEmailsSendsTheTextFilterWithTheMailbox(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/query",{"ids":["e1","e2"],"total":9,"position":2},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	result, err := c.QueryEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		Query{MailboxID: "m1", Text: "Mailflow verifier", Limit: 2, Anchor: "e0"})
	if err != nil {
		t.Fatalf("QueryEmails: %v", err)
	}
	if result.Total != 9 || result.Position != 2 || len(result.IDs) != 2 {
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
		Filter map[string]string `json:"filter"`
		Limit  int               `json:"limit"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][1], &args)
	if args.Filter["text"] != "Mailflow verifier" || args.Filter["inMailbox"] != "m1" || len(args.Filter) != 2 {
		t.Fatalf("filter = %+v", args.Filter)
	}
	if args.Limit != 2 {
		t.Fatalf("limit = %d", args.Limit)
	}
}

func TestQueryEmailsWithoutAMailboxFiltersOnlyByText(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/query",{"ids":[],"total":0,"position":0},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	if _, err := c.QueryEmails(context.Background(), Credentials{User: "u", Password: "p"}, "acct",
		Query{Text: "needle", Limit: 5}); err != nil {
		t.Fatalf("QueryEmails: %v", err)
	}

	_, _, _, body := cap.get()
	var sent struct {
		MethodCalls [][]json.RawMessage `json:"methodCalls"`
	}
	_ = json.Unmarshal(body, &sent)
	var args struct {
		Filter map[string]string `json:"filter"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][1], &args)
	if len(args.Filter) != 1 || args.Filter["text"] != "needle" {
		t.Fatalf("filter = %+v", args.Filter)
	}
}
