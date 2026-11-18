# Design

## Context

See `proposal.md` for motivation. The API already logs one structured line per
request with the route pattern, never the raw path or credentials. The
application Compose project has a loopback-published API and a private data
network. OpenTelemetry, Prometheus, and Grafana are not present yet.

## Goals / Non-Goals

**Goals:**

- Measure HTTP rate, errors, and latency without leaking mail data.
- Keep local runs working with no collector.
- Ship a usable dashboard with the application unit.

**Non-Goals:**

- Alerting, SLOs, log aggregation, tracing the mail server or worker, or remote
  metric storage.

## Decisions

### A dedicated Prometheus registry behind `/metrics`

A registry owned by the observability package is passed into the HTTP server as
a handler. The alternative was the Prometheus default registry; it would mix
Go runtime and third-party collectors into the contract and make tests depend on
global state. `/metrics` is registered outside the protected `/v1` group and is
bound by the same loopback-only listener, so it is reachable only locally.

### Route patterns are the only high-cardinality guard

The OpenTelemetry HTTP instrumentation labels requests by the matched route
pattern and status, not by raw paths, which contain message identifiers. The
alternative — custom middleware deriving labels from URLs — would reintroduce
identifiers and make the leak easy to miss in review. No body, address, or
header value is ever a metric label.

### OTLP export is optional and non-fatal

A tracer provider with an OTLP HTTP exporter is installed only when
`MAILFLOW_OTEL_EXPORTER_OTLP_ENDPOINT` is set. The exporter uses a bounded
timeout and a batch processor; an unavailable collector produces exporter
warnings, not failed requests. The alternative — requiring a collector — would
break local development and make observability a dependency of mail delivery.

### Prometheus and Grafana are provisioned, not clicked together

The datasource and dashboard live as files mounted read-only into their
containers. The alternative was configuring Grafana through its API at deploy
time; it would add a mutable step and a secret for an admin login. Grafana runs
with anonymous viewer access on a loopback-only port, so no admin password is
created or stored.

## Risks / Trade-offs

- **A new dependency set.** OpenTelemetry and Prometheus client libraries are
  standard and pinned in `go.mod`; the existing Go checks still gate them.
- **Metrics are process-local.** No remote storage means history is limited to
  the Prometheus container's volume, which is documented.
- **Anonymous Grafana access.** Acceptable only because the port is loopback;
  the Compose gate asserts that it never widens.
