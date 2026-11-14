# Tasks

## 1. Add the CI gate

- [x] 1.1 Write `.github/workflows/ci.yml` with a compose-contract job that
  verifies the render, service inventory, pinned image and published ports.
- [x] 1.2 The gate reads only the rendered config (`docker compose config`:
  services, images, ports), never the live stack, so it tests the pull request
  rather than the host. Ports are compared in ascending order
  (`443,465,993,8080`).
- [ ] 1.3 Confirm the gate passes on PR #1. The first run failed with
  `IndentationError`: the multi-line Python was indented inside the YAML block
  scalar. The script is now a single line and asserts the rendered ports.

## 2. Add the deployment workflow

- [x] 2.1 Write `.github/workflows/deploy.yml` with the steps: checkout,
  prepare `.env` from the `production` Environment, `bootstrap.sh`,
  `docker compose up -d`, wait for HTTPS, ports + tailnet certificate, summary.
- [x] 2.2 Create the GitHub Environment `production` with secrets
  `MAIL_BOOTSTRAP_PASSWORD`, `MAIL_ACCOUNT_ALICE_PASSWORD`,
  `MAIL_ACCOUNT_BOB_PASSWORD` (all required for a deployment).
- [x] 2.3 Open a pull request and observe the deployment run on the self-hosted
  runner. Verify: the job deploys the stack on the host and the summary shows
  the verdict (PR #1 open → branch `add-deployment-pipeline`).

## 3. Add PR-Agent to the PR flow

- [x] 3.1 Write `.github/workflows/pr-agent.yml` using
  `The-PR-Agent/pr-agent@main` with the GitHub Environment secret
  `GEMINI_API_KEY` (via `GOOGLE_AI_STUDIO.GEMINI_API_KEY`), `GITHUB_TOKEN` and
  the `github_action_config.*` flags. Uses free-tier Gemini through Google AI
  Studio: `config.model` must carry the LiteLLM provider prefix
  (`gemini/gemini-3-flash-preview`), otherwise the bare model id is routed to
  Vertex AI and fails with `DefaultCredentialsError`. No fallback model is
  configured (`[]`).
- [x] 3.2 Add the GitHub Environment secret `GEMINI_API_KEY` (free-tier Google
  AI Studio key). Verify: the workflow references the secret and the gate passes.
- [x] 3.3 Open a pull request and observe PR-Agent's review/describe/improve
  comments. Verify: the comments appear on PR #1 (branch `add-deployment-pipeline`).

## 4. Record the change in OpenSpec

- [x] 4.1 Complete this change: proposal, design, tasks, specification.
- [ ] 4.2 When the change's acceptance scenarios are observed to pass, archive
  it (moving it from `openspec/changes/` to `openspec/changes/archive/`).
