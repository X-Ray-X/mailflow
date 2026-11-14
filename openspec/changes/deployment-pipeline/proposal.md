# Proposal

## Why

The mail infrastructure (Stalwart v1.1.0, `deploy/mail/`) is deployed on the
homelab host as a single Docker Compose project, but nothing about that
deployment is yet repeatable: the release is pinned, the stack is verified by a
manual script, and every change has to be applied by hand on the host.

## What Changes

- Add a continuous-delivery pipeline for the mail infrastructure:
  - `.github/workflows/ci.yml` — PR gate: the compose project renders, contains
    only the mail service, the image is the pinned release, and the published
    ports match the documented inventory.
  - `.github/workflows/deploy.yml` — on push to `main` (or manual dispatch):
    checkout → write `deploy/mail/.env` from the GitHub `production`
    Environment secrets → `bootstrap.sh` → `docker compose up -d` on the
    homelab host → verify published ports and the tailnet TLS certificate.
  - `.github/workflows/pr-agent.yml` — PR-Agent as a review / describe /
    improve agent inside the PR flow.
  - `openspec/changes/deployment-pipeline/` — this change itself, tracked with
    OpenSpec.
- Keep the deployment boundary: an application deploy must never recreate,
  upgrade, or reset the mail containers or volumes.
- No new ports published: IMAPS (993) and SMTP (465) land on the LAN, JMAP and
  HTTPS stay loopback only; the stack stays reachable over Tailscale at
  `mail.mailflow.test`.

## Capabilities

### New Capabilities

- `deployment-pipeline`: reproducible CI gate for the mail infrastructure,
  automated deploy to the homelab host, and tailnet-access verification.

### Modified Capabilities

- `mail-infrastructure`: deployment is now repeatable by the pipeline and is
  followed by the host-side spec verification (`scripts/verify.sh`).
- `mailbox-access`: unchanged by this change (it describes client access to the
  mail server).

## Impact

- New deployment/configuration assets: three GitHub Actions workflows and one
  OpenSpec change.
- New operational routine: every pull request gets a fast structural gate, and
  a push to `main` updates the stack on the homelab host host.
- Secrets moved from `deploy/mail/.env` (untracked on the host) into GitHub
  Environment secrets named `MAIL_BOOTSTRAP_PASSWORD`,
  `MAIL_ACCOUNT_ALICE_PASSWORD`, `MAIL_ACCOUNT_BOB_PASSWORD` in the
  `production` Environment.
- Out of scope: this change does not change the mail configuration, the pinned
  Stalwart release, or the TLS plan; those stay a deliberate change in the
  `stalwart-local-environment` branch.
