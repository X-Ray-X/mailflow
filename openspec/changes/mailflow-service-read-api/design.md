# Design

## Context

See `proposal.md` for motivation. The facts that shape this design come from
`docs/integration-surface.md` and from a probe of the pinned release on 2026-11-15,
which closed three gaps that document recorded as unverified:

- `Email/query` supports `sort`, `limit`, `calculateTotal`, and cursor paging via
  `anchor` + `anchorOffset`. With four messages and a page size of two, page two
  returned the next two ids with no repeat, and page three returned an empty list.
- `Email/get` returns `bodyValues` only when `bodyValues` is requested in
  `properties`; `fetchTextBodyValues` alone is not enough.
- An unknown mailbox id does not fail `Email/query`; it returns an empty `ids` list.
  An unknown message id appears in `notFound`, not as an error.

Observed mailbox roles are `inbox`, `sent`, `drafts`, `trash`, `junk`, and `none`
for a custom mailbox. There is no `archive` role in the default set.

The development machine has no Go toolchain, so the design must not assume one.

## Goals / Non-Goals

**Goals:**

- A read contract that a consumer can use without knowing Stalwart exists.
- One place where vendor vocabulary is translated, so it can be tested directly.
- A deployment unit that cannot affect mail.

**Non-Goals:**

- Caching or storing mail data. The service is a translator, not a second store.
- Session management, tokens, or a user database.
- Write operations, attachments, search, and push.
- Exposing the API beyond the host in this change; there is no consumer yet.

## Decisions

### 1. Adapt through JMAP over loopback HTTPS

The service speaks JMAP to `https://127.0.0.1:443` with `mail.mailflow.test` as the
TLS server name, using the internal CA.

- **Alternative: IMAP.** Rejected. It would force IMAP vocabulary into the mapping
  layer, lose the mailbox-role model, and make flag semantics the service's problem.
- **Alternative: read Stalwart's storage.** Forbidden by the architecture invariants.

### 2. Forward the caller's credentials, keep no user store

The service accepts HTTP Basic credentials and forwards them to JMAP on each request.
The mail server stays the only authority for authentication, and the service stores
nothing about identity.

- **Alternative: service-issued session tokens.** Rejected for this change: it needs
  a durable store and makes the service a second authentication authority.
- **Alternative: OAuth.** The integration surface records it as unverified on this
  release; not a basis for a contract.

Consequence: each request costs one JMAP session read plus one method call. Accepted
for now; connection reuse keeps it to one TLS handshake per connection.

### 3. Opaque, prefixed, reversible identifiers

Identifiers are `mb1_<base64url>` for mailboxes and `msg1_<base64url>` for messages,
encoding the vendor identifier. Clients treat them as opaque; the `1` is the encoding
version so a future mapping table can coexist with existing identifiers.

- **Alternative: raw vendor identifiers.** Rejected: leaks the vendor and breaks the
  replaceability test.
- **Alternative: ULIDs plus a mapping table.** Rejected for this change: it needs
  PostgreSQL and a reverse lookup on every request, for no user-visible benefit.

The encoding is not a security boundary. The tests assert only that identifiers differ
from vendor identifiers and are stable across requests.

### 4. Resolve the account from the session, never from the address

`primaryAccounts["urn:ietf:params:jmap:mail"]` is read from the session document on
each request. Deriving an account id from an address is impossible — ids are opaque
and server-assigned — and caching one would cache an authentication result.

### 5. Cursor pagination by anchor

Listing uses `sort: receivedAt desc`, `limit`, and a cursor built from the last
returned message: `anchor: <vendor id>`, `anchorOffset: 1`. `calculateTotal` supplies
the "more messages exist" signal.

- **Alternative: `position` offsets.** Rejected: a message arriving between pages
  shifts every later offset and repeats an item, which the spec forbids.
- A deleted anchor returns a Mailflow `invalid_cursor` error so the client can restart
  from the first page rather than silently skipping mail.

### 6. Translate vocabulary in one package

