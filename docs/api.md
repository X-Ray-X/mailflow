# Mailflow API

The Go service that publishes Mailflow's HTTP contract. It adapts to the mail
server through JMAP and translates the result into Mailflow identifiers and
vocabulary; nothing in a response is vendor-shaped.

A first browser client consumes this contract; see
[`docs/web-client.md`](web-client.md) for its scope and credential handling.

The contract covers reads and writes: sending a plain-text message, saving and
submitting drafts, replying to a message, reading and downloading attachments,
searching mail by text, changing a message's read and starred flags, moving it
between mailboxes, deleting it to trash, and creating, renaming, and deleting
custom mailboxes. Uploading attachments and push are later changes.

## Runtime and build

| Item | Value |
|---|---|
| Go release | 1.27.4 (stable on 2026-11-15) |
| Builder image | `golang:1.27.4-alpine` |
| Runtime image | `alpine:3.23`, non-root uid 10001 |
| Platform | `linux/arm64` (built on the host architecture, cross-compiled) |

Neither the development machine nor the CI runner is assumed to have a Go
toolchain. `app/scripts/go.sh` runs `gofmt`, `go vet` and `go test` inside the
pinned builder image:

```sh
./app/scripts/go.sh
```

Build the image:

```sh
docker buildx build --platform linux/arm64 -t mailflow-api:dev app/
```

## Quality gates

A pull request is expected to pass `ci.yml`, which checks the Compose contracts
and then runs, in pinned toolchain images:

```sh
./app/scripts/go.sh
COPYFILE_DISABLE=1 tar -cf - --exclude=node_modules --exclude=dist -C web . \
  | docker run --rm -i -w /src node:22-alpine \
      sh -c 'tar -xf - -C /src && npm ci && npm run build && npm test'
python3 -m py_compile app/scripts/verify-api.py
COPYFILE_DISABLE=1 tar -cf - --exclude=.git -C . . \
  | docker run --rm -i -e OPENSPEC_TELEMETRY=0 -w /src node:22-alpine \
      sh -c 'tar -xf - -C /src && npx --yes @fission-ai/openspec@2.0.0 validate --all --strict'
```

Source is streamed on stdin rather than bind-mounted: the self-hosted runner
runs in a container, so the host Docker daemon cannot resolve the checkout path.
`COPYFILE_DISABLE=1` stops macOS tar from emitting extended attributes that GNU
tar inside the image turns into `._*` files; the `--exclude` flags keep local
build output out of the stream.

These prove the service compiles and passes unit tests, the client builds and
passes its tests, the verification scripts parse, and the specs are coherent. The
live API and IMAP checks in `verify-api.py` still need a running stack; CI does
not start mail containers.

Operational metrics are available at `GET /metrics` on the same loopback
listener, with route and status labels only; see
[`docs/observability.md`](observability.md). This endpoint is not part of the
versioned `/v1` contract.

## Configuration

Everything comes from the environment and is validated at startup. A missing or
malformed value stops the process before it serves, naming the variable.

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `MAILFLOW_JMAP_URL` | yes | — | Mail server JMAP endpoint, `https` only |
| `MAILFLOW_JMAP_HOSTNAME` | yes | — | Name the mail certificate must cover |
| `MAILFLOW_CA_FILE` | yes | — | PEM bundle used to verify the mail server |
| `MAILFLOW_LISTEN_ADDR` | no | `127.0.0.1:8080` | Listen address |
| `MAILFLOW_REQUEST_TIMEOUT` | no | `10s` | Bound on one request, including the mail call |
| `MAILFLOW_PAGE_SIZE_DEFAULT` | no | `25` | Page size when the caller does not ask |
| `MAILFLOW_PAGE_SIZE_MAX` | no | `100` | Largest page the contract returns |
| `MAILFLOW_BODY_MAX_BYTES` | no | `262144` | Cap on a returned text body |
| `MAILFLOW_DB_HOST` | no | `db` | Application database host; Compose DNS on the private data network |
| `MAILFLOW_DB_PORT` | no | `5432` | Application database port |
| `MAILFLOW_DB_NAME` | no | `mailflow` | Application database name |
| `MAILFLOW_DB_USER` | no | `mailflow` | Application database user |
| `MAILFLOW_DB_PASSWORD` | yes | — | Application database password; never logged |
| `MAILFLOW_IDEMPOTENCY_TTL` | no | `24h` | How long a completed send key replays its result |
| `MAILFLOW_IDEMPOTENCY_LEASE` | no | `2m` | How long an unfinished send blocks its key; must exceed the request timeout |
| `MAILFLOW_SEND_MAX_RECIPIENTS` | no | `50` | Largest number of `to` plus `cc` recipients |
| `MAILFLOW_SEND_MAX_BYTES` | no | `262144` | Cap on a submitted text body |

