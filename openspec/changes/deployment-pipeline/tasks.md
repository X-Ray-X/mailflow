# Tasks

## 1. Add the CI gate

- [x] 1.1 Write `.github/workflows/ci.yml` with a compose-contract job that
  verifies the render, service inventory, pinned image and published ports.
- [x] 1.2 The gate uses the same `docker compose` commands as
  `scripts/verify.sh` (config, ps). Verified against the repository.

## 2. Add the deployment workflow

- [ ] 2.1 Write `.github/workflows/deploy.yml` with the steps: checkout,
  prepare `.env` from the `production` Environment, `bootstrap.sh`,
  `docker compose up -d`, wait for HTTPS, ports + tailnet certificate, summary.
- [ ] 2.2 Create the GitHub Environment `production` with secrets
  `MAIL_BOOTSTRAP_PASSWORD`, `MAIL_ACCOUNT_ALICE_PASSWORD`,
  `MAIL_ACCOUNT_BOB_PASSWORD` (all required for a deployment).
- [ ] 2.3 Open a pull request and observe the deployment run on the self-hosted
  runner. Verify: the job deploys the stack on the host and the summary shows
  the verdict.

## 3. Add PR-Agent to the PR flow

- [ ] 3.1 Write `.github/workflows/pr-agent.yml` using
  `The-PR-Agent/pr-agent@main` with `OPENAI_KEY`, `GITHUB_TOKEN` and the
  `github_action_config.*` flags.
- [ ] 3.2 Add the repository secret `OPENAI_KEY` (free-tier OpenAI-compatible
  key, or the provider key you choose). Verify: the workflow references the
  secret and the gate passes.
- [ ] 3.3 Open a pull request and observe PR-Agent's review/describe/improve
  comments. Verify: the comments appear on the PR.

## 4. Record the change in OpenSpec

- [ ] 4.1 Complete this change: proposal, design, tasks, specification.
- [ ] 4.2 When the change's acceptance scenarios are observed to pass, archive
  it (moving it from `openspec/changes/` to `openspec/changes/archive/`).
