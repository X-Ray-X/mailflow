# Load testing

A bounded, read-only load check for the published API. It exists to generate a
controlled traffic spike for the demo and for quick capacity checks. It is a
verification activity, not a service: no k6 container, port, dashboard, or
result file survives a run.

## Trigger

The canonical trigger is the manually dispatched **Load test — mailflow API**
workflow. It never runs on a pull request or a push, so it cannot gate a merge
or overlap a deploy unnoticed.

| Input | Default | Ceiling |
|---|---|---|
| `duration` | `1m` | 10s–5m |
| `vus` | `10` | 1–50 |

It runs on the self-hosted runner with `environment: production`, attaches a
pinned `grafana/k6:0.54.0` container to `mailflow-app-data`, and reaches
`http://api:8080`. Credentials come from the `MAIL_ACCOUNT_BOB_PASSWORD`
Environment secret; the account address is the public demo address. The script
is streamed on stdin, so there is no bind mount of the checkout.

After the run, the job summary contains the k6 output and a pointer to the
Grafana dashboard. No artifacts are uploaded.

```sh
gh workflow run load-test.yml --ref main -f duration=1m -f vus=10
gh run list --workflow load-test.yml --limit 1
gh run watch <run-id> --exit-status
```

## Local run

The same script and image can be run from a checkout against the local stack:

```sh
VUS=10 DURATION=1m ./app/scripts/load.sh
```

The wrapper reads `deploy/mail/.env` for credentials, validates the same
ceilings, and uses `--rm`. Override `K6_IMAGE`, `MAILFLOW_NETWORK`, or
`BASE_URL` only if you know why you need to.

## What is exercised

Only read endpoints: mailbox list, message pages, text search, message detail,
and attachment metadata. The script discovers the caller's inbox and one
message in `setup()`, so there are no fixed identifiers. Each iteration sleeps
one second, keeping the default load at a modest, homelab-friendly rate.

k6 fails the run if checks drop below 99%, request failures exceed 1%, or p95
exceeds one second. These are guardrails against a broken run, not product SLOs.

## Reading the result

- **Client side:** k6's summary in the job output or local terminal — VUs,
  iterations, check rate, `http_req_failed`, and `http_req_duration` p95.
- **Server side:** the existing **Mailflow API** Grafana dashboard over the run
  window — request rate by `http_route`, 5xx rate, and p95. Prometheus scrapes
  every 15 seconds, so wait one scrape interval before reading the dashboard.

No k6 metrics are pushed into Prometheus, and no dashboard is changed by a load
run. The outbox, RabbitMQ, and worker are not instrumented; their state is
checked with DB counts during the async verification, not here.

## What this deliberately does not do

- No write or send load: those create mail, idempotency rows, and outbox events
  that would have to be cleaned up.
- No IMAP, SMTP, or JMAP load: the published API is the boundary under test.
- No CI trigger, alerting, SLOs, remote write, artifact upload, or permanent
  Compose service.
- No mail deployment change. The mail container and its data directory are
  untouched.