`MAILFLOW_JMAP_URL` names where to connect; `MAILFLOW_JMAP_HOSTNAME` is the TLS
server name. They differ when the service runs in a container: the URL names the
mail container, while the certificate is issued for the mail hostname.

The database holds application state only (idempotency records today); it never
holds mailbox state. The service refuses to start when it cannot reach it, so a
send is never accepted without a record. See
[`docs/application-database.md`](application-database.md).

Inside a container the process must bind `0.0.0.0:8080`, because Docker publishes
to the container's address rather than to its loopback. The host-side binding is
what stays on loopback.

## Running it against the local stack

The mail stack must be running first. The application is a Compose project of
its own (`deploy/app`) with the service and its PostgreSQL database. The service
joins the mail project's network so it can reach the mail container; the
database joins only the application's private data network.

```sh
docker buildx build --platform linux/arm64 -t mailflow-api:dev app/

# A copy of the mail CA in an app-owned volume. Never mount the mail data
# directory: the deployment units share traffic, not files.
docker volume create mailflow-app-ca >/dev/null
docker cp mailflow-stalwart:/etc/stalwart/tls/ca.pem - \
  | docker run --rm --user 0 -i -v mailflow-app-ca:/certs \
      --entrypoint sh mailflow-api:dev \
      -c 'tar -xf - -C /certs && chmod 644 /certs/ca.pem'

cd deploy/app
cp .env.example .env      # set MAILFLOW_DB_PASSWORD for local use
docker compose up -d
```

The database has no published port and its volume (`mailflow-app-db`) is
application-owned. An application deploy or restart preserves it; only an
explicit `docker compose down --volumes` deletes it. If the local mail stack
already holds host port 8080 (its setup listener does when `MAIL_SETUP_PORT=8080`),
publish the API on another loopback port instead.

On a deployed host the workflow generates `deploy/app/.env` from the example on
every run. Set the published port as `MAILFLOW_API_PORT` in the repository's
`production` environment (default 8080), and the database password as the
`MAILFLOW_DB_PASSWORD` secret; a value edited in the checkout does not survive,
because the checkout is cleaned. If another container already publishes the
port, the workflow fails before creating the application container and names
the variable to change. The application's own container from the previous deploy
does not count: that run replaces it.

Stop the stack with `cd deploy/app && docker compose down`. That touches nothing
in the mail project and keeps the database volume.

## Authentication

Every `/v1` request needs HTTP Basic credentials for a mail account. The service
forwards them to the mail server on each request and keeps no user store,
sessions, or tokens. The account is resolved from the authenticated session, so
a request cannot name another account.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" http://127.0.0.1:8080/v1/mailboxes
```

`GET /healthz` needs no credentials.

`app/scripts/verify-api.py` exercises the whole contract against a running
instance, using the demo accounts from `deploy/mail/.env`:

```sh
./app/scripts/verify-api.py --base http://127.0.0.1:8080
```

It checks paging, body content, search, cross-account refusal, malformed input,
sending and replay, draft creation and submission, replies and threading,
attachment metadata and download, flag updates, mailbox creation, rename and
deletion, message moves and soft deletion, and scans every response it receives
— reads and writes — for vendor vocabulary.

## Endpoints

All responses are `application/json; charset=utf-8` and carry
`Cache-Control: no-store`. Timestamps are RFC 3339 in UTC.

### `GET /v1/mailboxes`

Every mailbox of the caller's account.

```json
{
  "mailboxes": [
    { "id": "mb1_ZA.YQ", "name": "Inbox", "role": "inbox", "unread": 4, "total": 4 },
    { "id": "mb1_ZA.aA", "name": "Projects", "role": "custom", "unread": 0, "total": 7 }
  ]
}
```

`role` is one of `inbox`, `sent`, `drafts`, `trash`, `junk`, or `custom`.
Standard mailboxes are identified by role, never by display name.

### `POST /v1/mailboxes`

Creates a top-level custom mailbox.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{"name": "Projects"}' \
  http://127.0.0.1:8080/v1/mailboxes
```

