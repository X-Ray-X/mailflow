# Mailflow web client

A small React/TypeScript single-page client for the published API. It is the
first consumer of the contract: it speaks only `/v1`, never IMAP, JMAP, or SMTP,
and it shows Mailflow fields only. It deliberately does not expose mail server
blob identifiers, query states, or protocol keywords.

## What it can do

- Sign in with a mail account's address and password.
- List mailboxes and paged message lists; search by text, optionally within the
  selected mailbox.
- Read a message's subject, sender, recipients, and text body, and download its
  attachments.
- Send a plain-text message with a fresh idempotency key, save a draft, send a
  draft, and reply to a message.
- Toggle read/starred, move a message to another mailbox, and delete it to trash.

Out of scope: mailbox administration, HTML mail, attachment upload, push, reply
all, forward, and offline mode. A draft cannot be edited in place; save a new
one and delete the old one, as described in [`api.md`](api.md).

## Credentials

Credentials are entered in the sign-in form and held in React state for the
current session. They are sent as HTTP Basic authentication on every request,
never placed in a URL, browser storage, or logs. Reloading the page signs the
user out; sign in again. Treat this as a demo client, not a credential manager.

## Build and run

The client is a static build served by nginx in the application Compose project.
Build the image on the host, then let Compose publish it on loopback:

```sh
docker buildx build -t mailflow-web:local web/
cd deploy/app
MAILFLOW_WEB_IMAGE=mailflow-web:local docker compose up -d web
```

Open `http://127.0.0.1:8082/`. nginx serves the static files and proxies `/v1`
and `/healthz` to the API service, so the browser uses a single origin and the
API keeps its loopback-only host binding. Set `MAILFLOW_WEB_PORT` in
`deploy/app/.env` to change the host port.

To work on the client without Compose:

```sh
docker run --rm -v "$PWD/web:/src" -w /src node:22-alpine npm ci
docker run --rm -v "$PWD/web:/src" -w /src node:22-alpine npm run build
docker run --rm -v "$PWD/web:/src" -w /src node:22-alpine npm test
```

The build runs `tsc --noEmit` before Vite, so a field that is not part of the
typed contract fails the build.

## Verification pass

The automated checks cover the API client, pure page state, the TypeScript
build, static serving, and the nginx proxy. Interactive UI behaviour is verified
manually against the local stack:

1. Sign in as `bob@mailflow.test`; confirm the mailbox list loads.
2. Open Inbox, then a message with an attachment; confirm the body and the
   attachment name, and download the file.
3. Search for a phrase in a subject and in a body; confirm results and paging.
4. Compose and send to `alice@mailflow.test`; confirm the returned identifier
   and that a later replay note appears only when the key is reused.
5. Save a draft, open it from Drafts, and send it; confirm the identifier is
   unchanged and the message appears in Sent.
6. Reply to a received message; confirm the reply draft appears in Drafts.
7. Toggle read/starred, move a message, and delete it; confirm each refresh.
8. Sign out and reload; confirm credentials are not restored.

The result of this pass is recorded in the change's archive.
