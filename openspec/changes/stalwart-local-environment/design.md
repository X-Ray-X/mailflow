# Design

## Context

See `proposal.md` for motivation and `specs/` for the behavioral contract. This is a greenfield deployment: no mail infrastructure, no application code, and no specifications exist yet in the repository. The target host is an arm64 Radxa Rock 5T running Docker, reachable from the home network and from a Tailscale tailnet. The repository is private and version control is the only source of deployment configuration.

Two constraints shape everything below:

- The mail server is the source of truth for mailboxes. Nothing else may become a second store of mailbox state.
- The mail infrastructure must be deployable and upgradable independently of the application that will later sit in front of it.

## Goals / Non-Goals

**Goals:**

- A single-command, reproducible, persistent mail environment on the target host.
- Real IMAP and SMTP clients connected over TLS with certificate verification enabled.
- A recorded integration surface (JMAP endpoint, capabilities, event types, authentication paths) that the next change can design against.
- A deployment boundary that makes "application deploy never touches mail" mechanically true, not just a convention.

**Non-Goals:**

- Public internet mail, MX records, SPF/DKIM/DMARC, deliverability, reputation.
- Any Go service, PostgreSQL, queue, or worker.
- A web client or any custom UI.
- CI/CD, image publishing, or automated deployment. This change is operated manually and deliberately.
- Account self-service or an account management API of our own. Accounts are provisioned by a repository script through the mail server's documented management API.
- Spam filtering tuning, Sieve rules, or retention policies.

## Decisions

### Mail server: Stalwart, pinned by tag or digest

Stalwart is chosen over the alternatives because it is one component that covers IMAP, SMTP submission, and JMAP, and JMAP is the integration surface the Go service will use later.

| Option | Why not |
|---|---|
| Postfix + Dovecot | Protocol-complete and classic, but no modern API. The Go integration would have to observe Maildir or speak LMTP, which is exactly the hidden coupling this project wants to avoid. |
| mailcow | Mature, but a multi-container stack with its own orchestrator and upgrade path. Too much surface for a demo whose point is the service boundary, not mail administration. |
| Maddy | Simpler, but a smaller documented surface and no JMAP, so the later integration story weakens. |

Consequence: the pinned release is an input to the change, not an incidental detail. The exact tag or digest must be confirmed to exist for arm64 and its documentation consulted for every configuration option used. No option may be carried over from memory or from a different release.

Resolved: `v1.1.0`, pinned by tag in `deploy/mail/.env` and recorded with its multi-arch index and `linux/arm64` manifest digests in `docs/mail-infrastructure.md`. Its documentation was the only source for every option used; the mapping from option to documentation page is in that file.

### Two Compose projects, not one

The mail infrastructure is its own Compose project with its own file and volumes. The application stack will be a separate project later.

Alternative considered: one project for simplicity. Rejected because `docker compose up` on a shared project can recreate services the caller did not intend, which turns "the app deploy should not touch mail" into a review convention rather than a guarantee. The costs of two projects are one extra command to start everything and a documented start order, both acceptable.

### Domain: a reserved, non-routable test domain

Use a domain from the reserved `.test` namespace: `mailflow.test`, with hostname `mail.mailflow.test`. Mail between local accounts is delivered internally and never leaves the host.

| Option | Why not |
|---|---|
| A real domain you own | More realistic, but a misconfiguration causes real egress and real reputation damage. The demo does not need it. |
| `.local` | Collides with mDNS resolution on many clients; produces confusing failures. |
| `.home.arpa` | Legitimate for home networks, but reserves semantics around DNS that this setup does not use, and is more likely to be resolved by a client's DNS stack. |

Consequence: the local domain MUST be configured as a local domain on the server so delivery never consults external DNS, and submissions to any other domain MUST be refused per the spec rather than queued for a remote attempt.

### TLS: canonical tailnet hostname, certificate without client trust surgery

Clients must validate the certificate, so a self-signed certificate with "disable verification" is not acceptable. Preference order:

1. A certificate for the tailnet hostname (for example via `tailscale cert`), which is publicly trusted and needs no phone-side CA installation. Subject to HTTPS certificates being enabled for the tailnet, which must be verified rather than assumed.
2. An internal CA whose root is installed once on each client, documented explicitly.

