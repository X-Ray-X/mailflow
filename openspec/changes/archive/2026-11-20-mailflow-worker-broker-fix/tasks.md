# Tasks

## 1. Deploy fix

- [x] 1.1 Derive the AMQP URL user from `RABBITMQ_USER` in `deploy-app.yml`,
  assert consistency after substitution, and verify with a local simulation
  that the generated user and URL user match.
- [x] 1.2 Require the worker to be running with zero restarts in the health
  loop, and verify the loop fails for a crash-looping container.
- [ ] 1.3 Run `./app/scripts/go.sh` and `openspec validate --all --strict`;
  commit on `mailflow-worker-broker-fix`, PR, merge, and confirm a real deploy
  leaves the worker healthy.

## Workflow follow-up

- Archive the change and sync its delta.
