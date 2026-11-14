# mailflow
Mailflow service architecture challenge designed to showcase Spec-Driven Development experience with OpenCode and OpenSpec.

## Continuous delivery

The mail infrastructure is deployed from this repository on the homelab host.
The GitHub Actions runner on the host (`/opt/data/repos/scripts/actions-runner`)
runs the pipelines below; a second runner service
(`github-runner-mailflow`, `RUNNER_REPO: X-Ray-X/mailflow`) serves this
repository without touching the `rayofsunshine-site` runner.

### Workflows

| Workflow | When | What |
|---|---|---|
| `ci.yml` | pull request / manual | structural gate for `deploy/mail/compose.yaml` |
| `deploy.yml` | push to `main` / manual | deploy on the host, then verify ports and tailnet TLS |
| `pr-agent.yml` | pull request / comment | PR-Agent review / describe / improve |

### Runner

- `github-runner` — registered for `X-Ray-X/rayofsunshine-site`.
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
`config.fallback_models` holds cheaper fallbacks.