A first create answers `201`:

```json
{ "id": "mb1_ZA.bmV3" }
```

The name is trimmed, must be 1–255 bytes, and must not contain `/` or a control
character. It is unique among the caller's mailboxes ignoring case, because the
list is flat; `Projects` and `projects` cannot both exist. An invalid name is
`invalid_request`. A name already in use is `already_exists` and creates
nothing, so a retried create after an unknown outcome cannot make a second
mailbox: list the account to find the first one.

The mailbox is subscribed, so a standard IMAP client sees it without an extra
subscription step. Names with `/` are refused even though the mail server might
accept them, because `/` is the IMAP hierarchy delimiter and a flat contract
cannot represent the result unambiguously. Hierarchy is not exposed: a nested
mailbox created by another client is listed flat.

### `PATCH /v1/mailboxes/{mailboxID}`

Renames a custom mailbox. The request states the resulting name, so repeating it
is a no-op.

```sh
curl -sS -X PATCH -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{"name": "Work"}' \
  http://127.0.0.1:8080/v1/mailboxes/mb1_ZA.bmV3
```

Success is `204` with no body. The name rules and case-insensitive uniqueness
are the same as create; a collision is `already_exists`. A standard mailbox
(`inbox`, `sent`, `drafts`, `trash`, `junk`) is refused with `invalid_request`
even though the mail server would permit the rename: clients find those
mailboxes by role, and renaming one would break that. An unknown or foreign
identifier is `not_found`.

### `DELETE /v1/mailboxes/{mailboxID}`

Deletes an empty custom mailbox. Success is `204` with no body.

Only a mailbox that holds no messages and no child mailboxes can be deleted. A
mailbox that still holds either answers `mailbox_not_empty`; move or delete its
contents first. Nothing is cascaded and no message is removed as a side effect.
A standard mailbox is `invalid_request`, an unknown or foreign identifier is
`not_found`, and repeating a delete answers `not_found` while changing nothing
else.

### `GET /v1/mailboxes/{mailboxID}/messages`

Messages in one mailbox, newest first.

| Query | Meaning |
|---|---|
| `limit` | Page size. Above the maximum it is clamped; below 1 it is rejected |
| `cursor` | `nextCursor` from the previous page |

```json
{
  "messages": [
    {
      "id": "msg1_ZA.Ym1hYWFhYWw",
      "subject": "Paging probe",
      "from": { "name": "", "email": "alice@mailflow.test" },
      "receivedAt": "2026-11-15T22:18:11Z",
      "read": false,
      "starred": false,
      "hasAttachment": false
    }
  ],
  "nextCursor": "cur1_ZA.Ym1hYWFhYWw",
  "hasMore": true
}
```

`hasMore` is always present. `nextCursor` appears whenever `hasMore` is true;
a request that omits `cursor` starts at the newest message. Following a cursor
never repeats a message already returned, because the cursor anchors the next
page after the last message the page ended on. If that message is removed before
the next request, the cursor answers `invalid_cursor`; restart from the first
page rather than skipping mail.

### `GET /v1/search`

Finds messages by text. The query matches subject and body text; the observed
mail server does no relevance ranking, so results are newest first. Reading a
result uses `GET /v1/messages/{messageID}`.

