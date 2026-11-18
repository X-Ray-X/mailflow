# Proposal

## Why

The pull-request workflow only checks Compose structure. It does not compile the
Go service, build or test the web client, validate OpenSpec artifacts, or notice
that the application project now includes a static client. The deploy workflow
also does not build the client image, so a deploy cannot publish the UI.

## What Changes

- Add pull-request gates for Go formatting, vet, and unit tests; web client
  dependency install, TypeScript build, and unit tests; Python script syntax;
  and `openspec validate --all --strict`. Every gate runs in a pinned toolchain
  image so the runner's host tooling does not decide the result.
- Update the application Compose gate for the `web` service and its non-internal
  frontend network, while keeping the invariants that the database is private
  and no mail directory is mounted.
- Build the web image in the application deploy workflow, set
  `MAILFLOW_WEB_IMAGE` in the generated `.env`, and verify the client container
  is running without changing the mail infrastructure.
- Document the required checks and what each one proves.

Out of scope: load testing, pushing images to a registry, changing the mail
deployment workflow, adding proprietary CI services, and any change to the
published API or application behaviour.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployment-pipeline`: adds code-quality gates and coverage of the static
  client in CI and deployment.

## Impact

- `.github/workflows/ci.yml` and `.github/workflows/deploy-app.yml`.
- `docs/` for the gate inventory and the deploy summary.
- No application code, database schema, or mail deployment change.
