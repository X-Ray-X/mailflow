# Design

## Context

See `proposal.md` for motivation. The API already records send success in
PostgreSQL and replays it by idempotency key. The application Compose project
has an internal data network and a generated `.env`. There is no broker or
background worker yet. Stalwart owns mailbox state and must not be touched by
this change.

## Goals / Non-Goals

**Goals:**

- Attach a durable event to each completed send without changing the HTTP
  contract or coupling send availability to RabbitMQ.
- Demonstrate at-least-once delivery with idempotent consumption.
- Keep all new state in the application database.

**Non-Goals:**

- Making sends asynchronous from the client's point of view.
- Exposing events via HTTP, webhooks, or delivery receipts.
- Retry strategies beyond the worker's next bounded poll.

## Decisions

### Transactional outbox instead of publishing from the request

`Complete` gains an event parameter and writes the idempotency success and an
outbox row in one transaction. The alternative was publishing to RabbitMQ from
the handler; it would make a broker outage fail a send that already happened, or
silently drop the event. A second alternative — polling RabbitMQ as the source
of truth — would let the broker own application state. The outbox stays in
PostgreSQL, which already owns idempotency records.

### One worker process, two bounded loops

The worker relays unpublished rows to a durable queue and consumes that queue
into `send_events`. The relay acknowledges a publish before marking the row
published; the consumer inserts with `ON CONFLICT DO NOTHING` and acknowledges
only after the insert. The alternative was two separately deployed processes; it
would add deploy surface without changing the failure semantics. Both loops use
request contexts with timeouts and never run unbounded retries.

### The queue carries identifiers, not mail

The message body is `{"account": "...", "messageId": "msg1_...", "eventId": 42}`.
The alternative was including the message content or a mail server identifier;
it would make the queue a second mailbox store and risk leaking vendor values
into logs. No mail protocol is called by the worker.

### Generated broker credentials on the private network

RabbitMQ joins the application's internal data network with no published port.
User and password come from the generated `.env`, produced by the deploy
workflow from secrets, with tracked only as placeholders. The alternative was
the default guest account on a published management port, which would widen the
attack surface and violate the loopback-only rule.

### The API keeps answering when the broker is down

Only the worker touches RabbitMQ. If the broker is unavailable, outbox rows stay
unpublished and are retried on the next poll; sends and replay continue to work.
The alternative — failing sends when publishing fails — would turn a non-critical
audit event into a mail outage.

## Risks / Trade-offs

- **The event may be delivered more than once.** At-least-once delivery is
  intentional; the event table's primary key on the outbox id makes consumption
  idempotent.
- **A publish can succeed but the published mark fail.** The row is retried and
  the duplicate is deduplicated by the consumer. Both paths are bounded.
- **The worker needs a broker to make progress.** That is acceptable because it
  is not in the request path; the outbox retains the event until it recovers.
- **New infrastructure on the homelab host.** RabbitMQ runs as a container in
  the application project, with no volumes holding mail and no published ports.
