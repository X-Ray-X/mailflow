# Design

## Context

See `proposal.md` for motivation. `ci.yml` currently has one job that checks the
rendered mail and application Compose files. `deploy-app.yml` builds only the Go
image and generates `.env` from `.env.example`. The repository now has a Go
module, a Node client, Python verification scripts, and OpenSpec artifacts, none
of which are checked on a pull request. The runner is self-hosted on arm64.

## Goals / Non-Goals

**Goals:**

- Make the pull-request verdict depend on buildable, tested code and valid specs.
- Keep the app Compose invariants explicit now that a third service exists.
- Deploy the client image from the same commit as the API.

**Non-Goals:**

- Load testing, registries, hosted CI services, or changing the mail deploy.
- Changing application behaviour or the published contract.

## Decisions

### Each quality gate runs in a pinned container image

Go checks use `golang:1.27.4-alpine` through `app/scripts/go.sh`; client checks
use `node:22-alpine`; OpenSpec validation uses `npx @fission-ai/openspec@2.0.0`.
The alternative was installing toolchains on the self-hosted runner; it would
make the verdict depend on machine state and drift between runs. Python syntax
checks use `python3 -m py_compile`, which needs no third-party package.

### Quality gates are steps in the existing job

The new checks are steps in `compose-contract` rather than a separate job. The
single required check already covers every Compose project, and a second job
would duplicate checkout and complicate the branch-protection story. The cost is
a longer job; each step prints its own result so a failure is attributable.

### The application gate is updated, not loosened

The existing assertion of exactly `{api, db}` becomes `{api, db, web}`. The new
assertions keep the database private, forbid bind mounts, require loopback for
every published port, and check that the client's frontend network is not
internal while the data network still is. The alternative was dropping the
service-set assertion; it would stop detecting accidental new services.

### The deploy builds a second commit-tagged image

`deploy-app.yml` builds `mailflow-web:<sha>` with `docker build` and substitutes
`MAILFLOW_WEB_IMAGE` into the generated `.env`. The health loop checks the web
container as well as the API and database, but does not curl it: the runner's
loopback is not the host's, and container health is the existing pattern. The
alternative was pushing to a registry; it adds credentials and a failure mode
the homelab does not need.

## Risks / Trade-offs

- **Container pulls and npx need network on the runner.** The images and the
  pinned OpenSpec package are already used by local development. If the runner
  is offline, the gate fails loudly rather than passing unverified code.
- **A longer single job.** Each step names its result, and cancelling in-progress
  runs remains disabled so a deploy cannot be raced.
- **Client health is container state, not a browser check.** The interactive UI
  pass stays a documented manual step; the deploy gate proves the static service
  is up, not that every click works.
