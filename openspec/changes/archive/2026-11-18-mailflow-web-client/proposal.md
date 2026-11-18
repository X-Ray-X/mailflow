# Proposal

## Why

The service publishes a complete mail contract but there is no client that uses
it. A first client proves the contract is usable end to end and gives the
homelab a practical way to read and send internal mail without speaking IMAP.

## What Changes

- Add a React/TypeScript single-page client under `web/`, served as static files
  by the application deployment unit.
- Cover the core flows: sign in with mail credentials, browse mailboxes and
  message pages, search, read a message, download attachments, send a message,
  save and send drafts, reply, and patch flags or move/delete a message.
- Use only the published `/v1` contract. The client never speaks IMAP, JMAP, or
  SMTP, and it never renders a mail server identifier or vendor field.
- Keep credentials in browser memory for the session only. No token store, no
  `localStorage` password, and no credentials in logs or URLs.
- Add a `web` service to the application Compose project that serves the built
  files and proxies `/v1` to the API, so the browser uses one origin. The mail
  deployment unit is untouched.
- Add build-time and unit checks for the client, and update the run docs.

Out of scope: mailbox create/rename/delete in the UI, offline mode, push
notifications, HTML mail rendering, attachment upload, reply-all, forward,
themes, and any change to the published API or the mail infrastructure.

## Capabilities

### New Capabilities

- `web-client`: the behavior and boundaries of the first published client.

### Modified Capabilities

None. The `mail-api` contract is consumed as it stands.

## Impact

- `web/`: new client source, build configuration, and tests.
- `deploy/app/`: one new static service and its documented port; the API and
  database services are unchanged.
- `docs/`: run instructions and the client boundary.
- No dependency in the Go service, no database change, and no mail deployment
  change.
