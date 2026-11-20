# Design

## Context

See `proposal.md` for motivation. The service uses `net/http.ServeMux` and
publishes a stable `/v1` contract. `docs/api.md` is the human contract and
`openspec/specs/` is the authoritative behavior record. There is no OpenAPI
artifact today.

## Goals / Non-Goals

**Goals:** let a developer generate an accurate spec from the handler code and
browse it locally without touching the deployed service.

**Non-Goals:** publishing the spec, serving a UI from the Go binary, generating
clients, adding CI checks that gate merges, or changing any endpoint.

## Decisions

### Generate from handler annotations into an ignored directory

`swaggo/swag` runs in a pinned Go image and writes only `swagger.json` and
`swagger.yaml` under `app/docs/generated/`, which is gitignored. The alternative
— committing a hand-written OpenAPI file — would create a third source of truth
that could drift from `docs/api.md` and the code. Generating on demand means the
local view always reflects the current handlers.

### Serve the UI in a separate ephemeral container

`swaggerapi/swagger-ui` runs on `127.0.0.1:8088` with the generated JSON bind-mounted
read-only. This is a local-only bind mount, acceptable because it does not run on
the self-hosted runner or in Compose. The alternative — adding a Swagger route to
the Go service — would add a runtime dependency and a published endpoint for a
developer convenience.

### No specs delta

The change declares `skip_specs: true`. It changes no observable behavior, so
there is nothing to merge into `openspec/specs/`. Adding a requirement such as
"the system shall offer local Swagger UI" would mix developer tooling into the
authoritative behavior spec.

### Annotations are documentation, not validation

The swagger comments describe the existing DTOs and handlers. `docs/api.md` and
`verify-api.py` remain the contract description and its executable check. A
local script compares the generated path list with the registered routes so a
missing annotation is visible, but this is not a merge gate.

## Risks / Trade-offs

- **Annotations can go stale.** Regenerating on every use and checking the path
  list keeps this honest; the consequence of drift is local only.
- **Loopback port 8088 must be free.** The script checks before starting the
  container and refuses to publish beyond loopback.
- **Credentials in the browser.** Swagger UI keeps them for the page session;
  no credential is stored in the repo, image, or script.
