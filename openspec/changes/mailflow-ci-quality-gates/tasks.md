# Tasks

## 1. Pull-request gates

- [x] 1.1 Add Go, web client, Python syntax, and OpenSpec validation steps to
  `.github/workflows/ci.yml`, each in a pinned toolchain image; verify the
  commands by running the same container commands locally and recording their
  output, and check the workflow YAML parses.
- [x] 1.2 Update the application Compose gate in `ci.yml` for the `web` service
  and frontend network while keeping the database, loopback, and no-mail-mount
  assertions; verify by extracting the gate script and running it against
  `deploy/app/.env.example` with the web image variable set.

## 2. Deployment and close-out

- [x] 2.1 Build the commit-tagged web image in `deploy-app.yml`, write its tag
  into the generated `.env`, check the client container's health in the wait
  loop, and name its address in the run summary; verify the workflow YAML parses
  and the deploy still asserts the mail container is unchanged.
- [x] 2.2 Update `README.md` and `docs/api.md` with the required checks and what
  each proves; verify the documented commands match the workflow steps and run
  against the local checkout.
- [x] 2.3 Run `./app/scripts/go.sh`, the web build/tests, and
  `openspec validate --all --strict`; review the diff for secrets and accidental
  scope changes, then commit on the `mailflow-ci-quality-gates` branch with the
  verification result and verify the tree is clean.

## Workflow follow-up

- Archive the change after the gates have been observed to pass.
- Verify the archived result and sync any remaining delta.
