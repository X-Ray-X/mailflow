# Design

See `proposal.md` for motivation and `specs/` for the behavior contract. This
document records how the write surface and its application state are built, and
why each consequential choice was made.

## Context

The service is a stateless HTTP adapter over the mail server's JMAP API. Every
request forwards the caller's Basic credentials; the service keeps no sessions
and no user store. The read path never needs persistent state.

Two facts constrain this change:

- Sending has an external side effect. A timeout after the mail server accepted a
  message leaves the caller unable to distinguish "not sent" from "sent".
- `docs/integration-surface.md` records `Email/set`, `Email/import`, and
  `EmailSubmission/set` as **not exercised** on the pinned release
  (`ghcr.io/stalwartlabs/stalwart:v1.1.0`). The write path must be designed
  against a probe, not against RFC assumptions.

## Goals / Non-Goals

**Goals**

- One write surface under `/v1` that the future React client and any HTTP client
  use, with the same identifier and vocabulary rules as the read surface.
- A retried send is safe by default: callers cannot send twice by repeating a
  request.
- Application state survives an application redeploy without ever touching the
  mail deployment unit.

**Non-Goals**

- Attachments, HTML bodies, drafts, threading, search, push, move/delete.
- Exactly-once delivery. The design removes duplicates caused by client retries
  and keeps any remaining window small, bounded, and documented.
- A durable outbox or background delivery. Sending stays synchronous inside the
  request.

## Decisions

### 1. Write endpoints: state-based, not action-based

`POST /v1/messages` creates a send; `PATCH /v1/messages/{id}` sets `read` and
`starred` to the stated values. A PATCH body names the desired state, not a
transition, so repeating it changes nothing. That makes flag updates idempotent
without an idempotency key.

Alternatives rejected: `POST /messages/{id}/read` and `/star` action endpoints
(one endpoint per verb, and a retry is indistinguishable from a second action);
`PUT /messages/{id}` with the whole message (clients would have to round-trip
fields they do not own, and a stale client could clobber a newer state).

### 2. Idempotency in PostgreSQL, not in memory

A process-local map would be lost on every deploy and restart, which is exactly
when clients retry. The deployed service is restarted by each application
deploy, so in-memory deduplication would be absent after the most likely failure
mode. PostgreSQL is already sanctioned for "idempotency records" and is the
smallest option that survives the lifecycle.

Alternatives rejected: in-memory with TTL (fails the survival scenario);
requiring callers to search their sent mailbox before retrying (race-prone and a
worse contract); a transactional outbox with a delivery worker (durable retries
are valuable, but they change the endpoint's timing contract and add a worker —
a later change, not this one).

### 3. Claim, act, record; unique key per account

One table, `send_idempotency`, keyed by `(account_name, key)`. `account_name` is
the authenticated username, which is stable; the mail server's opaque account id
is not stored.

Flow:

1. `INSERT ... ON CONFLICT (account_name, key) DO NOTHING`.
2. No row inserted → read the existing row and decide:
   - request hash differs → `conflict`;
   - `succeeded` and hash matches → replay the stored identifier with
     `Idempotency-Replayed: true`;
   - `in_progress` and the claim is younger than the lease → `in_progress`;
   - `in_progress` and the claim is older than the lease → reclaim with a
     conditional `UPDATE` and proceed.
3. Row inserted → perform the submission, then set `succeeded`, the Mailflow
   message id, and the completion time in one statement.

The unique constraint, not a read-then-write check, resolves concurrent
duplicates: two processes cannot both insert. Time is taken from the database
(`now()`), so clock skew between replicas cannot expire a claim early.

Alternatives rejected: `SELECT` then `INSERT` (a race between two requests);
advisory locks (tied to a connection, and a crashed process leaves no record);
letting the mail server deduplicate (no such key exists in JMAP submission).

### 4. Schema bootstrap: embedded SQL under an advisory lock

One `CREATE TABLE IF NOT EXISTS` and one index, applied at startup under a
session advisory lock so a second replica starting concurrently is serialized.
A migration framework is more machinery than one additive table justifies; the
moment a column needs changing, this decision is revisited with a real migration
tool rather than a growing startup script.

Alternatives rejected: a migration library now (dependency and operational
surface for one table); applying DDL from the deploy workflow (`psql` on the host
would need the password and duplicate startup logic).

### 5. Driver: `pgx` pool directly

