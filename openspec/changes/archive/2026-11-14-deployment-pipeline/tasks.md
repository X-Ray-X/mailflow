# Tasks

## 1. Add the CI gate

- [x] 1.1 Write `.github/workflows/ci.yml` with a compose-contract job that
  verifies the render, service inventory, pinned image and published ports.
- [x] 1.2 The gate reads only the rendered config (`docker compose config`:
  services, images, ports), never the live stack, so it tests the pull request
  rather than the host. Ports are compared in ascending order
  (`443,465,993,8089`).
- [x] 1.3 Confirm the gate passes on a pull request. Run `43555418517`
  (PR #2, `a0b0f1d`) passed in 22s. The first attempt failed with
  `IndentationError`: the multi-line Python was indented inside the YAML block
  scalar. The script is now a single line and asserts the rendered ports.

## 2. Add the deployment workflow

- [x] 2.1 Write `.github/workflows/deploy.yml` with the steps: checkout,
  prepare `.env` from the `production` Environment, `bootstrap.sh`,
  `docker compose up -d`, wait for HTTPS, ports + tailnet certificate, summary.
- [x] 2.2 Create the GitHub Environment `production` with secrets
  `MAIL_BOOTSTRAP_PASSWORD`, `MAIL_ACCOUNT_ALICE_PASSWORD`,
  `MAIL_ACCOUNT_BOB_PASSWORD` (all required for a deployment).
- [x] 2.3 Observe the deployment run on the self-hosted runner. PR #1 was
  merged into `stalwart-local-environment`, not `main`, so `deploy.yml`
  (push to `main`) has never run. Run `43561620282` (`main`, `11516f9`) passed
  end to end: bootstrap created the administrator, installed the certificate,
  and the HTTPS wait and SAN check passed.
- [x] 2.4 Mount `/path/to/mailflow` into the runner container at the
  same absolute path and use
  `MAIL_DATA_DIR=/path/to/mailflow/deploy/mail/data`. Confirmed from a
  read-only probe: without it, the host daemon resolves bind mounts outside the
  checkout and the mail data is not persistent. The deployed container now
  mounts `/path/to/mailflow/deploy/mail/data/{etc,lib,tls}`, owned by
  uid/gid `2000`.
- [x] 2.5 Reach the service from inside the container. The runner's loopback is
  not the host's, so the published setup and management ports are unreachable
  from the workflow. `bootstrap.sh`, the HTTPS wait, and the SAN check use
  `docker exec mailflow-stalwart` against the internal listeners (`8080`,
  `443`). Run `43561620282` passed with this path; the internal setup and
  HTTPS listeners answer `200`.

## 3. Add PR-Agent to the PR flow

- [x] 3.1 Write `.github/workflows/pr-agent.yml` using
  `The-PR-Agent/pr-agent@v0.50.0` with the GitHub Environment secret
  `GEMINI_API_KEY` (via `GOOGLE_AI_STUDIO.GEMINI_API_KEY`), `GITHUB_TOKEN` and
  the `github_action_config.*` flags. The job declares
  `environment: production` so the Environment secret is visible; that also
  makes the mail secrets readable by the action, which is accepted for now.
  Uses free-tier Gemini through Google AI
  Studio: `config.model` must carry the LiteLLM provider prefix
  (`gemini/gemini-3-flash-preview`), otherwise the bare model id is routed to
  Vertex AI and fails with `DefaultCredentialsError`. No fallback model is
  configured (`[]`).
- [x] 3.2 Wire the GitHub Environment secret `GEMINI_API_KEY` (free-tier Google
  AI Studio key) into the workflow. The first run did not authenticate: the
  bare model id was routed to Vertex AI.
- [x] 3.3 Observe PR-Agent's review/describe/improve comments. Run
  `43555418406` (PR #2, `a0b0f1d`) passed after declaring
  `environment: production`; the PR shows a generated description, a Reviewer
  Guide and Code Suggestions.

## 4. Record the change in OpenSpec

- [x] 4.1 Complete this change: proposal, design, tasks, specification.
- [x] 4.2 When the change's acceptance scenarios are observed to pass, archive
  it (moving it from `openspec/changes/` to `openspec/changes/archive/`).
  Deployment run `43561620282` passed; archiving with the deployment-pipeline
  spec synced to the main specs.
