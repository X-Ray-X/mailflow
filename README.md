# mailflow

Mailflow service architecture challenge designed to showcase Spec-Driven Development experience with OpenCode and OpenSpec.

## Continuous delivery

The mail infrastructure is deployed from this repository on the homelab host.
The GitHub Actions runner on the host (`/home/radxa/repos/scripts/actions-runner`)
runs the pipelines below; a second runner service
(`github-runner-mailflow`, `RUNNER_REPO: X-Ray-X/mailflow`) serves this
repository without touching the pre-existing runner.

### Workflows

| Workflow | When | What |
|---|---|---|
| `ci.yml` | pull request / manual | structural gate for `deploy/mail/compose.yaml` |
| `deploy.yml` | push to `main` / manual | deploy on the host, then verify ports and tailnet TLS |
| `pr-agent.yml` | pull request / comment | PR-Agent review / describe / improve |

### Runner

- `github-runner-mailflow` — registered for `X-Ray-X/mailflow`, its own data
  directory `runner-data-mailflow`.

### Secrets

The `production` GitHub Environment must contain (values never committed):

| Secret | Purpose |
|---|---|
| `MAIL_BOOTSTRAP_PASSWORD` | bootstrap credential used by the first start only |
| `MAIL_ACCOUNT_ALICE_PASSWORD` | demo account alice |
| `MAIL_ACCOUNT_BOB_PASSWORD` | demo account bob |

Both demo accounts (`alice`/`bob`) exist so mail can be sent between local
accounts and exercised against the mailbox-access specs; they are also the
homelab's public demo accounts.

### Verify after a deployment

Run `deploy/mail/scripts/verify.sh` on the host. It is the authority for the
mailbox-access and persistence specs; the pipeline only covers structural
checks plus the tailnet certificate SAN assertion.

### PR-Agent (no subscription)

PR-Agent works with any OpenAI-compatible key. Small-scale, no-subscription
options:

- OpenRouter free tier — repository secret `OPENAI_KEY`.
- Google AI Studio free quota — repository secret `GEMINI_API_KEY`, with the
  provider env name `GOOGLE_AI_STUDIO.GEMINI_API_KEY` and `config.model` set
  to a small Gemini model.

Set `config.model` to your model of choice (the default is `gpt-6.1-sol`);
`config.fallback_models` holds cheaper fallbacks. Model ids need the LiteLLM
provider prefix: `gemini/gemini-3-flash-preview`, not the bare id, or the
request is treated as Vertex AI and fails without Google Cloud credentials.

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
