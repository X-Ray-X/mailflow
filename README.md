# mailflow

Mailflow service architecture challenge designed to showcase Spec-Driven Development experience with OpenCode and OpenSpec.

## Continuous delivery

Both deployment units are deployed from this repository on the homelab host.
A self-hosted GitHub Actions runner on the host runs the pipelines below.

### Workflows

| Workflow | When | What |
|---|---|---|
| `ci.yml` | pull request / manual | structural gate for both Compose projects |
| `deploy.yml` | push to `main` / manual | deploy the mail infrastructure, then verify ports and tailnet TLS |
| `deploy-app.yml` | push to `main` / manual | build the service image on the host, deploy the application project, verify health and that mail was untouched |
| `pr-agent.yml` | pull request / comment | PR-Agent review / describe / improve (advisory, never blocks) |

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

The same environment also holds two variables, used by the mail deploy:

| Variable | Purpose |
|---|---|
| `MAIL_DATA_DIR` | absolute host path for the mail data directory |
| `MAIL_TAILNET_IP` | optional host tailnet address, printed in the run summary |

The application deploy needs no secret: it builds its image on the host and
tags it by commit.

### Verify after a deployment

Run `deploy/mail/scripts/verify.sh` on the host. It is the authority for the
mailbox-access and persistence specs; the pipeline only covers structural
checks plus the tailnet certificate SAN assertion.

After an application deploy, the pipeline asserts the service reports healthy
and that the mail container's identity and start time did not change. The read
contract itself is exercised with the requests in
[`docs/api.md`](docs/api.md).

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
publishes the read contract (mailboxes, message list, message content); sending
and flag changes are the next change.

## Layout

```
app/                Go service: read contract, mail adapter, tests
deploy/mail/        mail infrastructure Compose project (Stalwart, pinned)
deploy/app/         application Compose project (service only)
docs/               operational and integration documentation
openspec/specs/     current behavior, authoritative
openspec/changes/   work in flight, with tasks and design
```

## Quickstart

Requires Docker with the Compose plugin, plus `curl`, `openssl`, and `python3`.

### Mail infrastructure

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

### Application

The service needs the mail stack running first. It joins the mail project's
network because a container cannot reach the host's loopback.

```sh
docker buildx build --platform linux/arm64 -t mailflow-api:dev app/
mkdir -p deploy/app/certs
docker cp mailflow-stalwart:/etc/stalwart/tls/ca.pem deploy/app/certs/ca.pem

cd deploy/app
cp .env.example .env
docker compose up -d
```

| Service | Address |
|---|---|
| Mailflow API, loopback only | `http://127.0.0.1:8080/v1/` |
| Health | `http://127.0.0.1:8080/healthz` |

If the local mail stack already holds host port 8080 (its setup listener does
when `MAIL_SETUP_PORT=8080`), set `MAILFLOW_API_PORT` in `deploy/app/.env` to a
free loopback port. That file is host-owned: the deploy workflow creates it only
when absent and rewrites only the image tag, so the port survives later deploys.
The contract, configuration, and examples are in [`docs/api.md`](docs/api.md).

```sh
./app/scripts/verify-api.py --base http://127.0.0.1:8080
```

exercises the contract against a running instance: paging, body content,
cross-account refusal, malformed input, and a vendor-vocabulary scan.

Clients must resolve `mail.mailflow.test` to the host and trust the internal CA
at `deploy/mail/data/tls/ca.pem`. Do not disable certificate verification.

- Day-to-day operation, TLS, backup/restore, upgrade: [`docs/mail-infrastructure.md`](docs/mail-infrastructure.md)
- Observed JMAP surface, capabilities, events: [`docs/integration-surface.md`](docs/integration-surface.md)
- Published read contract, configuration, run recipe: [`docs/api.md`](docs/api.md)

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
