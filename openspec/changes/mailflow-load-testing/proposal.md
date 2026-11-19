# Proposal

## Why

The service now has metrics, a provisioned dashboard, and a repeatable live
verifier, but nothing that generates a controlled traffic spike. For the demo,
and for real capacity checks, we need a bounded load test that is trivial to
trigger and leaves no state behind.

## What Changes

- Add a small k6 script that exercises only read endpoints of the published
  `/v1` contract: mailbox list, message pages, search, message detail, and
  attachment metadata.
- Add a manually triggered GitHub workflow (`workflow_dispatch`) as the primary
  trigger. It runs a pinned k6 image on the self-hosted runner, attaches it to
  the application's private data network, and streams the script on stdin so
  there are no bind mounts and no surviving containers or result files.
- Add a thin local command for developers that uses the same script and image.
- Use the existing Prometheus/Grafana stack as the server-side view of the run
  and k6's own summary as the client-side verdict. No exporters, remote write,
  dashboard changes, or new ports.
- Bound the run with input ceilings, a job timeout, a concurrency group, and k6
  thresholds that fail on errors or latency.

Out of scope: write or send load, IMAP/SMTP/JMAP load tests, CI or PR triggers,
alerting, SLOs, artifact uploads, permanent k6 services, registry pushes, and
any change to the mail deployment unit or the published API.

## Capabilities

### New Capabilities

- `load-testing`: the boundaries and obligations of a repeatable load check.

### Modified Capabilities

None. The `mail-api` contract and the observability stack are consumed as they
stand.

## Impact

- `app/scripts/load/`: k6 script and local wrapper.
- `.github/workflows/load-test.yml`: manual trigger.
- `docs/load-testing.md`, `README.md`, and `docs/observability.md`: how to run
  it and how to read the result.
- No application code, Compose service, database schema, or mail change.