Either way, the **canonical hostname** is the certificate's hostname. Clients connect to that name, not to a bare LAN IP, because an IP would never match the certificate. Tailscale MagicDNS resolves `*.ts.net` names by default, so a custom hostname needs a hosts entry or a split-DNS record; the certificate path does not change.

This is the decision most likely to waste an evening if reversed, which is why it is made here rather than during implementation.

**Resolved in implementation:** option 2 is in place — `bootstrap.sh` generates an internal CA and a server certificate with `subjectAltName = DNS:mail.mailflow.test, DNS:mailflow.test`, installs it through the `Certificate` object, and reloads it. Verification was done with certificate checking enabled against that CA, with SNI set to the canonical name. On the Radxa the tailnet is reachable, but `tailscale status --json` reports `"CertDomains": null`: HTTPS certificates are not enabled, so option 1 is not available. The internal CA is the chosen path for now; enabling tailnet HTTPS certificates remains the preferred future path.

The bootstrap call sets `requestTlsCertificate: false`. Without it, the server attempts ACME for the `.test` domain and logs repeated `acme.auth.error: invalidContact` failures. A `.test` name can never obtain a public certificate, so asking is not a harmless default.

### Persistence: host bind mount under a dedicated path

Mail data lives in a host directory owned by this project rather than an anonymous volume, so backup, inspection, and restore are ordinary filesystem operations and the data path is explicit in documentation.

Alternative considered: named Docker volumes. Cleaner ergonomics, but backup requires knowing the volume name and the mount namespace. For a system whose data is the whole point, explicit paths are worth the small extra care.

### Ports: only what clients and administration need

Publish IMAPS and authenticated submission. Do not publish port 25: there is no external mail, and not publishing it removes an entire class of failure and abuse.

Administrative and JMAP HTTP endpoints stay bound to loopback on the host, or are exposed only over the tailnet, because the Go service will eventually reach them from the same host.

Resolved port map: `993` (IMAPS) and `465` (submission, implicit TLS) published on all interfaces; `443` (JMAP and management, HTTPS) and `8089` (setup and health, plain HTTP) bound to `127.0.0.1` only; `25`, `995`, and `4190` stay inside the container network. This is asserted by `scripts/verify.sh` against `docker compose ps`, not just documented.

### Accounts: scripted bootstrap through the documented management API, secrets on the host

The two demonstration accounts are created by `deploy/mail/scripts/bootstrap.sh` calling the pinned release's JMAP management API (`x:Account/set` with a `User` object), with the exact steps recorded in `docs/mail-infrastructure.md`. Passwords live in an untracked host-side environment file; a tracked example file lists names only.

This replaces the earlier plan to defer provisioning. Deferral would have left the "single documented command produces a working environment" requirement unmet, and the management API is a documented, supported interface rather than an invented mechanism. The script is idempotent: it checks `x:Account/get` before creating, and a second run changes nothing. Account lifecycle as a product capability — self-service, password reset, deletion — is still out of scope for this change.

The administrator credential returned by `x:Bootstrap/set` is written once to `data/secrets/admin.cred` with mode 600. It is not an environment variable and does not appear in logs; verification reads it from that file for a read-only queue query.

Observed, and needed for future work: `x:Domain/get` returns domain objects with no `id` field. The domain identifier is the response's `accountId`, and accounts reference it as `domainId`. 

### Outbound: refuse relaying explicitly

Configuring the hosted domain is not enough to keep mail local. Stalwart's default `MtaStageRcpt.allowRelaying` expression is `{"else":"!is_empty(authenticated_as)"}`, so any authenticated client could submit a message to any external domain and the server would queue it for outbound delivery. That directly contradicts the "no external egress" requirement, and it was observed before being fixed: an authenticated session got `250 2.1.5 OK` for `RCPT TO:<nobody@example.net>`.

The supported mechanism is the RCPT-stage singleton itself. `bootstrap.sh` sets `allowRelaying` to `{"else":"false"}` and runs the `ReloadSettings` action, because a settings singleton is stored immediately but only applied on reload. After that, an external recipient is refused with `550 5.7.1 Relay not allowed.` while a local recipient is still accepted, and the outbound queue (`x:QueuedMessage/get`) stays empty. Verification asserts the client-visible code, the empty queue, and the `smtp.relay-not-allowed` server event.

Port 25 is still not published, so this is a second, independent barrier rather than the only one.

### Sent copy: the client writes it, the server stores it

