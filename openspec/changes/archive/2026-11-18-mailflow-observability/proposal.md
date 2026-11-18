# Proposal

## Why

The service logs one line per request but exposes no metrics or traces, so an
operator cannot see request rate, errors, or latency without reading logs. The
homelab needs a small observability stack that uses the published service and
does not touch the mail deployment unit.

## What Changes

- Instrument the HTTP server with OpenTelemetry, exporting traces through OTLP
  when an endpoint is configured and always exposing Prometheus metrics from a
  dedicated registry.
- Publish `/metrics` on the loopback-only API listener, unauthenticated and with
  no mail content in labels or messages.
- Add Prometheus and Grafana containers to the application Compose project,
  with a provisioned datasource and a small dashboard for request rate, error
  rate, and latency.
- Make the OTLP endpoint optional so local runs work without a collector.
- Document the endpoints, scrape configuration, and what is and is not measured.

Out of scope: log aggregation, alerting rules, SLOs, tracing the mail server or
worker, high-cardinality labels, remote storage, and any change to the mail
deployment unit or the published `/v1` contract.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `service-deployment`: adds observability endpoints and containers to the
  application unit.

## Impact

- `app/`: new observability package, dependencies, routes, and configuration.
- `deploy/app/`: Prometheus and Grafana services with provisioned configuration;
  loopback ports only.
- `docs/`: endpoints, dashboard, and local verification.
- No mail protocol, mailbox state, or `/v1` contract change.
