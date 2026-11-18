# Tasks

## 1. Upstream surface

- [x] 1.1 Probe the pinned release with `bob@mailflow.test` and the existing
  attachment message: read `Email/get` with the attachments property, confirm
  metadata fields and stable ordering across two reads, and fetch one blob
  through the session's `downloadUrl` using the caller's credentials. Record the
  commands, observed responses, failure cases, and anything still unexercised in
  `docs/integration-surface.md`; verify each statement against the probe transcript.
- [x] 1.2 Add attachment metadata and blob download to `app/internal/jmap/mail.go`
  and the session type, including HTTPS/host validation of the expanded download
  URL and error mapping; verify with fake-server unit tests that the metadata
  request is explicit, the URL is built from the observed template, a foreign
  host is refused, and an upstream failure stays in the transport error classes.

## 2. Adapter and contract

- [x] 2.1 Add read-only attachment metadata and a bounded download to
  `app/internal/mail`, with the caller's account checked against the message
  identifier before any upstream call; verify unit tests cover a readable
  attachment, a foreign message, an unknown message, an out-of-range position,
  and an upstream failure.
- [x] 2.2 Publish `GET /v1/messages/{id}/attachments` and
  `GET /v1/messages/{id}/attachments/{index}` with Mailflow-only DTOs, a bounded
  streamed response, a sanitised download filename, `Content-Type`,
  `Cache-Control: no-store`, and `X-Content-Type-Options: nosniff`; verify API
  tests cover success, empty list, foreign and unknown messages, malformed and
  unknown positions, a too-large upstream body, and a vendor-vocabulary scan of
  the response headers and body.

## 3. Live verification and close-out

- [x] 3.1 Update `docs/api.md` and `README.md` with both endpoints, the
  position-addressing rule, the size bound, and the fact that attachments are
  read-only; verify the documented examples against the running local stack with
  the demo credentials in `deploy/mail/.env` and the existing attachment message.
- [x] 3.2 Extend `app/scripts/verify-api.py` with attachment listing, a real
  download, a foreign message, a malformed position, an unknown position, and a
  vendor-vocabulary scan of the new responses; verify the script exits
  successfully against `http://127.0.0.1:8081` and scans every new response.
- [x] 3.3 Cross-check one attachment over IMAP by fetching the same message's
  structure and body part, and confirm the bytes match the HTTP download;
  record the command and result in `docs/integration-surface.md`.
- [ ] 3.4 Run `./app/scripts/go.sh` and `openspec validate --all --strict`;
  verify formatting, vet, unit tests, and all specs pass, and state any check
  that could not run. Review `git status` and the full diff for secrets,
  unbounded reads, unhandled errors, and data-loss paths, then commit on the
  `mailflow-attachments` branch with the verification result in the message and
  verify the tree is clean.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
