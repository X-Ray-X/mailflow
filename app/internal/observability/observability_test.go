package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsCarryRouteAndStatusWithoutMailData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setup, err := New(ctx, "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = setup.Shutdown(ctx) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/{messageID}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"msg1_secret"}`))
	})
	server := httptest.NewServer(setup.Wrap(mux))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/v1/messages/msg1_alice@mailflow.test")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	recorder := httptest.NewRecorder()
	setup.Metrics.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, want := range []string{"http_server_request_duration_seconds", "/v1/messages/{messageID}", "201"} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q\n%s", want, body[:min(len(body), 800)])
		}
	}
	for _, leak := range []string{"alice@mailflow.test", "msg1_secret", "msg1_alice"} {
		if strings.Contains(body, leak) {
			t.Fatalf("metrics leak %q", leak)
		}
	}
}

func TestNewWorksWithoutACollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setup, err := New(ctx, "", nil)
	if err != nil || setup == nil || setup.Metrics == nil || setup.Wrap == nil || setup.Shutdown == nil {
		t.Fatalf("setup = %+v error %v", setup, err)
	}
	if err := setup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