Stalwart does not create a sent copy of an SMTP submission, and the pinned release has no setting to enable one. Its system Sieve scripts, which run at the DATA stage, can reject, discard, and edit a message, but they cannot file it into a mailbox. Routing the submission back to the sender's own address would deliver it as received mail, not as a sent record.

The behavior that standard clients actually rely on is the one recorded in the spec: the client appends its own copy to the account's sent mailbox over IMAP, and the server accepts and retains it. Verification sends over SMTP, appends over IMAP the way a client does, and then reads the copy back from a second connection. This is not a workaround for a missing feature; it is how IMAP clients are specified to behave, and it keeps the server out of a decision that belongs to the client.

## Risks / Trade-offs

- **Configuration drift across releases** → pin exactly one release, consult only that release's documentation, and record every option used in `docs/`. Never transplant configuration from a different version.
- **TLS hostname mismatch** → one canonical hostname, documented once, used by every client and by later service configuration.
- **Refusing external recipients may not be a single toggle** → resolved: `MtaStageRcpt.allowRelaying` plus `ReloadSettings`, observed as `550 5.7.1 Relay not allowed.` with an empty outbound queue. If a future release changes the default or the singleton, the verification script fails rather than the requirement silently regressing.
- **arm64 image unavailable for the chosen release** → resolved: `v1.1.0` publishes a native `linux/arm64` manifest, confirmed with `docker buildx imagetools inspect`.
- **Volume mishandling during experiments** → the data path is explicit and documented; back up the directory before any upgrade or configuration experiment. Restore requires `up -d --force-recreate`: `start` reuses the existing bind mount and keeps reading the directory that was moved aside.
- **Multi-client flag consistency depends on IMAP extension support** → verified with independent simultaneous IMAP connections: read and flagged state set on one is visible on another, and survives reconnect. This used one client implementation from two sessions; cross-product flag consistency was not exercised and is not claimed. Thunderbird on macOS covers desktop account setup and sending; a mobile client is documented but was not configured.
- **First start needs internet** → the image downloads the WebUI and spam rules on first boot. Mail protocols work without them, but a fully offline first start leaves those resources missing. Recorded in `docs/mail-infrastructure.md`.

## Migration Plan

Greenfield; nothing to migrate. Deployment is manual for this change, and steps 1–4 are what `scripts/bootstrap.sh` performs:

1. Create the host data directory and untracked environment file.
2. Start the mail project with the pinned release in bootstrap mode.
3. Configure the hostname and domain, then create and install the internal CA certificate.
4. Set `allowRelaying` to false and provision both accounts.
5. Run `scripts/verify.sh`, which connects with real IMAP/SMTP clients and replays the spec scenarios, including a stop/start and a container recreation.

Rollback is `docker compose down` for this project only. The data directory is preserved; deleting it is an explicit, separate, deliberate action and never part of a restart or upgrade.

Verified here: steps 1–5 on macOS arm64 with OrbStack, and the deployment on the Radxa host. The tailnet certificate path is unavailable on the current tailnet (`CertDomains` is null), so the internal CA is in use.

## Open Questions

- **Which exact Stalwart release to pin** → resolved: `v1.1.0`, the current stable release, native `linux/arm64`, digest recorded in `docs/mail-infrastructure.md`.
- **Whether the tailnet certificate path works on the Radxa** → resolved for now: the tailnet is reachable but HTTPS certificates are disabled (`CertDomains` is null), so `tailscale cert` is unavailable. The internal CA is the chosen path; enabling tailnet HTTPS certificates remains the production preference.
- **Whether the same procedure works on the target host** → resolved: the deployment workflow ran on the Radxa and the container is healthy with persistent host mounts. Host reboot persistence remains unverified; it needs a maintenance window on the host.
- **Whether two distinct client products behave as the two-session check does** → narrowed: the requirement now asks for a desktop application plus a second implementation reading the same account. Thunderbird on macOS was configured with both accounts and delivered a test message on 2026-11-15, and the protocol client reads the same mailboxes. Cross-product flag consistency is not claimed, and a mobile client is documented but not configured.
- **Whether local delivery depends on internet access** → resolved: with the container's default route removed, so no egress was possible, `verify.py` passed every check including delivery and both refusal paths. A first start still needs internet to fetch the WebUI and spam rules; a running stack does not.
