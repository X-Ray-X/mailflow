# Tasks

## 1. Local spec generation and UI

- [x] 1.1 Add Swagger annotations to the HTTP handlers and public DTOs in
  `app/internal/api/`, then verify a generated spec contains every registered
  `/v1` route and no vendor field in its schemas.
- [x] 1.2 Add `app/scripts/swagger.sh` to generate JSON/YAML into an ignored
  `app/docs/generated/` directory and optionally start a loopback-only
  Swagger UI container; verify the script regenerates cleanly and the UI
  serves the spec on `127.0.0.1:8088`.
- [x] 1.3 Document the local workflow and the "generated, not authoritative"
  boundary in `docs/api.md` and `README.md`; verify the documented commands.
- [x] 1.4 Run `./app/scripts/go.sh` and `openspec validate --all --strict`;
  review the diff for runtime changes, committed generated files, and secrets,
  then commit on the `mailflow-local-swagger` branch with the verification
  result.

## Workflow follow-up

- PR, merge after CI, then archive the change. No specs delta to sync.
