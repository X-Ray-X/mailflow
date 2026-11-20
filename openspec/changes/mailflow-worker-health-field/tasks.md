# Tasks

## 1. Health field fix

- [x] 1.1 Change the worker health check to read top-level `RestartCount`,
  verify the template against a live container, run `./app/scripts/go.sh` and
  `openspec validate --all --strict`, then commit, PR, merge, and confirm the
  deploy passes with a healthy worker.

## Workflow follow-up

- Archive the change and sync its delta.
