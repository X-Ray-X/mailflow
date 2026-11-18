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
