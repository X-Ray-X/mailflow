# Design

## Context

See `proposal.md` for motivation. The deploy generates `.env` from
`.env.example`, substitutes secrets, then applies the Compose project. The
worker connects to RabbitMQ using `MAILFLOW_RABBITMQ_URL`. The existing health
loop checked `.State.Running` for the worker, unlike the other services which
have real health checks.

## Goals / Non-Goals

**Goals:** make broker credentials internally consistent by construction, and make the deploy verdict reflect a worker that stays up.

**Non-Goals:** rotating the broker password, changing the default username, changing Compose, or tracing the worker.

## Decisions

### Derive the URL user from the configured broker user

The prepare step reads `RABBITMQ_USER` from the generated `.env` and uses it in
the sed substitution that builds `MAILFLOW_RABBITMQ_URL`. The alternative —
hardcoding `mailflow` — caused this incident because the tracked default is
`mailflow-local`. A post-substitution assertion compares the two and fails the
deploy if they differ.

### Require zero restarts in the health gate

The loop now checks `.State.Status == running` and `.State.RestartCount == 0`
for the worker. The alternative — adding a fake HTTP healthcheck to the worker
image — would be misleading because the worker serves no HTTP. A restart is an
unambiguous signal that the process failed after startup.

### Keep the broker credential set unchanged

The existing volume was initialized with the tracked default user; changing it
would require reinitializing application state. The fix changes only how the
URL is assembled, so existing volumes keep working.

## Risks / Trade-offs

- A worker restarted once by an external operator would fail the next deploy's
  health gate until its container is recreated. Compose recreates it on each
  deploy, so the count resets for this workflow.
- The deploy still does not verify message processing end to end; it verifies
  the worker stays connected at startup. The live outbox verification covers
  processing separately.
