# Proposal

## Why

Send bookkeeping currently happens inside the request path, and there is no
durable way to react to an accepted message. A queue and workers let the service
record a send event once and process it asynchronously without making the mail
server or a broker the source of mailbox truth.

## What Changes

- Add a transactional outbox row in the application database when a send or
  draft submission is recorded. The row is written in the same transaction as
  the idempotency completion, so a completed send cannot lose its event.
- Add RabbitMQ to the application Compose project on the private network, with
  generated credentials and no published port.
- Add a Go worker process that relays unpublished outbox rows to RabbitMQ and
  consumes them into a `send_events` table. Both loops use bounded calls, manual
  acknowledgement, and idempotent inserts.
- Keep the published HTTP contract unchanged. The event is application state
  only; it never mirrors a mailbox.
- Document the queues, credentials, retention, and failure behavior.

Out of scope: changing send semantics to return before submission, exposing
events through the API, delivery receipts, webhooks, retries with backoff beyond
the worker's next poll, registry pushes, and any change to the mail deployment
unit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `application-state`: adds an outbox and an event table, both application
  state only.
- `deployment-pipeline`: deploys the broker and worker with generated secrets
  and verifies them without touching mail.

## Impact

- `app/internal/idempotency`, `app/internal/store`, `app/internal/api`, and a
  new worker command.
- `deploy/app/`: RabbitMQ and worker services, generated broker credentials,
  and the deploy workflow.
- `docs/`: outbox semantics, worker operation, and verification.
- No mail protocol, mailbox state, or HTTP contract change.