| Query | Meaning |
|---|---|
| `q` | Required, non-empty search text |
| `mailboxId` | Optional; restrict results to one of the caller's mailboxes |
| `limit` | Page size, with the same clamp as a message listing |
| `cursor` | `nextCursor` from the previous page |

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  'http://127.0.0.1:8080/v1/search?q=Mailflow&limit=25'
```

The response is a message listing page: `messages`, always-present `hasMore`,
and `nextCursor` when another page exists. A query with no matches returns an
empty `messages` array. An empty query, an unknown parameter, or a malformed
limit is `invalid_request`. An unknown or foreign `mailboxId` is `not_found`,
and a stale cursor is `invalid_cursor`; restart the search in that case.

### `GET /v1/messages/{messageID}`

One message with its text body.

```json
{
  "id": "msg1_ZA.Ym1hYWFhYWw",
  "subject": "Paging probe",
  "from": { "name": "", "email": "alice@mailflow.test" },
  "to": [{ "name": "", "email": "bob@mailflow.test" }],
  "cc": [],
  "receivedAt": "2026-11-15T22:18:11Z",
  "read": false,
  "starred": false,
  "hasAttachment": false,
  "text": "Body number 2 for the paging probe.\n",
  "textTruncated": false
}
```

`to` and `cc` are always arrays. `text` is the message's `text/plain` body and is
empty when the message has no such part. A body longer than
`MAILFLOW_BODY_MAX_BYTES` is cut and `textTruncated` is `true`; the cut never
splits a UTF-8 character.

### `GET /v1/messages/{messageID}/attachments`

Lists the files attached to a message the caller can read.

```json
{
  "attachments": [
    { "position": 0, "filename": "payload.bin", "contentType": "application/octet-stream", "size": 2048 }
  ]
}
```

`attachments` is always an array and is empty when the message has no files.
`position` is a zero-based index into this list and is the only way to address
an attachment: mail server blob identifiers and download addresses are never
published. The order is stable for the life of the message. Inline parts and
parts without a filename are not listed. An unknown or foreign message is
`not_found`.

Attachments are read-only in this version: there is no upload or edit endpoint,
and sending still accepts a plain-text body only.

### `GET /v1/messages/{messageID}/attachments/{position}`

Downloads one attachment by its position from the list above.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -o payload.bin \
  http://127.0.0.1:8080/v1/messages/msg1_ZA.Ym1hYWFhYXA/attachments/0
```

Success is `200` with the raw bytes, the attachment's content type, a
`Content-Disposition` naming its file, and `Cache-Control: no-store`. A declared
`Content-Length` lets the client detect a download that ended early; treat an
incomplete body as a failure and retry. A download is bounded at 32 MiB, below
the mail server's own attachment limit; a larger attachment answers
`service_unavailable`. A non-integer or negative position is `invalid_request`,
and a position the message does not have is `not_found`. `HEAD` returns the
headers without the bytes.

### `POST /v1/messages`

Sends a plain-text message as the authenticated account. The sender is the
account's own identity; the request cannot name one.

The `Idempotency-Key` header is required: 1–200 visible ASCII characters. The
key is scoped to the authenticated account, so two accounts may use the same
value.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 5f0f8b9a-2f0e-4a35-9d6a-3f2f4b8c1d20' \
  -d '{
    "to": [{"name": "Bob", "email": "bob@mailflow.test"}],
    "cc": [],
    "subject": "Lunch",
    "text": "Noon?\n"
  }' \
  http://127.0.0.1:8080/v1/messages
```

`to` must name at least one recipient; `cc` may be omitted or empty. The request
may name at most `MAILFLOW_SEND_MAX_RECIPIENTS` recipients in total, and the
text body at most `MAILFLOW_SEND_MAX_BYTES` bytes. Every recipient must be in
the authenticated account's mail domain: the service is not a relay, and an
outside address is `invalid_request` rather than a silent send. An unknown
request field is also `invalid_request`; there is no vendor-shaped field to
accept.

A first send answers `201`:

```json
{ "id": "msg1_ZA.YjJhYWFhYXA" }
```

The identifier names the caller's copy in their `sent` mailbox. The recipient's
copy is a separate message with its own identifier.

Sending the same request with the same key again answers `201` with the same
body and `Idempotency-Replayed: true`; it does not send a second message. A
different request under the same key answers `409 conflict`. A duplicate that
arrives while the first attempt is still running answers `409 in_progress`.
When the mail server refuses the message itself, the answer is
`422 send_rejected` and the key is released so the caller may correct the input
and retry. When the outcome is unknown (a timeout or an unreachable mail
server), the claim is kept for the lease window so a retry cannot deliver a
duplicate; the caller should treat the first attempt as unknown.

### `POST /v1/drafts`

Saves a plain-text draft in the caller's drafts mailbox.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{
    "to": [{"name": "Bob", "email": "bob@mailflow.test"}],
    "subject": "Lunch",
    "text": "Noon?"
  }' \
  http://127.0.0.1:8080/v1/drafts
```

