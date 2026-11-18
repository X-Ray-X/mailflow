# Design

## Context

See `proposal.md` for motivation. The published API already covers reads,
search, sends, drafts, replies, attachments, and message patches. The
application Compose project runs the Go service and its database on a private
network; the mail project is independent. There is no browser client yet.

## Goals / Non-Goals

**Goals:**

- Prove the contract is sufficient for a normal mail session.
- Keep the client small, typed against the contract, and deployable as static
  files in the existing application unit.
- Make credential handling and failure reporting explicit.

**Non-Goals:**

- Mailbox administration UI, push, offline, HTML mail, upload, reply-all,
  forward, or themes.
- Any change to the Go API, the database, or the mail deployment unit.

## Decisions

### Vite, React, and TypeScript with a typed API client

The client builds with Vite and TypeScript, with a small `api.ts` that owns all
`fetch` calls and maps responses to local types. The alternative was plain
JavaScript with ad-hoc requests; it would let a vendor field or a typo in a path
reach the UI without a compile error. A heavier framework or state library was
rejected: one screen with a few panels does not need it.

### Credentials live in component state only

The sign-in form stores the address and password in memory and passes them to
the API client for the session. The alternative was `localStorage` or a refresh
token; both would persist a secret in the browser and widen the blast radius of
a shared machine. The cost is that a reload asks the user to sign in again,
which is documented.

### nginx serves the assets and proxies the API

A new `web` service in `deploy/app/compose.yaml` builds static assets and serves
them with nginx on a loopback host port. nginx proxies `/v1` and `/healthz` to
the `api` service on the application network. A separate non-internal frontend
network carries the published port: Docker does not publish ports for a service
attached only to the internal data network, which was observed during live
verification. The client still has no route to the mail network. The alternative was calling
`http://127.0.0.1:8081` from the browser with CORS headers on the Go service; it
would widen the service's configuration and expose a second origin. The client
therefore uses relative URLs and no CORS.

### The SPA keeps one source of truth

Selected mailbox, message, search query, and drafts live in React state. The
client re-fetches after each write instead of maintaining an optimistic local
mail store. The alternative was a local mirror of messages; it would make the
browser a second store of mailbox truth and drift from the server.

### Idempotency keys are generated per submission

`crypto.randomUUID()` produces a fresh key for each send or draft submission.
A replay response is shown as the original result with a replay note. The
alternative was deriving a key from the form contents; it would make a corrected
retry of a rejected message collide with the original key.

## Risks / Trade-offs

- **No browser automation in the local toolchain.** The change verifies the
  build, unit tests, static serving, and the nginx proxy against the live API.
  Interactive flows are checked with a documented manual pass and reported as
  such rather than claimed as automated.
- **A reload loses the session.** By design; the user signs in again.
- **Basic credentials cross the loopback boundary in the browser.** The client
  uses same-origin HTTPS only if the deployment terminates TLS in front of it;
  on the homelab loopback port the traffic stays local. This is documented and
  no credential is logged.
- **A new container in the application project.** It serves static files only
  and joins the application network, so it cannot touch mail containers or
  volumes.

## Open Questions

None. The user chose the core-flow MVP scope before planning.
