package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/X-Ray-X/mailflow/app/internal/store"
)

type fakeOutbox struct {
	rows      []store.OutboxEvent
	published []int64
	recorded  []store.OutboxEvent
	unpubErr  error
	markErr   error
	recordErr error
}

func (f *fakeOutbox) Unpublished(context.Context, int) ([]store.OutboxEvent, error) {
	return f.rows, f.unpubErr
}

func (f *fakeOutbox) MarkPublished(_ context.Context, ids []int64) error {
	f.published = append(f.published, ids...)
	return f.markErr
}

func (f *fakeOutbox) RecordEvent(_ context.Context, event store.OutboxEvent) error {
	f.recorded = append(f.recorded, event)
	return f.recordErr
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	event := store.OutboxEvent{ID: 42, Account: "acct", MessageID: "msg1_a"}
	decoded, err := Decode(Encode(event))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded != event {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestDecodeRefusesMalformedEvents(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"eventId":0,"account":"a","messageId":"m"}`,
		`{"eventId":1,"account":"a","messageId":"m","extra":"vendor"}`,
		`not json`,
	} {
		if _, err := Decode([]byte(body)); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestRelayOnceMarksOnlyConfirmedEvents(t *testing.T) {
	rows := []store.OutboxEvent{{ID: 1}, {ID: 2}, {ID: 3}}
	f := &fakeOutbox{rows: rows}
	published := []int64{}
	err := RelayOnce(context.Background(), f, 10, func(_ context.Context, event store.OutboxEvent) error {
		if event.ID == 3 {
			return errors.New("broker refused")
		}
		published = append(published, event.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("RelayOnce: %v", err)
	}
	if len(f.published) != 2 || f.published[0] != 1 || f.published[1] != 2 {
		t.Fatalf("marked published = %v", f.published)
	}
}

func TestRelayOnceStopsWhenPublishingFailsImmediately(t *testing.T) {
	f := &fakeOutbox{rows: []store.OutboxEvent{{ID: 1}}}
	err := RelayOnce(context.Background(), f, 10, func(context.Context, store.OutboxEvent) error {
		return errors.New("unavailable")
	})
	if err != nil || len(f.published) != 0 {
		t.Fatalf("error = %v marked = %v", err, f.published)
	}
}

func TestRelayOnceReportsMarkFailure(t *testing.T) {
	f := &fakeOutbox{rows: []store.OutboxEvent{{ID: 1}}, markErr: errors.New("db down")}
	err := RelayOnce(context.Background(), f, 10, func(context.Context, store.OutboxEvent) error { return nil })
	if err == nil {
		t.Fatal("swallowed the store failure")
	}
}

func TestConsumeRecordsTheEvent(t *testing.T) {
	f := &fakeOutbox{}
	event := store.OutboxEvent{ID: 7, Account: "acct", MessageID: "msg1_a"}
	if err := Consume(context.Background(), f, event); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if len(f.recorded) != 1 || f.recorded[0] != event {
		t.Fatalf("recorded = %+v", f.recorded)
	}
}