Success is `201` with the draft's Mailflow identifier:

```json
{ "id": "msg1_ZA.ZDFkYWZm" }
```

Every field is optional: `{}` saves an empty draft that a client can complete
later. `cc` may be omitted. The same recipient-domain rule, recipient count, and
body size as a send apply, so a draft saved here can be submitted unchanged; an
outside address or oversized input is `invalid_request`. An unknown field is
also `invalid_request`.

Draft creation takes **no** `Idempotency-Key`. It changes mailbox state rather
than delivering mail, so a duplicate is visible and recoverable: delete the
extra draft. A retry after a lost response can leave two drafts.

Draft content is immutable. There is no edit endpoint, because the mail server
refuses content updates. To change a draft, save a new one with `POST
/v1/drafts`, then delete the old one with `DELETE /v1/messages/{messageID}`.
The old draft moves to trash rather than being erased, so a client that stops
halfway leaves two visible drafts and loses nothing. Do not store the old
identifier and expect it to follow the new content: each save creates a new
message with a new identifier.

### `POST /v1/drafts/{draftID}/send`

Submits an existing draft. The message leaves drafts, appears in sent, and keeps
the identifier the draft had. A draft authored by another client is sendable
too: the service reads its To, Cc, and Bcc and checks every address before
submitting. Bcc is never returned in a message body.

The `Idempotency-Key` header is required, with the same rules as a send. A first
submission answers `201` with the draft's identifier; a replay answers `201`
with the same body and `Idempotency-Replayed: true`; the same key with another
draft is `409 conflict`, and a concurrent duplicate is `409 in_progress`.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Idempotency-Key: 1c8e1d3e-5a4b-4a62-9f0b-7e2d6a1c4f88' \
  -X POST \
  http://127.0.0.1:8080/v1/drafts/msg1_ZA.ZDFkYWZm/send
```

A message that is not a draft, or a draft with no recipients, answers
`invalid_request` and stays where it is. If the mail server declines the
submission the answer is `send_rejected` and the draft stays in drafts. An
unknown or foreign identifier is `not_found`. All these refusals release the
key, so the caller may correct the draft and retry with the same key. A timeout
or an unreachable mail server keeps the claim for the lease window instead,
because the outcome is unknown.

### `POST /v1/messages/{messageID}/reply`

Creates a draft reply to a message the caller can read.

```sh
curl -sS -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{"text": "Sounds good."}' \
  http://127.0.0.1:8080/v1/messages/msg1_ZA.YjJhYWFhYXA/reply
```

Success is `201` with the new draft's Mailflow identifier:

```json
{ "id": "msg1_ZA.ZTExZmZm" }
```

The recipient is the original sender, the subject gains one `Re: ` prefix
(unless it already has one), and the original message's threading is carried so
a standard client groups the reply with the conversation. The body is the
caller's text exactly: the service does not quote the original, add a signature,
or reply to everyone. Recipient and body bounds are the same as a send. Like
draft creation, a reply takes no idempotency key. An unknown or foreign original
is `not_found`, and nothing is created.

### `PATCH /v1/messages/{messageID}`

Sets a message's flags and optionally moves it. The request states the desired
value; at least one of `read`, `starred`, or `mailboxId` is required, and
repeating the same request changes nothing.

```sh
curl -sS -X PATCH -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{"read": true, "starred": true}' \
  http://127.0.0.1:8080/v1/messages/msg1_ZA.YjJhYWFhYXA
```

`mailboxId` moves the message:

```sh
curl -sS -X PATCH -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  -H 'Content-Type: application/json' \
  -d '{"mailboxId": "mb1_ZA.bmV3"}' \
  http://127.0.0.1:8080/v1/messages/msg1_ZA.YjJhYWFhYXA
