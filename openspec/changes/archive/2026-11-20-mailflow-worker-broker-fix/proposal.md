# Proposal

## Why

A deploy was reported green while the worker was crash-looping. The generated
AMQP URL hardcoded the username `mailflow`, but the broker was created with
`RABBITMQ_USER=mailflow-local` from `.env.example`, so authentication failed.
The health check only inspected `.State.Running`, which is briefly true during
a restart loop, so it did not catch the failure.

## What Changes

- Build the worker's AMQP URL from the configured `RABBITMQ_USER` instead of a
  hardcoded username, and fail early if the generated user and URL disagree.
- Treat a worker that has restarted as unhealthy, and stop a deploy that would
  otherwise leave a crash-looping worker running.
- Add a verification note showing the generated URL user matches the broker user.

Out of scope: changing broker credentials, changing the Compose service set,
changing application code, or touching the mail deployment unit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `service-deployment`: adds credential consistency and worker health as
  deployment gates.

## Impact

- `.github/workflows/deploy-app.yml` only.
- No application code, Compose change, or secret value change.
