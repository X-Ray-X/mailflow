# Proposal

## Why

The worker crash-loop fix added a health gate using `.State.RestartCount`, but
`docker inspect` exposes `RestartCount` at the top level. The template errored,
the value was empty, and the deploy failed at "wait for health" even though the
worker was healthy.

## What Changes

- Read `RestartCount` from the top level of `docker inspect`.
- Keep the requirement that the worker must be running with zero restarts.

Out of scope: other health fields, application code, Compose changes, and mail
infrastructure.

## Capabilities

None new.

### Modified Capabilities

- `service-deployment`: clarifies that deploy health checks read fields that
  exist in the Docker inspect schema.

## Impact

- One line in `.github/workflows/deploy-app.yml`.
