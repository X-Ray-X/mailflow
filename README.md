# mailflow

Mailflow service architecture challenge designed to showcase Spec-Driven Development experience with OpenCode and OpenSpec.

## What this is

A homelab email platform: a locally hosted mail server for mail between local
accounts, with a Go service in front of it as the only published API. The point
of the project is the service boundary — the mail server is replaceable, and
application deployments never recreate, upgrade, or reset mail infrastructure.

Current state: the mail infrastructure is deployed and verified. The Go service
does not exist yet.

## Layout

```
deploy/mail/        mail infrastructure Compose project (Stalwart, pinned)
docs/               operational and integration documentation
openspec/specs/     current behavior, authoritative
openspec/changes/   work in flight, with tasks and design
```

## Quickstart

Requires Docker with the Compose plugin, plus `curl`, `openssl`, and `python3`.

```sh
cd deploy/mail
cp .env.example .env     # set the account and bootstrap passwords
./scripts/bootstrap.sh   # idempotent first start, TLS, accounts
./scripts/verify.sh      # exercise the specs over IMAP and SMTP
```

A working stack then offers:

| Service | Address |
|---|---|
| IMAPS | `mail.mailflow.test:993` |
| Submission (implicit TLS) | `mail.mailflow.test:465` |
| JMAP / management, loopback only | `https://127.0.0.1:443/jmap/` |

Clients must resolve `mail.mailflow.test` to the host and trust the internal CA
at `deploy/mail/data/tls/ca.pem`. Do not disable certificate verification.

- Day-to-day operation, TLS, backup/restore, upgrade: [`docs/mail-infrastructure.md`](docs/mail-infrastructure.md)
- Observed JMAP surface, capabilities, events: [`docs/integration-surface.md`](docs/integration-surface.md)

## Specifications

Intended behavior lives in OpenSpec, not in this README.

```sh
openspec list
openspec show stalwart-local-environment
openspec validate stalwart-local-environment --strict
```

## Scope

In scope: mail between local accounts, standard IMAP/SMTP clients, the Go
service layer, a later React client.

Out of scope: public internet mail, MX/SPF/DKIM/DMARC, deliverability, spam
tuning, Kubernetes.
