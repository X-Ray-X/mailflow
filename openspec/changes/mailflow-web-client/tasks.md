# Tasks

## 1. Client foundation

- [x] 1.1 Scaffold `web/` with Vite, React, TypeScript, a pinned Node image, and
  a Docker build; verify `npm run build` and `npm test` succeed in the pinned
  `node:22-alpine` image with no local Node toolchain.
- [x] 1.2 Implement the typed API client for all endpoints the MVP uses,
  including Basic auth, error codes, replay headers, and per-submission
  idempotency keys; verify unit tests cover request shapes, error mapping, and
  replay reporting.

## 2. Core flows

- [x] 2.1 Build the signed-in shell with mailbox list, message pages, search,
  message detail, and attachment download; verify unit tests cover search-query
  building, page state, and the attachment URL, and build the client.
- [x] 2.2 Add compose/send, save draft, send draft, reply, and message
  patch/move/delete with a post-write refresh; verify unit tests cover the
  idempotency key, draft identifiers, and error handling, and build the client.

## 3. Deployment and close-out

- [x] 3.1 Wire a `web` service into `deploy/app/compose.yaml` with a loopback
  port and a same-origin nginx proxy to the API; verify the built image serves
  the SPA and that a proxied `/v1/mailboxes` request with demo credentials
  succeeds on the published host port.
- [x] 3.2 Update `docs/api.md`, `docs/web-client.md`, and `README.md` with the
  client's scope, credential handling, run command, and the manual verification
  pass; verify the documented commands against the running local stack.
- [ ] 3.3 Review `git status` and the full diff for secrets, unbounded requests,
  unhandled promises, and data-loss paths, then commit on the
  `mailflow-web-client` branch with the verification result and state which
  interactive checks could not be automated.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
