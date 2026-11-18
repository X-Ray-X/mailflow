// Package store keeps Mailflow application state in PostgreSQL.
//
// It owns exactly one thing today: the durable idempotency record that lets a
// retried send return the original result instead of sending again. It never
// stores mailbox content; the mail server remains the only source of mailbox
// truth.
package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/X-Ray-X/mailflow/app/internal/idempotency"
)

// Config is the database connection. The password is never logged.
type Config struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	Logger   *slog.Logger
}

// Store is a bounded PostgreSQL pool and the operations the send path needs.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

var _ idempotency.Recorder = (*Store)(nil)

// Claim states, kept as text so a row is readable in psql without a lookup.
const (
	stateInProgress = "in_progress"
	stateSucceeded  = "succeeded"
)

const (
	pingTimeout      = 5 * time.Second
	bootstrapTimeout = 15 * time.Second
	unlockTimeout    = 2 * time.Second
)

// bootstrapLockID is an arbitrary but fixed advisory-lock key. It serialises
// schema creation when two instances start at the same time.
const bootstrapLockID int64 = 0x6d61696c666c6f77 // "mailflow"

const schemaTable = `
CREATE TABLE IF NOT EXISTS send_idempotency (
    account_name text        NOT NULL,
    key          text        NOT NULL,
    request_hash bytea       NOT NULL,
    state        text        NOT NULL CHECK (state IN ('in_progress', 'succeeded')),
    message_id   text,
    claimed_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at   timestamptz NOT NULL,
    PRIMARY KEY (account_name, key)
)`

const schemaIndex = `
CREATE INDEX IF NOT EXISTS send_idempotency_expires_at
    ON send_idempotency (expires_at)`

// The outbox is application state only: it records that a send was accepted,
// never what the message contains. The event table is the consumer's idempotent
// projection of those events.
const schemaOutbox = `
CREATE TABLE IF NOT EXISTS send_outbox (
    id           bigserial PRIMARY KEY,
    account_name text        NOT NULL,
    message_id   text        NOT NULL,
    occurred_at  timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
)`

const schemaEvents = `
CREATE TABLE IF NOT EXISTS send_events (
    event_id     bigint PRIMARY KEY,
    account_name text        NOT NULL,
    message_id   text        NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now()
)`

// Open connects, verifies the connection, and creates the schema. A failure
// here stops startup: the service cannot honour deduplication without it.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	poolCfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("database pool defaults: %w", err)
	}
	// The connection is assembled from validated parts. No DSN is built, so a
	// password containing URL syntax needs no escaping and cannot appear in a
	// parse error.
	poolCfg.ConnConfig.Host = cfg.Host
	poolCfg.ConnConfig.Port = uint16(cfg.Port)
	poolCfg.ConnConfig.Database = cfg.Name
	poolCfg.ConnConfig.User = cfg.User
	poolCfg.ConnConfig.Password = cfg.Password
	poolCfg.ConnConfig.RuntimeParams["application_name"] = "mailflow-api"
	// No TLS: the database is reachable only on the application's private
	// Docker network and publishes no port. See docs/application-database.md.
	poolCfg.ConnConfig.TLSConfig = nil

	poolCfg.MaxConns = 8
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second
	poolCfg.ConnConfig.ConnectTimeout = pingTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{pool: pool, log: cfg.Logger}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	bootCtx, bootCancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer bootCancel()
	if err := s.bootstrap(bootCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

func (s *Store) bootstrap(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire database connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", bootstrapLockID); err != nil {
		return fmt.Errorf("lock schema bootstrap: %w", err)
	}
	defer func() {
		// Unlock on a context that is not already cancelled, or a timeout would
		// leave the lock held on this pooled connection.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", bootstrapLockID); err != nil {
			s.log.Warn("could not release the schema lock", "error", err)
		}
	}()

	for _, stmt := range []string{schemaTable, schemaIndex, schemaOutbox, schemaEvents} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("create application schema: %w", err)
		}
	}
	return nil
}

