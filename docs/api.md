# Mailflow API

The Go service that publishes Mailflow's HTTP contract. It adapts to the mail
server through JMAP and translates the result into Mailflow identifiers and
vocabulary; nothing in a response is vendor-shaped.

The contract is read-only. Sending, flags, mailbox changes, attachments, search,
and push are later changes.

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

`MAILFLOW_JMAP_URL` names where to connect; `MAILFLOW_JMAP_HOSTNAME` is the TLS
server name. They differ when the service runs in a container: the URL names the
mail container, while the certificate is issued for the mail hostname.

Inside a container the process must bind `0.0.0.0:8080`, because Docker publishes
to the container's address rather than to its loopback. The host-side binding is
what stays on loopback.

## Running it against the local stack

The mail stack must be running first. The service joins the mail project's
network so it can reach the mail container:

```sh
docker buildx build --platform linux/arm64 -t mailflow-api:dev app/
mkdir -p deploy/app/certs
docker cp mailflow-stalwart:/etc/stalwart/tls/ca.pem deploy/app/certs/ca.pem

docker run -d --name mailflow-api \
  --network mailflow-mail_default \
  -p 127.0.0.1:8080:8080 \
  -e MAILFLOW_JMAP_URL=https://mailflow-stalwart/jmap/ \
  -e MAILFLOW_JMAP_HOSTNAME=mail.mailflow.test \
  -e MAILFLOW_CA_FILE=/etc/mailflow/ca.pem \
  -e MAILFLOW_LISTEN_ADDR=0.0.0.0:8080 \
  -v "$PWD/deploy/app/certs/ca.pem:/etc/mailflow/ca.pem:ro" \
  mailflow-api:dev
```

The CA copy is a copy, not a mount of the mail data directory: the two
deployment units share traffic, not files. The deployed project puts that copy
in an app-owned volume (`mailflow-app-ca`) instead of a host path, because the
runner is a container and the host Docker daemon cannot see a file it writes;
the recipe above mounts a local copy directly. If the local mail stack already
holds host port 8080 (its setup listener does when `MAIL_SETUP_PORT=8080`),
publish the API on another loopback port instead.

On a deployed host the workflow generates `deploy/app/.env` from the example on
every run. Set the published port as `MAILFLOW_API_PORT` in the repository's
`production` environment (default 8080); a value edited in the checkout does not
survive, because the checkout is cleaned. If another container already publishes
the port, the workflow fails before creating the application container and names
the variable to change.

Stop it with `docker rm -f mailflow-api`. That touches nothing in the mail
project.

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

It checks paging, body content, cross-account refusal, malformed input, and
scans every response it receives for vendor vocabulary.

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
| `invalid_request` | 400 | A parameter is malformed |
| `method_not_allowed` | 405 | Only `GET` and `HEAD` are served |
| `service_unavailable` | 503 | The mail server is unreachable or timed out |
| `internal` | 500 | An unexpected failure |

Messages are fixed strings for humans. They never carry vendor error text,
stack traces, or credentials.

## Versioning

The contract lives under `/v1`. Within a version, changes are additive: fields
and endpoints may be added, existing fields keep their meaning, and nothing is
removed or repurposed. A breaking change needs a new version path.
