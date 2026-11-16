# Proposal

## Why

The mail infrastructure is deployed and verified, but the piece that defines this
project is missing: the Go service that publishes Mailflow's own contract. Today the
only way to read mail is a standard IMAP or JMAP client, so every future consumer —
the React client, any integration — would be coupled to Stalwart's protocols, object
types, and identifiers. Until the service exists, "the mail server is replaceable" is
an assertion rather than a property.

This change builds the read half of that boundary. It is deliberately the smaller
half: reads need no idempotency records and no database, so they establish the
contract and the adapter without also introducing application state.

## What Changes

- Introduce the Go service as a new deployment unit that adapts to Stalwart through
  its supported JMAP interface and never reads or writes its storage.
- Publish the **v1 read contract**: list mailboxes, list the messages in a mailbox,
  and fetch one message with its text content.
- Give every object a Mailflow identifier and a Mailflow vocabulary: `read`,
  `starred`, `hasAttachment`, not IMAP flags or vendor keywords.
- Authenticate callers against Stalwart and scope every response to the caller's own
  account, with no user store in the service.
- Deploy the service as its own Compose project through its own workflow, so an
  application deploy cannot recreate, stop, or upgrade mail containers or volumes.
- State upstream failures as Mailflow errors with bounded timeouts, never as vendor
  error text or codes.

Out of scope for this change: sending and submission, flag and mailbox mutations,
attachment download, full-text search, push or streaming updates, PostgreSQL and
any durable application state, the React client, and public internet mail. Those are
later changes; the write path in particular must first verify the JMAP methods that
`docs/integration-surface.md` records as unexercised.

## Capabilities

### New Capabilities

- `mail-api`: the published read contract — resources, identifiers, ordering,
  pagination, error shape, versioning, and the guarantee that no vendor object type,
  identifier, or protocol vocabulary appears in a public payload.
- `api-authentication`: how a caller proves which mailbox it may read, that Stalwart
  remains the only authority for credentials, and how authentication failures are
  reported without leaking vendor detail.
- `service-deployment`: the application as a deployment unit of its own — image,
  configuration, health, and a pipeline that cannot affect the mail infrastructure.

### Modified Capabilities

None. The mail infrastructure's own requirements do not change. Its existing
scenario "Application deployment leaves mail untouched" has been recorded as not
executed because no application stack existed; this change makes it executable.

## Impact

- New Go module for the service, plus its tests, in a directory of its own.
- New Compose project for the application, separate from `deploy/mail/`.
- New deployment workflow for the application, separate from the mail workflow.
- New contract documentation; `README.md` gains the service and its quickstart.
- The mail infrastructure, its Compose project, its workflow, its data, and its
  published ports are untouched.
- No database is introduced, so there is no migration or backup story to change.