// Claim reserves a send key. See idempotency.Recorder.
func (s *Store) Claim(ctx context.Context, account, key string, requestHash []byte, ttl, lease time.Duration) (idempotency.Claim, error) {
	// The unique primary key is the race resolver: only one of two concurrent
	// requests can insert, and the loser reads the winner's row.
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO send_idempotency (account_name, key, request_hash, state, expires_at)
		VALUES ($1, $2, $3, 'in_progress', now() + ($4::double precision * interval '1 second'))
		ON CONFLICT (account_name, key) DO NOTHING`,
		account, key, requestHash, ttl.Seconds())
	if err != nil {
		return idempotency.Claim{}, fmt.Errorf("claim send key: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return idempotency.Claim{State: idempotency.Claimed}, nil
	}

	var (
		existing  []byte
		state     string
		messageID string
		expired   bool
		stale     bool
	)
	err = s.pool.QueryRow(ctx, `
		SELECT request_hash, state, coalesce(message_id, ''),
		       expires_at <= now(),
		       claimed_at <= now() - ($3::double precision * interval '1 second')
		FROM send_idempotency
		WHERE account_name = $1 AND key = $2`,
		account, key, lease.Seconds()).Scan(&existing, &state, &messageID, &expired, &stale)
	if err != nil {
		return idempotency.Claim{}, fmt.Errorf("read send key: %w", err)
	}

	// A key past its retention is a new send, exactly as if it had never been
	// used. The conditional update keeps a concurrent winner authoritative.
	if expired {
		tag, err := s.pool.Exec(ctx, `
			UPDATE send_idempotency
			SET request_hash = $3, state = 'in_progress', message_id = NULL,
			    claimed_at = now(), completed_at = NULL,
			    expires_at = now() + ($4::double precision * interval '1 second')
			WHERE account_name = $1 AND key = $2 AND expires_at <= now()`,
			account, key, requestHash, ttl.Seconds())
		if err != nil {
			return idempotency.Claim{}, fmt.Errorf("reclaim expired send key: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return idempotency.Claim{State: idempotency.Claimed}, nil
		}
		return idempotency.Claim{State: idempotency.InFlight}, nil
	}

	if !bytes.Equal(existing, requestHash) {
		return idempotency.Claim{State: idempotency.Mismatch}, nil
	}
	if state == stateSucceeded {
		return idempotency.Claim{State: idempotency.Replay, MessageID: messageID}, nil
	}
	if stale {
		// The previous attempt died without recording success. Take over only
		// if nobody else has, which the claimed_at predicate guarantees.
		tag, err := s.pool.Exec(ctx, `
			UPDATE send_idempotency
			SET claimed_at = now(), expires_at = now() + ($4::double precision * interval '1 second')
			WHERE account_name = $1 AND key = $2 AND state = 'in_progress'
			  AND request_hash = $3
			  AND claimed_at <= now() - ($5::double precision * interval '1 second')`,
			account, key, requestHash, ttl.Seconds(), lease.Seconds())
		if err != nil {
			return idempotency.Claim{}, fmt.Errorf("reclaim stale send key: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return idempotency.Claim{State: idempotency.Claimed}, nil
		}
	}
	return idempotency.Claim{State: idempotency.InFlight}, nil
}

// Complete records a successful send. See idempotency.Recorder.
func (s *Store) Complete(ctx context.Context, account, key, messageID string, ttl time.Duration, event idempotency.SendEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin send completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE send_idempotency
		SET state = 'succeeded', message_id = $3, completed_at = now(),
		    expires_at = now() + ($4::double precision * interval '1 second')
		WHERE account_name = $1 AND key = $2`,
		account, key, messageID, ttl.Seconds())
	if err != nil {
		return fmt.Errorf("record successful send: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("send record disappeared before it was completed")
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO send_outbox (account_name, message_id) VALUES ($1, $2)`,
		event.Account, event.MessageID); err != nil {
		return fmt.Errorf("record send event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit send completion: %w", err)
	}
	return nil
}

// OutboxEvent is one unpublished row. Only identifiers cross this boundary.
type OutboxEvent struct {
	ID        int64
	Account   string
	MessageID string
}

// Unpublished returns the oldest bounded batch of events still awaiting
// publication. A batch is never unbounded, so one poll cannot monopolise the
// worker.
func (s *Store) Unpublished(ctx context.Context, limit int) ([]OutboxEvent, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("outbox batch size out of range")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_name, message_id FROM send_outbox
		WHERE published_at IS NULL ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	defer rows.Close()

	out := make([]OutboxEvent, 0, limit)
	for rows.Next() {
		var event OutboxEvent
		if err := rows.Scan(&event.ID, &event.Account, &event.MessageID); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// MarkPublished records that the broker accepted a bounded batch.
func (s *Store) MarkPublished(ctx context.Context, ids []int64) error {
	if len(ids) == 0 || len(ids) > 100 {
		return errors.New("published batch size out of range")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE send_outbox SET published_at = now() WHERE id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	if int(tag.RowsAffected()) != len(ids) {
		return errors.New("outbox row disappeared before it was marked published")
	}
	return nil
}

// RecordEvent stores a consumed event once. Delivery is at least once, so a
// repeated delivery must be a no-op rather than a duplicate row.
func (s *Store) RecordEvent(ctx context.Context, event OutboxEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO send_events (event_id, account_name, message_id)
		VALUES ($1, $2, $3) ON CONFLICT (event_id) DO NOTHING`,
		event.ID, event.Account, event.MessageID)
	if err != nil {
		return fmt.Errorf("record consumed event: %w", err)
	}
	return nil
}

// Release removes a claim that certainly produced no message. See
// idempotency.Recorder.
func (s *Store) Release(ctx context.Context, account, key string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM send_idempotency
		WHERE account_name = $1 AND key = $2 AND state = 'in_progress'`, account, key)
	if err != nil {
		return fmt.Errorf("release send key: %w", err)
	}
	return nil
}

// RunJanitor deletes expired records until ctx is cancelled. It sweeps at
// start and then on every interval, in bounded batches so one pass cannot hold
// a long transaction.
func (s *Store) RunJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	s.sweep(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *Store) sweep(ctx context.Context) {
	const batch = 1000
	for i := 0; i < 100; i++ {
		tag, err := s.pool.Exec(ctx, `
			DELETE FROM send_idempotency
			WHERE ctid IN (
				SELECT ctid FROM send_idempotency WHERE expires_at < now() LIMIT $1
			)`, batch)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("could not delete expired send records", "error", err)
			}
			return
		}
		if tag.RowsAffected() < batch {
			return
		}
	}
}
