# Tasks

Branch: `mailflow-send-and-flags`. Each task lists how its completion is
verified; a task is done when that check has been run and its result known.

## 1. Probe the pinned release's write surface

- [x] Exercise, against the running stack on the pinned release, `Identity/get`,
      `Email/set` create with `bodyValues`, `EmailSubmission/set` with an
      explicit envelope, `onSuccessUpdateEmail`, and a per-keyword
      `keywords/$seen` patch.
- [x] Record the exact request and response shapes, the observed role of the
      drafts and sent mailboxes, whether a keyword patch preserves
      `$hasattachment`, and whether submission itself moves the message out of
      Drafts, in `docs/integration-surface.md`.
- [x] Remove the corresponding lines from its "Not verified" section, leaving
      anything still unprobed listed.

**Verify:** the document contains the commands used and the observed JSON;
`openspec instructions` is not needed, but each claim in the new section is
traceable to a command in it.

## 2. Add the database to the application deployment unit

- [x] Add a `db` service to `deploy/app/compose.yaml`: arm64-native pinned image
      with its digest recorded, data in an application-owned volume, no
      published port, a `pg_isready` health check, bounded logging, and the
      application's private network.
- [x] Make `api` depend on a healthy `db` and pass the `MAILFLOW_DB_*` values.
- [x] Add the new keys to `deploy/app/.env.example`, and have
      `.github/workflows/deploy-app.yml` write `MAILFLOW_DB_PASSWORD` from the
      production environment into the generated `.env`.
- [x] Update the application structural gate in `.github/workflows/ci.yml` to
      expect exactly the service and the database, and to fail on a published
      database port, a non-application-owned volume, or any mail path.
- [x] Create `docs/application-database.md` with the pinned image and digest, the
      volume name, the network, and the no-published-port rule.

**Verify:** `cd deploy/app && docker compose --env-file .env.example config`
renders both services; the updated Python gate passes against that render; a
deliberately added `ports:` entry for `db` makes it fail; the digest recorded
matches `docker buildx imagetools inspect` for the pinned tag. The live deploy
step is only verified on the next real deployment and must be reported as such.

## 3. Add database configuration and the idempotency store

- [x] Add `MAILFLOW_DB_HOST`, `_PORT`, `_NAME`, `_USER`, `_PASSWORD`,
      `MAILFLOW_IDEMPOTENCY_TTL`, and `MAILFLOW_IDEMPOTENCY_LEASE` to config,
      validated at startup, with the lease required to exceed the request
      timeout; never log a value or the assembled connection string.
- [x] Add `internal/store` with a `pgxpool` pool, a bounded startup ping, the
      embedded schema bootstrap under an advisory lock, and the claim/replay/
      conflict/lease operations described in `design.md`.
- [x] Add the janitor that deletes expired rows in bounded batches and stops on
      context cancellation.
- [x] Wire the pool into `main.go`, fail startup when it is unreachable, and
      close it on shutdown.

**Verify:** `./app/scripts/go.sh` passes; config tests cover a missing password,
a non-numeric port, and a lease shorter than the timeout; starting the stack
creates `send_idempotency` (`psql -c '\d send_idempotency'`); a second start
leaves it unchanged; a missing password exits non-zero naming the variable and
prints no password.

## 4. Implement sending in the mail adapter

- [x] Extend the JMAP client to send one envelope with create, submission, and
      success-update calls using back-references, keeping the observed shapes
      from task 1.
- [x] Resolve the sender from `Identity/get`, refuse when the caller has no
      identity, and never accept a sender from the request.
- [x] Translate the result to a Mailflow message identifier; on submission
      failure, attempt to destroy the created draft and log a failed
      compensation.
- [x] Add an `ErrRejected` failure class for refusals and map it to the HTTP
      layer's `send_rejected`.
- [x] Keep the adapter behind an interface so the HTTP layer is testable without
      a mail server.

**Verify:** `./app/scripts/go.sh` passes, and unit tests with a fake upstream
assert the emitted method calls, the sender resolution, the compensation call on
failure, and the `ErrRejected` mapping.

## 5. Add `POST /v1/messages`

- [x] Validate the request: at least one recipient, recipient count and body
      size within the configured bounds, recipients inside the caller's domain,
      no caller-supplied sender, and a well-formed `Idempotency-Key` (1–200
      visible ASCII characters).
- [x] Claim the key, perform the send, record success, and answer `201` with the
      Mailflow identifier; replay with `Idempotency-Replayed: true`; answer
      `conflict` and `in_progress` for the two duplicate cases.
- [x] Answer `service_unavailable` without sending when the store cannot be
      reached.
- [x] Document the endpoint, the header, the request and response examples, the
      new codes, and the retention/replay window in `docs/api.md`.

**Verify:** `./app/scripts/go.sh` passes with table-driven tests for every
scenario above; `curl` as alice sending to bob returns `201`, the identifier is
fetchable with `GET /v1/messages/{id}`, and bob's inbox lists it.

## 6. Add `PATCH /v1/messages/{id}`

- [x] Accept `read` and `starred`, require at least one, refuse unknown fields,
      refuse a foreign or unknown identifier with `not_found`, and return the
      updated summary.
- [x] Patch only the named keywords so a flag change preserves draft and
      attachment state.
- [x] Document the endpoint, its request shape, and its error behavior in
      `docs/api.md`.

**Verify:** `./app/scripts/go.sh` passes; live requests mark a message read and
starred and a repeated request is a no-op; a message with an attachment still
reports `hasAttachment` after both flips; an unknown field answers
`invalid_request`.

## 7. Extend the contract verifier

- [x] Add checks to `app/scripts/verify-api.py`: send alice→bob, the same key
      replayed (one message, replay header), the same key with a different body
      (409), a missing key, an out-of-domain recipient, bob reading and starring
      the received message, and the sent copy appearing in alice's `sent`
      mailbox.
- [x] Include every write response in the vendor-vocabulary scan and assert the
      new codes are documented.

**Verify:** `./app/scripts/verify-api.py --base http://127.0.0.1:8080` reports
every check PASS against the running stack, including the existing read checks.

## 8. Document operations and close the loop

- [x] Add `MAILFLOW_DB_PASSWORD` and the new endpoints to `README.md`, and record
      the write surface as current instead of "next change".
- [x] Extend `docs/application-database.md`: retention and journal behaviour, a
      `pg_dump` backup and restore recipe, what a stale restore means for
      idempotency, and the no-port/private-network rule.
- [x] Run the completion gate and commit on the feature branch with the
      verification result, naming anything that was not verified.

**Verify:** `./app/scripts/go.sh`, `openspec validate mailflow-send-and-flags
--strict`, and the structural gate all pass; `git status` shows no `.env`, mail
data, or credentials; the commit message states what was verified live and what
was not.
