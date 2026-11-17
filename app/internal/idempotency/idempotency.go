// Package idempotency is the contract between the send handler and the record
// store.
//
// The handler decides whether a send may proceed; the store records that
// decision durably. Keeping the vocabulary here means the HTTP layer never
// depends on a database package and the store never depends on HTTP.
package idempotency

import (
	"context"
	"time"
)

// State is the outcome of asking to claim a key.
type State int

const (
	// Claimed means the caller now owns this key and must perform the send.
	Claimed State = iota
	// Replay means the same request already succeeded; the recorded message
	// identifier is the answer, and nothing may be sent.
	Replay
	// Mismatch means the key was already used for a different request.
	Mismatch
	// InFlight means a duplicate arrived while the first attempt is still
	// being processed and its lease has not expired.
	InFlight
)

// Claim is the store's answer to a claim request.
type Claim struct {
	State     State
	MessageID string
}

// Recorder is the durable record of sends that have been claimed, completed,
// or released.
type Recorder interface {
	// Claim reserves the key for this account and request hash. It returns
	// Replay or Mismatch for a key seen before, InFlight for a duplicate whose
	// lease has not expired, and Claimed when the caller may send.
	Claim(ctx context.Context, account, key string, requestHash []byte, ttl, lease time.Duration) (Claim, error)

	// Complete records the successful send. The identifier is what a replay
	// returns; it must be a Mailflow identifier, not a vendor one.
	Complete(ctx context.Context, account, key, messageID string, ttl time.Duration) error

	// Release removes a claim for a send that certainly did not happen, so the
	// caller may retry with the same key. It must not remove a success.
	Release(ctx context.Context, account, key string) error
}
