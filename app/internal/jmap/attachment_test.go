package jmap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGetAttachmentsReadsOnlyMetadata(t *testing.T) {
	cap := &capture{}
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"accountId":"d","list":[{
			"id":"e1",
			"attachments":[{"blobId":"b1","name":"payload.bin","type":"application/octet-stream",
				"size":2048,"disposition":"attachment","partId":"4","cid":null}]
		}]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)

	attachments, err := c.GetAttachments(context.Background(), Credentials{User: "u", Password: "p"}, "d", "e1")
	if err != nil {
		t.Fatalf("GetAttachments: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments = %+v", attachments)
	}
	att := attachments[0]
	if att.BlobID != "b1" || att.Name != "payload.bin" || att.Type != "application/octet-stream" ||
		att.Size != 2048 || att.Disposition != "attachment" || att.PartID != "4" {
		t.Fatalf("attachment = %+v", att)
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
		AccountID  string   `json:"accountId"`
		IDs        []string `json:"ids"`
		Properties []string `json:"properties"`
	}
	_ = json.Unmarshal(sent.MethodCalls[0][0], &method)
	_ = json.Unmarshal(sent.MethodCalls[0][1], &args)
	if method != "Email/get" || args.AccountID != "d" {
		t.Fatalf("call = %s %+v", method, args)
	}
	if len(args.IDs) != 1 || args.IDs[0] != "e1" {
		t.Fatalf("ids = %v", args.IDs)
	}
	if strings.Join(args.Properties, ",") != "id,attachments" {
		t.Fatalf("properties = %v", args.Properties)
	}
}

func TestGetAttachmentsReportsAnUnknownMessage(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"methodResponses":[["Email/get",{"accountId":"d","list":[]},"c"]]}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	if _, err := c.GetAttachments(context.Background(), Credentials{User: "u", Password: "p"}, "d", "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestDownloadURLRefusesAnotherHostOrScheme(t *testing.T) {
	c := &Client{hostname: "mail.test"}
	att := Attachment{BlobID: "b1", Name: "a.txt", Type: "text/plain"}
	for _, template := range []string{
		"http://mail.test/download/{accountId}/{blobId}/{name}?accept={type}",
		"https://elsewhere.test/download/{accountId}/{blobId}/{name}?accept={type}",
		"https://user@mail.test/download/{accountId}/{blobId}/{name}?accept={type}",
		"not a url",
	} {
		if _, err := c.downloadURL(template, "d", att); !errors.Is(err, ErrUpstream) {
			t.Fatalf("template %q: error = %v, want ErrUpstream", template, err)
		}
	}
}

func TestDownloadURLExpandsTheObservedTemplate(t *testing.T) {
	c := &Client{
		hostname: "mail.test",
		apiURL:   &url.URL{Scheme: "https", Host: "mailflow-stalwart:443"},
	}
	template := "https://mail.test/jmap/download/{accountId}/{blobId}/{name}?accept={type}"
	u, err := c.downloadURL(template, "d/1", Attachment{BlobID: "b/1", Name: "a b.txt", Type: "text/plain"})
	if err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if want := "https://mailflow-stalwart:443/jmap/download/d%2F1/b%2F1/a%20b.txt?accept=text%2Fplain"; u.String() != want {
		t.Fatalf("URL = %q, want %q", u.String(), want)
	}
}

func TestDownloadAttachmentStreamsTheBlob(t *testing.T) {
	var srv *httptest.Server
	var auth, accept string
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/jmap/session":
			_, _ = w.Write([]byte(`{"downloadUrl":"` + srv.URL + `/jmap/download/{accountId}/{blobId}/{name}?accept={type}"}`))
		case strings.HasPrefix(r.URL.Path, "/jmap/download/"):
			auth = r.Header.Get("Authorization")
			accept = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("attachment-bytes"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	c.hostname = "127.0.0.1"

	stream, err := c.DownloadAttachment(context.Background(), Credentials{User: "u", Password: "p"}, "d",
		Attachment{BlobID: "b1", Name: "payload.bin", Type: "application/octet-stream"})
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(data) != "attachment-bytes" {
		t.Fatalf("bytes = %q", data)
	}
	if !strings.HasPrefix(auth, "Basic ") || accept != "application/octet-stream" {
		t.Fatalf("auth = %q accept = %q", auth, accept)
	}
}

func TestDownloadAttachmentMapsHTTPFailures(t *testing.T) {
	cases := map[int]error{
		http.StatusNotFound:            ErrNotFound,
		http.StatusUnauthorized:        ErrUnauthorized,
		http.StatusInternalServerError: ErrUnavailable,
	}
	for status, want := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var srv *httptest.Server
			srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/jmap/session" {
					_, _ = w.Write([]byte(`{"downloadUrl":"` + srv.URL + `/jmap/download/{accountId}/{blobId}/{name}?accept={type}"}`))
					return
				}
				w.WriteHeader(status)
			})
			c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
			c.hostname = "127.0.0.1"
			stream, err := c.DownloadAttachment(context.Background(), Credentials{User: "u", Password: "p"}, "d",
				Attachment{BlobID: "b1", Name: "a.bin", Type: "application/octet-stream"})
			if stream != nil {
				_ = stream.Close()
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestDownloadAttachmentRefusesAnUnsafeSessionTemplate(t *testing.T) {
	srv, ca := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"downloadUrl":"https://elsewhere.test/download/{accountId}/{blobId}/{name}?accept={type}"}`))
	})
	c := clientFor(t, srv.URL, ca.writeFile(t), 2*time.Second)
	stream, err := c.DownloadAttachment(context.Background(), Credentials{User: "u", Password: "p"}, "d",
		Attachment{BlobID: "b1", Name: "a.bin", Type: "application/octet-stream"})
	if stream != nil {
		_ = stream.Close()
	}
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}