`github.com/jackc/pgx/v5/pgxpool` gives bounded pooling, context-aware queries,
and one dependency. Alternatives: `lib/pq` (in maintenance), `database/sql` over
the pgx stdlib adapter (an extra indirection with no benefit for one table).

### 6. Database configuration as discrete values

`MAILFLOW_DB_HOST`, `_PORT`, `_NAME`, `_USER`, `_PASSWORD`, with a required
password and defaults for the rest. A single DSN is fewer variables but requires
a secret to be URL-encoded, and a percent sign in a password becomes a startup
failure that looks like a malformed URL. The service assembles the connection
string internally and never logs it; logs name variables, never values.

The database is a service in the **application** Compose project, not a third
deployment unit. Its lifecycle is the application's, so an application
deploy/rollback never needs to reason about a separate stack, and the mail unit
is untouched. The database publishes no port; the application reaches it on the
project's private network.

### 7. Send path over JMAP submission, behind a probe

The intended sequence, matching the capabilities the session advertises:

1. `Identity/get` — resolve the single identity for the authenticated account.
   The sender address comes from here. The caller cannot supply a sender.
2. `Email/set create` — a plain-text message placed in the drafts mailbox.
3. `EmailSubmission/set` — submit the created email with the identity and an
   envelope whose recipients are the validated `to` and `cc`.
4. `onSuccessUpdateEmail` — move the message to the sent mailbox, clear the draft
   flag, and set read state, so the sent copy is correct without a second
   request.

Steps 2–4 are sent in one JMAP envelope with back-references so the create and
submission are one round trip. If the submission step fails, the adapter
attempts to destroy the created draft; if that compensation also fails, the
draft stays in Drafts and is logged — a leftover draft is visible and harmless,
whereas a reported-sent message that was never submitted is not.

`docs/integration-surface.md` records these methods as unverified on the pinned
release. The first task is therefore a probe. If the probe shows a different
shape, the design follows the observed behavior and the document records it; if
submission is not supported at all, implementation stops and reports rather than
falling back to SMTP or inventing configuration.

Alternatives rejected: SMTP submission through a second protocol client (a
second transport, a second credential path, and a second timeout model);
`Email/import` from a locally built MIME blob (needs upload plumbing and makes
the service author MIME, which is more code than `Email/set`); creating the
message directly in Sent and submitting it (Sent would briefly hold an unsent
message if submission fails).

### 8. Keywords are patched, never replaced

Flag updates send a JMAP patch of the named keyword only (`keywords/$seen`,
`keywords/$flagged`). Replacing the keyword map would drop `$draft`,
`$hasattachment`, and any keyword another client set. If the probe shows the
server does not accept per-keyword patches, the fallback is read-modify-write of
the full map within the same request — recorded here so the fallback is a known
decision, not an accident.

### 9. Replay semantics

A replayed send returns the original status (201) and identifier, plus
`Idempotency-Replayed: true`. Returning 200 for a replay would make clients treat
two paths differently; returning a conflict would turn a safe retry into an
error. The header is additive, so a v1 client ignoring it is unaffected.

### 10. Refusals are distinct from outages

The JMAP layer gains an `ErrRejected` class for method errors that mean "this
message will not be accepted" (bad recipient, forbidden sender, too large). The
HTTP layer maps it to 422 `send_rejected`. Mapping it to `service_unavailable`
would invite a retry that will fail the same way; mapping it to
`invalid_request` would blame a request that can be well-formed but undeliverable.

### 11. Health stays as it is

`/healthz` keeps reporting process health only. The database is checked at
startup (bounded ping, then bootstrap), and a later outage degrades sends, not
reads. Folding the database into health would restart a process that can still
serve mail reads, and would contradict the existing health requirement.

### 12. Recipients are restricted by the caller's own domain

A recipient is accepted only if its domain equals the domain of the identity
resolved for the caller. This needs no new configuration, cannot drift from the
mail domain, and keeps the service from being usable as a relay. A configured
domain was rejected for the drift risk.

## Data model

```sql
CREATE TABLE IF NOT EXISTS send_idempotency (
    account_name  text        NOT NULL,
    key           text        NOT NULL,
    request_hash  bytea       NOT NULL,
    state         text        NOT NULL CHECK (state IN ('in_progress', 'succeeded')),
    message_id    text,
    claimed_at    timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz,
    expires_at    timestamptz NOT NULL,
    PRIMARY KEY (account_name, key)
);
CREATE INDEX IF NOT EXISTS send_idempotency_expires_at
    ON send_idempotency (expires_at);
```