| Mail server | Mailflow |
|---|---|
| keyword `$seen` | `read: true` |
| keyword `$flagged` | `starred: true` |
| `hasAttachment` | `hasAttachment` |
| role `none` | `role: "custom"` |
| role `inbox`/`sent`/`drafts`/`trash`/`junk` | same names |

`Email/get` is always called with an explicit property list, because the default
returns every property including `blobId`, `threadId`, and `messageId`.

### 7. Standard library only

`net/http` with method-and-path patterns, `encoding/json`, `log/slog`, `context` with
a per-request timeout. No router, no framework, no ORM.

- **Alternative: a router such as chi.** Rejected: three routes do not justify a
  dependency, and `AGENTS.md` asks for a dependency smaller than the code it replaces.

### 8. Configuration from the environment, validated at startup

Required: `MAILFLOW_JMAP_URL`, `MAILFLOW_JMAP_HOSTNAME`, `MAILFLOW_CA_FILE`.
Optional with defaults: `MAILFLOW_LISTEN_ADDR` (`127.0.0.1:8080`),
`MAILFLOW_REQUEST_TIMEOUT` (`10s`), `MAILFLOW_PAGE_SIZE_DEFAULT` (`25`),
`MAILFLOW_PAGE_SIZE_MAX` (`100`), `MAILFLOW_BODY_MAX_BYTES` (`262144`).
Startup fails with a message naming the offending value.

A text body larger than the cap is truncated and the response says so, rather than
returning an unbounded payload.

### 9. Deploy as its own Compose project, loopback only

`deploy/app/compose.yaml`, project name `mailflow-app`, one service, publishing
`127.0.0.1:8080`. The pipeline deploys it through its own workflow, never referencing
the mail project.

- **Alternative: publish on the tailnet now.** Deferred. There is no consumer, and
  `AGENTS.md` says not to widen exposure before something needs it.
- Health is checked with `docker exec` inside the container, because the runner is
  itself a container and the host's loopback is not the runner's loopback — the same
  constraint that shaped `bootstrap.sh`.

The service trusts the internal CA through a copy at `deploy/app/certs/ca.pem`, made
during deployment with `docker cp` from the mail container. The alternative — mounting
the mail data directory into the application — would blur the boundary between the two
deployment units.

### 10. Build and test in the pinned Go image

Neither the development machine nor the runner is guaranteed a Go toolchain, so
`go build` and `go test` run in a pinned `golang` image, and the runtime image is
multi-stage on the same version. The exact version is chosen at implementation time
from the current stable release and recorded in the Dockerfile and `docs/`.

- **Alternative: install Go on the host.** Works, but makes the build depend on
  unversioned host state and leaves the runner unverified.

## Risks / Trade-offs

- **Credential pass-through doubles upstream calls** → one session read plus one
  method call per request; keep-alive reuses the TLS connection. Measured in the
  deployment smoke test rather than assumed.
- **A stale CA copy breaks TLS after a CA rotation** → the deploy copies `ca.pem` on
  every run, so the copy tracks the mail container.
- **Cursor invalidated by deletion** → explicit `invalid_cursor` error instead of a
  silent page shift.
- **Body truncation surprises a consumer** → the response states truncation; the cap
  is configuration, not a constant buried in code.
- **No rate limiting** → the mail server advertises four concurrent requests; the
  service's timeout bounds a request, and the deployment smoke test records the
  observed behavior. A limit is a later change if a consumer makes it necessary.
- **Loopback-only API is not yet reachable by a client** → deliberate; widening the
  binding belongs to the change that adds the consumer.

## Migration Plan

1. Deploy the mail infrastructure first; it is already deployed.
2. Deploy the application project. It reads mail; it writes nothing.
3. Rollback is stopping or removing the application project. Mail containers, volumes,
   and configuration are untouched, which is the scenario this change makes testable.

## Open Questions

- Whether the React client needs a different cursor shape. Deferrable: the cursor is
  opaque to clients, so the encoding can change with a contract version.
- Whether session reads need caching for latency. Deferrable: measure first; caching
  an authentication result is a decision that deserves its own change.
