// Command mailflow-worker relays send events from the PostgreSQL outbox to
// RabbitMQ and consumes them into the application event table. It never speaks
// a mail protocol and never touches mailbox state.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/X-Ray-X/mailflow/app/internal/store"
	"github.com/X-Ray-X/mailflow/app/internal/worker"
)

const (
	defaultQueue   = "mailflow.send.events"
	defaultPoll    = time.Second
	publishTimeout = 5 * time.Second
	maxAttempts    = 5
)

type config struct {
	db       store.Config
	queueURL string
	queue    string
	poll     time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	var cfg config
	port := 5432
	if raw := getenv("MAILFLOW_DB_PORT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 65535 {
			return cfg, errors.New("MAILFLOW_DB_PORT must be a valid port")
		}
		port = n
	}
	password := getenv("MAILFLOW_DB_PASSWORD")
	if password == "" {
		return cfg, errors.New("MAILFLOW_DB_PASSWORD is required")
	}
	host := getenv("MAILFLOW_DB_HOST")
	if host == "" {
		host = "db"
	}
	name := getenv("MAILFLOW_DB_NAME")
	if name == "" {
		name = "mailflow"
	}
	user := getenv("MAILFLOW_DB_USER")
	if user == "" {
		user = "mailflow"
	}
	cfg.db = store.Config{Host: host, Port: port, Name: name, User: user, Password: password}

	cfg.queueURL = strings.TrimSpace(getenv("MAILFLOW_RABBITMQ_URL"))
	parsed, err := url.Parse(cfg.queueURL)
	if err != nil || parsed.Scheme != "amqp" || parsed.Host == "" || parsed.User == nil {
		return cfg, errors.New("MAILFLOW_RABBITMQ_URL must be an amqp URL with credentials")
	}
	cfg.queue = getenv("MAILFLOW_QUEUE_NAME")
	if cfg.queue == "" {
		cfg.queue = defaultQueue
	}
	cfg.poll = defaultPoll
	if raw := getenv("MAILFLOW_WORKER_POLL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < 100*time.Millisecond || d > time.Minute {
			return cfg, errors.New("MAILFLOW_WORKER_POLL must be between 100ms and 1m")
		}
		cfg.poll = d
	}
	return cfg, nil
}

type publisher struct {
	channel       *amqp.Channel
	queue         string
	confirmations <-chan amqp.Confirmation
}

func newPublisher(conn *amqp.Connection, queue string) (*publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open publisher channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publish confirmations: %w", err)
	}
	return &publisher{channel: ch, queue: queue, confirmations: ch.NotifyPublish(make(chan amqp.Confirmation, 1))}, nil
}

func (p *publisher) publish(ctx context.Context, body []byte, attempt int) error {
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	headers := amqp.Table{"x-attempt": int64(attempt)}
	if err := p.channel.PublishWithContext(ctx, "", p.queue, false, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent,
		Body: body, Headers: headers,
	}); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	select {
	case confirm := <-p.confirmations:
		if !confirm.Ack {
			return errors.New("broker did not confirm the event")
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("publish confirmation: %w", ctx.Err())
	}
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Error("invalid worker configuration", "error", err)
		os.Exit(1)
	}
	db, err := store.Open(ctx, cfg.db)
	if err != nil {
		log.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	conn, err := amqp.Dial(cfg.queueURL)
	if err != nil {
		log.Error("broker unavailable", "error", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	consumerChannel, err := conn.Channel()
	if err != nil {
		log.Error("consumer channel failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = consumerChannel.Close() }()
	if _, err := consumerChannel.QueueDeclare(cfg.queue, true, false, false, false, nil); err != nil {
		log.Error("queue declaration failed", "error", err)
		os.Exit(1)
	}
	if err := consumerChannel.Qos(1, 0, false); err != nil {
		log.Error("consumer qos failed", "error", err)
		os.Exit(1)
	}

	publisher, err := newPublisher(conn, cfg.queue)
	if err != nil {
		log.Error("publisher setup failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = publisher.channel.Close() }()

	go relayLoop(ctx, db, publisher, cfg.poll, log)
	consumeLoop(ctx, db, consumerChannel, publisher, cfg.queue, log)
}

func relayLoop(ctx context.Context, db *store.Store, publisher *publisher, poll time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := worker.RelayOnce(ctx, db, 20, func(publishCtx context.Context, event store.OutboxEvent) error {
				return publisher.publish(publishCtx, worker.Encode(event), 0)
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("outbox relay failed", "error", err)
			}
		}
	}
}

func consumeLoop(ctx context.Context, db *store.Store, channel *amqp.Channel, publisher *publisher, queue string, log *slog.Logger) {
	deliveries, err := channel.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		log.Error("consumer failed", "error", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-deliveries:
			if !ok {
				log.Warn("consumer channel closed")
				return
			}
			handleDelivery(ctx, db, publisher, delivery, log)
		}
	}
}

func handleDelivery(ctx context.Context, db *store.Store, publisher *publisher, delivery amqp.Delivery, log *slog.Logger) {
	event, err := worker.Decode(delivery.Body)
	if err != nil {
		log.Warn("discarding malformed event", "error", err)
		_ = delivery.Nack(false, false)
		return
	}
	if err := worker.Consume(ctx, db, event); err != nil {
		attempt, _ := delivery.Headers["x-attempt"].(int64)
		attempt++
		if attempt >= maxAttempts {
			log.Error("dropping event after bounded retries", "eventId", event.ID, "attempt", attempt, "error", err)
			_ = delivery.Nack(false, false)
			return
		}
		if err := publisher.publish(ctx, delivery.Body, int(attempt)); err != nil {
			log.Warn("could not requeue event", "eventId", event.ID, "error", err)
			_ = delivery.Nack(false, false)
			return
		}
		_ = delivery.Ack(false)
		return
	}
	_ = delivery.Ack(false)
}
