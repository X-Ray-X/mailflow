package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/X-Ray-X/mailflow/app/internal/idempotency"
)

// The store talks to PostgreSQL, so these tests run only when a disposable
// database is named. The pinned image and the invocation are recorded in
// docs/application-database.md.
func testConfig(t *testing.T) Config {
	t.Helper()
	host := os.Getenv("MAILFLOW_TEST_DB_HOST")
	if host == "" {
		t.Skip("MAILFLOW_TEST_DB_HOST is not set")
	}
	port := 5432
	if raw := os.Getenv("MAILFLOW_TEST_DB_PORT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("MAILFLOW_TEST_DB_PORT: %v", err)
		}
		port = n
	}
	name := os.Getenv("MAILFLOW_TEST_DB_NAME")
	if name == "" {
		name = "mailflow_test"
	}
	user := os.Getenv("MAILFLOW_TEST_DB_USER")
	if user == "" {
		user = "mailflow"
	}
	password := os.Getenv("MAILFLOW_TEST_DB_PASSWORD")
	if password == "" {
		password = "test"
	}
	return Config{
		Host:     host,
		Port:     port,
		Name:     name,
		User:     user,
		Password: password,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	s, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)

	if _, err := s.pool.Exec(ctx, "DELETE FROM send_idempotency"); err != nil {
		t.Fatalf("clear records: %v", err)
	}
	return s
}

func claim(t *testing.T, s *Store, account, key string, hash []byte, ttl, lease time.Duration) idempotency.Claim {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := s.Claim(ctx, account, key, hash, ttl, lease)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return got
}

