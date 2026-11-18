# application-state Specification

## Purpose

Defines what Mailflow keeps in its own database, so application state such as
idempotency records survives deployment without becoming a second store of
mailbox truth.

## Requirements

### Requirement: Application state is not mailbox state

The application database SHALL hold only Mailflow application state, such as
idempotency records and later outbox entries or projections. It SHALL NOT hold
message bodies, subjects, participant addresses, mailbox membership, message
flags, or account credentials.

#### Scenario: Mail activity leaves only application state

- **WHEN** an account has sent messages, changed flags, and read its mail
- **THEN** the database contains no subject, body, participant address, or flag
  state for those messages

#### Scenario: Mailbox state is still read from the mail server

- **WHEN** the database is emptied and the service restarts
- **THEN** mailboxes and messages remain readable, because the mail server is
  still the only source of mailbox state

### Requirement: Application state belongs to the application unit

The database SHALL be part of the application deployment unit, reachable only
from that unit's private network, and SHALL NOT publish a host port or accept
connections from the mail project.

#### Scenario: The database is not reachable from the host

- **WHEN** the host's published ports are inspected
- **THEN** no port forwards to the application database

#### Scenario: Stopping the application does not stop mail

- **WHEN** the application unit, including its database, is stopped
- **THEN** the mail server keeps serving clients

### Requirement: Application state survives deployment

The database's data volume SHALL be application-owned and SHALL survive an
application deploy, restart, or rollback. A failed or repeated deployment SHALL
NOT discard records that earlier requests depend on.

#### Scenario: A retried send still resolves after a deploy

- **WHEN** a send succeeds, the application is redeployed, and the same
  idempotency key is retried with the same request
- **THEN** the service returns the original result and sends nothing again

#### Scenario: A rollback keeps the records

- **WHEN** the application is rolled back to an earlier commit
- **THEN** the database volume and its records are unchanged

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
