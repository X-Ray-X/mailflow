# Tasks

## 1. Instrumentation

- [x] 1.1 Add an observability package with a dedicated Prometheus registry,
  OpenTelemetry HTTP instrumentation, and optional bounded OTLP trace export;
  verify unit tests that `/metrics` contains route-labelled request data and no
  raw identifiers or addresses, and that startup works without a collector.
- [x] 1.2 Wire `/metrics` and the instrumentation into the API server and
  options without changing `/v1`; verify API tests cover the endpoint, its
  content type, and that protected routes still require credentials.

## 2. Stack and close-out

- [x] 2.1 Add Prometheus and Grafana services with provisioned datasource and
  dashboard on loopback ports; verify the Compose gate rejects a widened port
  and that the local stack serves the metrics target and dashboard.
- [x] 2.2 Update `docs/api.md`, `docs/observability.md`, and `README.md` with the
  endpoints, scrape config, dashboard, and limits; verify documented commands
  against the running local stack.
- [x] 2.3 Run `./app/scripts/go.sh`, the web build/tests, and
  `openspec validate --all --strict`; review the diff for secrets, unbounded
  exporters, high-cardinality labels, and data-loss paths, then commit on the
  `mailflow-observability` branch and verify the tree is clean.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
