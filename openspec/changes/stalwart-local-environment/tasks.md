# Tasks

## 1. Resolve the pinned release and host prerequisites

- [x] 1.1 Identify the current stable Stalwart release and confirm it publishes a native `linux/arm64` image; record the exact tag (and digest if available) in `docs/mail-infrastructure.md`. Verify: `docker buildx imagetools inspect <image>:<tag>` lists `linux/arm64`.
- [x] 1.2 Read that release's own documentation for every option this change will use (local domain, network listeners, TLS, storage path, account bootstrap) and record each option with a documentation reference. Verify: every option used in configuration appears in the recorded list, and none is copied from another release.
- [x] 1.3 Confirm host readiness on the homelab host: Docker with the Compose plugin, tailnet membership, whether tailnet HTTPS certificates are available, and the chosen data path. Verify: `docker compose version` succeeds, `tailscale status` reports the host, and the certificate path (tailnet certificate or internal CA) is recorded as the chosen option. Observed on 2026-11-14: Compose `2.26.1-4`; `tailscale status` reports the host on the tailnet; `tailscale status --json` reports `"CertDomains": null`, so tailnet HTTPS certificates are unavailable. Chosen path: the internal CA (`CN=Mailflow Internal CA`, SANs `mail.mailflow.test, mailflow.test`). The configured `MAIL_DATA_DIR` exists, owned `2000:2000`.

## 2. Deploy the mail infrastructure

- [x] 2.1 Create the mail infrastructure Compose project as a deployment unit separate from the future application stack, with the pinned image and a bind-mounted data path. Verify: `docker compose config` renders cleanly and the rendered configuration contains no application services.
- [x] 2.2 Add a tracked `.env.example` with variable names only and make the real environment file untracked. Verify: `git status --short` shows no environment file and `git check-ignore -v .env` confirms it is ignored.
- [x] 2.3 Configure the reserved local test domain, the canonical hostname, and TLS material for that hostname. Verify: `openssl s_client -connect <hostname>:993 -servername <hostname>` presents a certificate whose subject or SAN matches the hostname, using verification enabled.
- [x] 2.4 Publish only IMAPS and authenticated submission; bind administrative and JMAP HTTP endpoints to loopback or the tailnet. Verify: the observed published ports match `docs/mail-infrastructure.md` exactly, and no port 25 is published.
- [x] 2.5 Provision the two demonstration accounts through the pinned release's documented administration path. Verify: each account authenticates over IMAP with its own credentials and sees only its own mailbox.

## 3. Verify client access and delivery

- [x] 3.1 Configure a desktop client (Thunderbird or Apple Mail) using only the documented host, ports, and credentials. Verify: the client connects with certificate verification enabled, lists mailboxes, and sends a message without error. Reported by the operator on 2026-11-15: Thunderbird on macOS configured with both `alice@mailflow.test` and `bob@mailflow.test`; a test message from Alice arrived for Bob. No certificate warning was reported.
- [ ] 3.2 Configure a mobile client over the tailnet using the same documented hostname. Verify: the client connects with certificate verification enabled and lists the same mailboxes as the desktop client. Deferred on 2026-11-15: mobile client not configured yet.
- [x] 3.3 Send a message with an HTML body, a plain-text alternative, and an attachment between the two local accounts. Verify: exactly one copy appears in the recipient inbox, and the downloaded attachment is byte-identical (`shasum` on both ends).
- [x] 3.4 Exercise read state, flagged state, a custom mailbox created in one client, and the sent mailbox. Verify: each of those scenarios passes when observed from a second, independent connection to the same account. Coverage across two client applications is tasks 3.1 and 3.2.
- [x] 3.5 Stop and start the stack, then recreate the server container with the same pinned release and configuration. Verify: messages, flags, custom mailboxes, and accounts are all still present after each step.
- [x] 3.6 Attempt a submission to a recipient outside the local domain and an unauthenticated submission. Verify: the external submission is refused with no egress attempt observed in server logs, and the unauthenticated submission is refused.
- [x] 3.7 If any observed behavior differs from the specs, update the affected spec or design text in this change and record the discrepancy. Verify: `openspec validate stalwart-local-environment` passes and no spec scenario contradicts an observed result.

## 4. Document operation and the integration surface

- [x] 4.1 Write `docs/mail-infrastructure.md` covering start, stop, restart, backup, restore, ports, hostname, TLS arrangement, and the account bootstrap steps. Verify: following the document from a stopped state reproduces a working stack, and the backup/restore procedure restores a message deleted before the backup.
- [x] 4.2 Write `docs/integration-surface.md` recording the observed JMAP session endpoint and capabilities, the eventsource endpoint, the candidate event types relevant to later changes, the supported authentication paths, and the pinned version. Verify: every endpoint the file presents as usable was exercised successfully with a real request, and anything not exercised is listed as unverified.
- [x] 4.3 Update `README.md` with a short quickstart that points to the two documents above. Verify: a reader following only the README reaches a running stack with a connected client.

## 5. Integration check

- [ ] 5.1 From a clean state (stack down, no clients connected), follow only the documented commands, then execute every scenario in both specs and record the outcome of each. Verify: every scenario passes, or the failure is recorded with its cause and the affected spec or task is corrected.

## Workflow follow-up

- Archive the change after review, once every task above is complete and the specs reflect observed behavior.
- Confirm the archived main specs for `mail-infrastructure` and `mailbox-access` read correctly after archive.