```

After a move the message is in exactly the named mailbox. Its flags and
attachments are unchanged, and a patch may move and change flags in one request.
Moving a message to the mailbox it is already only in is a successful no-op. The
target must be one of the caller's mailboxes; an unknown or foreign identifier
is `not_found` and the message stays where it was.

The answer is the message summary in the same shape as a list entry:

```json
{
  "id": "msg1_ZA.YjJhYWFhYXA",
  "subject": "Lunch",
  "from": { "name": "", "email": "alice@mailflow.test" },
  "receivedAt": "2026-11-17T02:11:58Z",
  "read": true,
  "starred": true,
  "hasAttachment": false
}
```

Only `read`, `starred`, and `mailboxId` exist. An omitted field is left
unchanged; an unknown field, an empty body, an empty `mailboxId`, or a value of
the wrong type is `invalid_request`. A message identifier that belongs to
another account, or no longer exists, is `not_found`. The patch touches only the
named flags, so a draft or an attachment keeps its state.

### `DELETE /v1/messages/{messageID}`

Deletes a message by moving it to the caller's `trash` mailbox.

```sh
curl -sS -X DELETE -u "$MAILFLOW_USER:$MAILFLOW_PASSWORD" \
  http://127.0.0.1:8080/v1/messages/msg1_ZA.YjJhYWFhYXA
```

Success is `204` with no body. The message is not erased: it leaves the mailbox
it was in and appears in `trash`, so a client can restore it by patching its
`mailboxId` back to another mailbox. Deleting a message whose only mailbox is
already `trash` changes nothing and succeeds, so a repeat is safe. An unknown or
foreign identifier is `not_found`. Permanent deletion and emptying trash are not
part of this version.

### `GET /healthz`

```json
{ "status": "ok" }
```

Unauthenticated and independent of the mail server: a mail outage must not make
the process look unhealthy and trigger a restart.

## Identifiers

| Prefix | Object |
|---|---|
| `mb1_` | Mailbox |
| `msg1_` | Message |
| `cur1_` | Pagination cursor |

They are opaque to clients: do not parse, compare, or construct them. Each one
is scoped to the account it was issued to. The mail server numbers objects per
account, so two accounts routinely hold the same internal identifier for
different objects; an identifier presented by the wrong account is refused with
`not_found` (or `invalid_cursor` for a cursor) rather than resolving to whatever
object happens to share that number. Identifiers are not secrets and are not a
security boundary — the mail server still decides what the caller may read, so a
forged identifier yields `not_found`, not access.

## Errors

Every failure shares one shape:

```json
{ "code": "not_found", "message": "not found" }
```

| Code | Status | Meaning |
|---|---|---|
| `unauthorized` | 401 | Missing or rejected credentials |
| `not_found` | 404 | No such mailbox or message for this caller |
| `invalid_cursor` | 400 | The cursor no longer resolves; restart from the first page |
| `invalid_request` | 400 | A parameter or body is malformed, or the request is not valid for this account |
| `method_not_allowed` | 405 | The path does not serve that method; `Allow` names the ones it does |
| `conflict` | 409 | The idempotency key was already used for a different request |
| `in_progress` | 409 | A send with this idempotency key is still being processed |
| `already_exists` | 409 | A mailbox with that name already exists for this caller |
| `mailbox_not_empty` | 409 | The mailbox still holds messages or child mailboxes |
| `send_rejected` | 422 | The mail server declined the message itself; retrying the same input cannot succeed |
| `service_unavailable` | 503 | The mail server or the application database is unreachable or timed out |
| `internal` | 500 | An unexpected failure |

Messages are fixed strings for humans. They never carry vendor error text,
stack traces, or credentials.

## Idempotency retention

A completed send or draft-submission key replays its result for
`MAILFLOW_IDEMPOTENCY_TTL` (default 24 hours). After that the record expires and
a retry with the same key is a new submission, so a client that retries days
later must not assume the original result.
An unfinished claim blocks its key for `MAILFLOW_IDEMPOTENCY_LEASE` (default 2
minutes); after the lease a retry may take over, which bounds a duplicate but
cannot prove the first attempt did not happen. Records live in the application
database, not in the mail server. Completing one also writes a durable send
event to an outbox that a Go worker publishes to RabbitMQ and records in an
event table. This is asynchronous application bookkeeping: it cannot change a
mailbox, delay a send, or alter the HTTP response; see
[`docs/application-database.md`](application-database.md).

## Versioning

The contract lives under `/v1`. Within a version, changes are additive: fields
and endpoints may be added, existing fields keep their meaning, and nothing is
removed or repurposed. A breaking change needs a new version path.
