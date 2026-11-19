# Tasks

## 1. Generator and trigger

- [x] 1.1 Write a bounded k6 script for read-only `/v1` endpoints with route
  checks, input ceilings, timeouts, and failure thresholds; verify it against
  the local stack with a short run and confirm the summary contains no mail
  content.
- [ ] 1.2 Add a `workflow_dispatch` workflow that validates inputs, streams the
  script into a pinned k6 image on `mailflow-app-data`, uses production
  secrets, has a timeout and concurrency group, and writes only the k6 summary
  to the job summary; verify YAML parses and trigger one short run on the
  homelab, watching it to a verdict.
- [x] 1.3 Add a local wrapper that uses the same script, image, and network;
  verify a short local run produces a visible spike in the existing Grafana
  dashboard and leaves no container or file behind.

## 2. Close-out

- [x] 2.1 Document the trigger, inputs, correlation with Grafana, and the
  read-only boundary in `docs/load-testing.md`, with links from `README.md` and
  `docs/observability.md`; verify the documented commands.
- [ ] 2.2 Run `./app/scripts/go.sh`, the web build/tests, and
  `openspec validate --all --strict`; review the diff for secrets, unbounded
  runs, persistent artifacts, and mail-state changes, then commit on the
  `mailflow-load-testing` branch with the verification result.

## Workflow follow-up

- Open a PR, merge after CI, then archive the change and sync its delta.
