# Proposal

## Why

The published contract is already documented in `docs/api.md` and exercised by
`verify-api.py`, but local development has no interactive way to inspect and try
the endpoints. A locally generated Swagger UI would add a small amount of polish
and make it easier to explore the API without writing `curl` commands.

## What Changes

- Add Swagger annotations as comments on the HTTP handlers and public DTOs.
- Add a local script that generates `swagger.json` and `swagger.yaml` into an
  ignored directory using a pinned `swag` toolchain image.
- Add an optional loopback-only Swagger UI container that reads the generated
  spec, with credentials entered at runtime.
- Document the local workflow and state explicitly that the generated spec is a
  development convenience, not a source of truth.

Out of scope: serving Swagger from the Go service, adding a Swagger route to the
published API, committing generated spec files, changing CI or deploy,
publishing a third contract artifact, and any behavior change to `/v1`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This change declares `skip_specs: true` because it is developer tooling
only.

## Impact

- `app/internal/api/`: documentation comments only.
- `app/scripts/swagger.sh`, `.gitignore`, and local docs.
- No runtime dependency, no deployment change, no specs to sync.