func TestClaimReplayConflictAndInFlight(t *testing.T) {
	s := openTestStore(t)
	const account = "alice@mailflow.test"
	hashA := []byte("hash-a")

	if got := claim(t, s, account, "key-1", hashA, time.Hour, time.Minute); got.State != idempotency.Claimed {
		t.Fatalf("first claim = %v, want Claimed", got.State)
	}
	if got := claim(t, s, account, "key-1", hashA, time.Hour, time.Minute); got.State != idempotency.InFlight {
		t.Fatalf("duplicate claim = %v, want InFlight", got.State)
	}
	if got := claim(t, s, account, "key-1", []byte("hash-b"), time.Hour, time.Minute); got.State != idempotency.Mismatch {
		t.Fatalf("different request = %v, want Mismatch", got.State)
	}
	if got := claim(t, s, "bob@mailflow.test", "key-1", hashA, time.Hour, time.Minute); got.State != idempotency.Claimed {
		t.Fatalf("same key for another account = %v, want Claimed", got.State)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Complete(ctx, account, "key-1", "msg1_sent", time.Hour, idempotency.SendEvent{Account: account, MessageID: "msg1_sent"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got := claim(t, s, account, "key-1", hashA, time.Hour, time.Minute)
	if got.State != idempotency.Replay {
		t.Fatalf("completed claim = %v, want Replay", got.State)
	}
	if got.MessageID != "msg1_sent" {
		t.Fatalf("replay id = %q", got.MessageID)
	}
	if got := claim(t, s, account, "key-1", []byte("hash-b"), time.Hour, time.Minute); got.State != idempotency.Mismatch {
		t.Fatalf("different request after completion = %v, want Mismatch", got.State)
	}
}

func TestLeaseExpiryLetsARetryTakeOver(t *testing.T) {
	s := openTestStore(t)
	hash := []byte("hash")
	if got := claim(t, s, "acct", "key", hash, time.Hour, 200*time.Millisecond); got.State != idempotency.Claimed {
		t.Fatalf("first claim = %v", got.State)
	}
	if got := claim(t, s, "acct", "key", hash, time.Hour, 200*time.Millisecond); got.State != idempotency.InFlight {
		t.Fatalf("immediate duplicate = %v, want InFlight", got.State)
	}

	time.Sleep(300 * time.Millisecond)
	if got := claim(t, s, "acct", "key", hash, time.Hour, 200*time.Millisecond); got.State != idempotency.Claimed {
		t.Fatalf("after the lease = %v, want Claimed", got.State)
	}
}

func TestRetentionExpiryMakesTheKeyNewAgain(t *testing.T) {
	s := openTestStore(t)
	hash := []byte("hash")
	if got := claim(t, s, "acct", "key", hash, 100*time.Millisecond, 50*time.Millisecond); got.State != idempotency.Claimed {
		t.Fatalf("first claim = %v", got.State)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Complete(ctx, "acct", "key", "msg1_sent", 100*time.Millisecond, idempotency.SendEvent{Account: "acct", MessageID: "msg1_sent"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	got := claim(t, s, "acct", "key", []byte("different"), time.Hour, time.Minute)
	if got.State != idempotency.Claimed {
		t.Fatalf("after retention = %v, want Claimed", got.State)
	}
}

func TestReleaseAllowsARetry(t *testing.T) {
	s := openTestStore(t)
	hash := []byte("hash")
	if got := claim(t, s, "acct", "key", hash, time.Hour, time.Minute); got.State != idempotency.Claimed {
		t.Fatalf("first claim = %v", got.State)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Release(ctx, "acct", "key"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := claim(t, s, "acct", "key", hash, time.Hour, time.Minute); got.State != idempotency.Claimed {
		t.Fatalf("after release = %v, want Claimed", got.State)
	}

	// Release must not remove a success.
	if err := s.Complete(ctx, "acct", "key", "msg1_sent", time.Hour, idempotency.SendEvent{Account: "acct", MessageID: "msg1_sent"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := s.Release(ctx, "acct", "key"); err != nil {
		t.Fatalf("Release after completion: %v", err)
	}
	got := claim(t, s, "acct", "key", hash, time.Hour, time.Minute)
	if got.State != idempotency.Replay || got.MessageID != "msg1_sent" {
		t.Fatalf("after releasing a success = %+v, want Replay", got)
	}
}

func TestJanitorDeletesExpiredRecords(t *testing.T) {
	s := openTestStore(t)
	if got := claim(t, s, "acct", "expired", []byte("hash"), 50*time.Millisecond, time.Minute); got.State != idempotency.Claimed {
		t.Fatalf("claim = %v", got.State)
	}
	time.Sleep(100 * time.Millisecond)

	s.sweep(context.Background())

	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM send_idempotency").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("records after the sweep = %d, want 0", count)
	}
}

func TestSecondOpenKeepsTheSchemaAndRecords(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if got := claim(t, s, "acct", "key", []byte("hash"), time.Hour, time.Minute); got.State != idempotency.Claimed {
		t.Fatalf("claim = %v", got.State)
	}
	if err := s.Complete(ctx, "acct", "key", "msg1_sent", time.Hour, idempotency.SendEvent{Account: "acct", MessageID: "msg1_sent"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	second, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	got, err := second.Claim(ctx, "acct", "key", []byte("hash"), time.Hour, time.Minute)
	if err != nil {
		t.Fatalf("Claim after second Open: %v", err)
	}
	if got.State != idempotency.Replay || got.MessageID != "msg1_sent" {
		t.Fatalf("after second Open = %+v, want the earlier replay", got)
	}
}

func resetOutbox(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(ctx, "TRUNCATE send_outbox, send_events"); err != nil {
		t.Fatalf("truncate outbox: %v", err)
	}
}

func TestCompleteWritesOneOutboxRowAndEventConsumptionIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	resetOutbox(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	claim, err := s.Claim(ctx, "acct", "key-outbox", []byte("hash"), time.Hour, time.Minute)
	if err != nil || claim.State != idempotency.Claimed {
		t.Fatalf("claim = %+v error %v", claim, err)
	}
	event := idempotency.SendEvent{Account: "acct", MessageID: "msg1_sent"}
	if err := s.Complete(ctx, "acct", "key-outbox", "msg1_sent", time.Hour, event); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	rows, err := s.Unpublished(ctx, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("unpublished = %+v error %v", rows, err)
	}
	if rows[0].Account != "acct" || rows[0].MessageID != "msg1_sent" || rows[0].ID == 0 {
		t.Fatalf("event row = %+v", rows[0])
	}

	if err := s.MarkPublished(ctx, []int64{rows[0].ID}); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}
	again, err := s.Unpublished(ctx, 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("published row still pending: %+v error %v", again, err)
	}

	if err := s.RecordEvent(ctx, rows[0]); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := s.RecordEvent(ctx, rows[0]); err != nil {
		t.Fatalf("repeat RecordEvent: %v", err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM send_events WHERE event_id = $1", rows[0].ID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("event rows = %d, want 1", count)
	}
}

func TestOutboxBatchIsBounded(t *testing.T) {
	s := openTestStore(t)
	resetOutbox(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key-batch-%d", i)
		if _, err := s.Claim(ctx, "acct", key, []byte(key), time.Hour, time.Minute); err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if err := s.Complete(ctx, "acct", key, "msg1_"+key, time.Hour,
			idempotency.SendEvent{Account: "acct", MessageID: "msg1_" + key}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	rows, err := s.Unpublished(ctx, 2)
	if err != nil || len(rows) != 2 {
		t.Fatalf("batch = %+v error %v", rows, err)
	}
	if _, err := s.Unpublished(ctx, 0); err == nil {
		t.Fatal("accepted a zero-size batch")
	}
	if _, err := s.Unpublished(ctx, 101); err == nil {
		t.Fatal("accepted an unbounded batch")
	}
}
