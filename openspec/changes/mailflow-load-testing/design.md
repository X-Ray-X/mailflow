# Design

## Context

See `proposal.md` for motivation. The application publishes `/v1` over a
loopback listener, exports OpenTelemetry metrics through `/metrics`, and ships a
provisioned Prometheus/Grafana dashboard. The self-hosted runner cannot expose
its checkout path to the host Docker daemon, which is why CI and deploy stream
source into containers instead of bind-mounting it. No load generator exists.

## Goals / Non-Goals

**Goals:** a one-action demo trigger; a bounded, repeatable run; a result that
can be correlated with the existing dashboard; zero persistent artifacts.

**Non-Goals:** write-path load, mail-protocol load, CI gating, SLOs, alerting,
remote-write integration, and new application services.

## Decisions

### Manual workflow is the primary trigger

`workflow_dispatch` with inputs for duration and VUs is the canonical path. The
local wrapper is a convenience that calls the same script and image. The
alternative — a pull-request trigger — would make latency a flaky merge gate on
a small homelab and encourage tuning the test to the machine rather than the
service. A push trigger would run loads on every deploy, which is wasteful and
risky.

### The generator joins the private app network and streams its script

`docker run --rm -i --network mailflow-app-data ... k6 run -` reads the script
from stdin and targets `http://api:8080`. The alternative — a bind mount of the
checkout — fails on this runner for the same reason the deploy failed earlier.
A permanent k6 Compose service was rejected: it would change the application
service set and add an always-on process for an occasional check.

### Read endpoints only

The script exercises only GET routes. Sends create mail, idempotency records,
and outbox events; cleaning those up means touching real mailbox state or
application tables, which contradicts the "leave no state" rule. The
alternative — a write phase with a manifest and cleanup — was deliberately
deferred. If it is ever added, it needs its own opt-in input and a dedicated
account.

### Existing metrics are the server-side result

No k6 remote-write output, no new exporter, no dashboard edits. The load run's
time window is recorded in the job summary, and the operator reads request
rate, error rate, and p95 from the existing dashboard. The alternative —
pushing k6 metrics into Prometheus — would add remote-write receiver config,
pollute the production dashboard with test-only series, and imply SLOs we do
not have.

### Thresholds are guardrails, not SLOs

k6 fails the run on excessive errors, failed checks, or p95 over a generous
ceiling. These values protect the demo from a broken run; they are not promises
about service availability. The alternative — treating them as product SLOs —
would overstate what a laptop-class homelab can guarantee.

## Risks / Trade-offs

- **Someone runs the load during a deploy.** The workflow has a concurrency
  group and a short timeout, and the docs say not to overlap deploys. Full
  cross-workflow locking would be more machinery than this demo needs.
- **A ceiling is still too high for the host.** Defaults are low (10 VUs, 1
  minute) and inputs are capped; the operator can tune down.
- **Prometheus's 15-second scrape can miss a very short run.** The default run
  is one minute, and docs advise waiting one scrape interval before reading the
  dashboard.
- **k6's image version floats if unpinned.** The version is pinned in the
  workflow and recorded in the docs, consistent with the other toolchain images.
