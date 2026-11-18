# Spec Delta

## ADDED Requirements

### Requirement: Completed sends have a durable outbox event

When the service records a completed send or draft submission, it MUST write an
event row in the same database transaction as the idempotency success. A replay
MUST NOT create a second event. The event MUST contain the caller's account and
the Mailflow message identifier, and MUST NOT contain mailbox content.

#### Scenario: A send is recorded

- **WHEN** a send is accepted and completed
- **THEN** exactly one outbox row exists for that send alongside its success
  record

#### Scenario: A send is replayed

- **WHEN** the same idempotency key is used again
- **THEN** no additional outbox row is created

### Requirement: A worker relays and consumes events asynchronously

A Go worker MUST publish unpublished outbox rows to RabbitMQ and consume them
into an event table. Publishing MUST be acknowledged before a row is marked
published. Consuming MUST be idempotent, acknowledged only after the insert
succeeds, and MUST NOT change mailbox state or call a mail protocol.

#### Scenario: A message is sent

- **WHEN** the worker processes the outbox
- **THEN** the event appears in RabbitMQ and then in the application event table,
  with the same message identifier

#### Scenario: Consumption is repeated

- **WHEN** the same event is delivered twice
- **THEN** the event table still contains one row for it

#### Scenario: Broker is unavailable

- **WHEN** RabbitMQ cannot be reached
- **THEN** outbox rows remain unpublished, the API keeps answering normally, and
  the worker retries on its next bounded poll

### Requirement: Queue credentials are deployment state

RabbitMQ MUST run on the application's private network with no published port.
Its credentials MUST be generated at deploy time into the generated `.env`, and
MUST NOT appear in tracked files, images, or logs.

#### Scenario: The application is deployed

- **WHEN** the application Compose project starts
- **THEN** the broker and worker are present, reachable only inside the project,
  and configured from generated values
