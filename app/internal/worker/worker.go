// Package worker holds the queue-side logic. It knows outbox rows and JSON
// events but not AMQP, so the relay and consumer rules can be tested without a
// broker.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/X-Ray-X/mailflow/app/internal/store"
)

// Outbox is the slice of the store the worker needs.
type Outbox interface {
	Unpublished(ctx context.Context, limit int) ([]store.OutboxEvent, error)
	MarkPublished(ctx context.Context, ids []int64) error
	RecordEvent(ctx context.Context, event store.OutboxEvent) error
}

// Event is the wire shape on the queue. It carries identifiers only.
type Event struct {
	ID        int64  `json:"eventId"`
	Account   string `json:"account"`
	MessageID string `json:"messageId"`
}

func encode(event store.OutboxEvent) []byte {
	body, _ := json.Marshal(Event{ID: event.ID, Account: event.Account, MessageID: event.MessageID})
	return body
}

// Decode reads one event. Unknown fields and empty identifiers are refused so a
// malformed message cannot be turned into a database write.
func Decode(body []byte) (store.OutboxEvent, error) {
	var event Event
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&event); err != nil {
		return store.OutboxEvent{}, fmt.Errorf("decode event: %w", err)
	}
	if event.ID <= 0 || event.Account == "" || event.MessageID == "" {
		return store.OutboxEvent{}, errors.New("event is missing an identifier")
	}
	return store.OutboxEvent{ID: event.ID, Account: event.Account, MessageID: event.MessageID}, nil
}

// Encode is the testable form of the publisher's payload.
func Encode(event store.OutboxEvent) []byte { return encode(event) }

// RelayOnce publishes at most one bounded batch. Only events whose publish was
// confirmed are marked published; the rest stay for the next poll.
func RelayOnce(ctx context.Context, outbox Outbox, batch int, publish func(context.Context, store.OutboxEvent) error) error {
	rows, err := outbox.Unpublished(ctx, batch)
	if err != nil {
		return err
	}
	published := make([]int64, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			break
		}
		if err := publish(ctx, row); err != nil {
			break
		}
		published = append(published, row.ID)
	}
	if len(published) == 0 {
		return nil
	}
	return outbox.MarkPublished(ctx, published)
}

// Consume records one delivered event. The broker is acknowledged by the
// caller only after this returns nil.
func Consume(ctx context.Context, outbox Outbox, event store.OutboxEvent) error {
	return outbox.RecordEvent(ctx, event)
}
