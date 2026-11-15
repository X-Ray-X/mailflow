# Proposal

## Why

Mailflow is a service layer over a real mail server, but no part of that server exists yet. Every later capability — the Go API, the web client, event ingestion, deployment — depends on an assumption that a standard mail client can authenticate against a locally hosted mailbox and exchange mail with another local account. That assumption is currently unverified.

This change establishes the smallest real mail environment that proves it: a persistent, reproducible Stalwart instance with a local domain, two accounts, and real clients connected over the local network or Tailscale. It also records the integration surface that the Go service will consume, so later changes build against observed behavior rather than documentation guesses.

## What Changes

- Add a mail infrastructure stack, separate from the future application stack, running a pinned Stalwart release with persistent storage.
- Configure one local, non-routable mail domain and provision two accounts (sender and recipient) for demonstration.
- Expose IMAP and authenticated SMTP submission on documented ports, reachable from the home network and Tailscale clients.
- Establish a TLS arrangement that real clients (Thunderbird, Apple Mail, phone clients) accept without disabling verification.
- Verify internal user-to-user delivery: a message sent by one account is delivered to the other account's mailbox locally, with no external DNS or internet delivery involved.
- Verify folder and flag behavior from a real client, including where sent messages are recorded.
- Record the observed integration facts for later changes: reachable JMAP endpoint and capability set, available event types, authentication paths, ports, and configuration decisions required to reproduce the deployment.
- Explicitly out of scope: public internet mail, MX records, SPF/DKIM/DMARC, deliverability, the Go service, the React client, and CI/CD.

## Capabilities

### New Capabilities

- `mail-infrastructure`: reproducible, persistent deployment of the mail server, its configuration, local domain, accounts, TLS, and network exposure.
- `mailbox-access`: standard client access to mailboxes over IMAP and authenticated SMTP, including internal delivery between local accounts and observable folder and flag behavior.

### Modified Capabilities

None. No specifications exist yet.

## Impact

- New deployment assets: a mail-infrastructure Compose project with pinned image version, configuration scripts, and a host bind-mounted data path. It is deliberately independent of the future application Compose project so that an application deployment can never recreate or upgrade the mail server.
- New operational documentation: how to start, restart, back up, and connect clients; which ports are published and why.
- New recorded integration surface for the Go service (`docs/`), consumed by the next change.
- No application code, no PostgreSQL, no queue, no public mail delivery.
- Requires an arm64-compatible Stalwart image, since the target homelab host is a arm64 homelab host.
