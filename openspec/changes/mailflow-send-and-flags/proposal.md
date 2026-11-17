# Proposal

## Why

The service publishes only reads. A user can list and open mail but cannot reply,
send, mark read, or star a message, so the product cannot replace a mail client
yet. Sending is also the first operation with an external side effect: a retry
after a timeout could deliver twice, which is worse than failing. The read
contract deliberately deferred this work until the upstream write surface could
be verified.

## What Changes

- Add `POST /v1/messages` to send a plain-text message from the authenticated
  account. The sender is the authenticated account; a client cannot name another
  sender. Recipients are restricted to the caller's own mail domain.
- Require an `Idempotency-Key` on send. A repeated key with the same request
  returns the original result instead of sending again; the same key with a
  different request is refused.
- Record idempotency state in PostgreSQL, owned by the application deployment
  unit. It is Mailflow application state, never mailbox content, and it survives
  an application redeploy or rollback.
- Add `PATCH /v1/messages/{id}` to set read and starred state. The request states
  the desired value, so repeating it is naturally idempotent.
- Add error codes for a refused send, a key reused with a different request, and
  a duplicate request still in flight. Writes fail with `service_unavailable`
  when the record store is unavailable; reads keep working.
- Version the write surface under the existing `/v1` path, additively.

Out of scope: attachments, HTML bodies, drafts, reply/forward threading, moving
or deleting messages, search, push, and public internet mail. Sending outside
the local domain is out of scope and refused.

## Capabilities

### New Capabilities

- `application-state`: what the application may keep in PostgreSQL (idempotency
  records, later outbox and projections), where it lives, how it survives
  deployment, and that mailbox contents never live there.

### Modified Capabilities

- `mail-api`: adds the write contract (send, flag updates), its validation,
  idempotency behavior, and error codes, and renames the versioned-contract
  requirement because the contract is no longer read-only.
- `service-deployment`: the application deployment unit now includes a database
  and its volume, the deploy preserves that state, and the structural gate
  covers the second service.

## Impact

- `app/`: new HTTP routes and validation, send and flag operations in the mail
  adapter, a small PostgreSQL store, startup configuration and connectivity
  checks, and unit tests.
- `deploy/app/compose.yaml` and `.env.example`: a database service, an
  application-owned volume, and connection settings. No published database port.
- `.github/workflows/deploy-app.yml` and `ci.yml`: the application secret for the
  database password, and a structural gate that now expects two services.
- `README.md`, `docs/api.md`, `docs/integration-surface.md`, and a new operational
  page for the application database.
- Dependencies: one PostgreSQL driver in the Go module, and a pinned PostgreSQL
  image. No change to the mail deployment unit, its image, or its volumes.
