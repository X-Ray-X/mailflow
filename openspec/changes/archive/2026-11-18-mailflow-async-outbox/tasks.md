# Tasks

## 1. Durable outbox

- [x] 1.1 Extend the idempotency recorder and PostgreSQL store with a
  transactional event write: success plus one outbox row, with a replay creating
  no second row; verify store tests against a disposable database and unit tests
  for the recorder interface.
- [x] 1.2 Add the outbox and event tables and bounded claim/publish/consume
  queries to the store; verify store tests cover idempotent consumption,
  published marking, and a bounded batch.

## 2. Broker and worker

- [x] 2.1 Add a Go worker command with a bounded relay loop and consumer loop
  using the pinned AMQP client, decoding only identifier-shaped events; verify
  unit tests cover event decoding, duplicate consumption, and bounded retry
  behavior.
- [x] 2.2 Add RabbitMQ and worker services to `deploy/app/compose.yaml` with
  generated credentials and no published broker port, and wire the new image
  binary and deploy workflow; verify the compose gate, workflow YAML, and a live
  deploy-time render.

## 3. Verification and close-out

- [x] 3.1 Update `docs/api.md`, `docs/application-database.md`, and `README.md`
  with the outbox semantics, worker operation, credentials, and failure
  behavior; verify documented commands against the running stack.
- [x] 3.2 Send a message through the live API and confirm the event reaches
  RabbitMQ and the event table, that a replay adds no row, and that stopping the
  broker leaves sends working; record commands and results.
- [x] 3.3 Run `./app/scripts/go.sh`, the web build/tests, and
  `openspec validate --all --strict`; review the diff for secrets, unbounded
  loops, unhandled errors, and mailbox-state changes, then commit on the
  `mailflow-async-outbox` branch and verify the tree is clean.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