`message_id` holds a Mailflow message identifier, so a replay returns the
identifier the client would have received. `request_hash` is SHA-256 over the
validated request (recipients, subject, text), so the same key with a different
body is detected. `expires_at` bounds retention; a janitor goroutine deletes
expired rows in bounded batches and stops on shutdown. No message content is
stored: the hash is one-way and the identifiers are opaque.

## Request and response shapes

`POST /v1/messages`, `Content-Type: application/json`, `Idempotency-Key`
required (1–200 visible ASCII characters):

```json
{
  "to": [{ "name": "Bob", "email": "bob@mailflow.test" }],
  "cc": [],
  "subject": "Lunch",
  "text": "Noon?"
}
```

`201 Created` (or the same body on replay):

```json
{ "id": "msg1_ZA.Ym1hYWFhYWw" }
```

`PATCH /v1/messages/{messageID}`:

```json
{ "read": true, "starred": false }
```

`200 OK` returns the same summary shape the listing uses, with the new state.

New configuration: `MAILFLOW_DB_HOST` (default `db`), `MAILFLOW_DB_PORT`
(default `5432`), `MAILFLOW_DB_NAME` (default `mailflow`), `MAILFLOW_DB_USER`
(default `mailflow`), `MAILFLOW_DB_PASSWORD` (required),
`MAILFLOW_SEND_MAX_RECIPIENTS` (default `50`), `MAILFLOW_SEND_MAX_BYTES`
(default `262144`), `MAILFLOW_IDEMPOTENCY_TTL` (default `24h`),
`MAILFLOW_IDEMPOTENCY_LEASE` (default `2m`). Startup refuses a lease shorter
than the request timeout.

## Risks / Trade-offs

- **A crash after submission but before the success record** can leave an
  `in_progress` row. After the lease, a retry reclaims it and could send twice.
  → The lease is longer than the request timeout, the success record is written
  immediately after submission, and the window is documented in `docs/api.md`.
  A durable outbox would close it fully and is deferred as its own change.
- **An `in_progress` row blocks its key until the lease expires.** → The lease is
  short (default 2 minutes) and the client receives `in_progress`, which is
  retryable, instead of a false success.
- **A failed compensation leaves a draft.** → It is never sent; it appears in
  Drafts and is logged. Losing mail is worse than leaving a draft.
- **Losing the database volume loses deduplication history.** → The volume is
  application-owned and never removed by a deploy; the operational page
  documents a `pg_dump` backup. After a restore from an old copy, a retry of a
  send made after that copy could duplicate. This is stated in the docs rather
  than implied away.
- **The database password reaches the host's `.env` and the container
  environment.** → The file is mode 600 and generated per deploy; the value is
  supplied by the production environment, never committed, and never logged. The
  database publishes no port.
- **Database traffic is unencrypted inside the Compose network.** → It never
  leaves the host's private bridge network and no port is published. TLS between
  the two containers is deferred; if the database is ever moved off the host,
  that decision must be revisited first.
- **The write path rests on an unverified JMAP surface.** → The probe precedes
  any adapter code, its observations are recorded in
  `docs/integration-surface.md`, and an unsupported method stops the change
  instead of producing a fallback nobody specified.
- **The PostgreSQL image could float or drift.** → The image is pinned by exact
  release and digest and recorded next to the Stalwart pin; the structural gate
  asserts the application project still contains exactly the declared services.
- **A stale `Idempotency-Key` reused for a genuinely new message is refused.** →
  That is the intended trade-off: the client must choose a new key per distinct
  message. The contract states it.

## Migration Plan

1. Land the database service, configuration, store, and routes together; the
   read contract is unchanged, so an older client sees no difference.
2. Deploy the application only. The first start creates the volume, bootstraps
   the table, and joins the existing application network. The mail unit is not
   restarted, recreated, or upgraded; the deploy workflow's existing
   mail-unchanged check remains the proof.
3. Rollback: run the previous image. It ignores the database and keeps serving
   reads; the table is additive, so no downgrade is needed. The volume is not
   removed by `compose up --remove-orphans`.
4. Verify with `app/scripts/verify-api.py`: the existing read checks, then the
   new send, replay, conflict, flag, domain-refusal, and vocabulary checks.

## Open Questions

None that change the specs or the task breakdown. The one genuine unknown — the
pinned release's exact write-method behavior — is resolved by the first task and
does not change the endpoint contract.
