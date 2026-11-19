# Observability

The Go service exports operational metrics through OpenTelemetry and exposes
them in Prometheus format. Tracing is optional; Prometheus and Grafana ship with
the application deployment unit on loopback ports only.

## Endpoint

`GET http://127.0.0.1:8080/metrics` needs no credentials and returns Prometheus
text. It is served by the same loopback-only listener as the API. Labels are the
matched route pattern and the response status, never a raw path, mail address,
message identifier, or credential. There is no mail content in metrics.

The instrumentation records `http_server_request_duration_seconds` with
`http_route` and `http_response_status_code` labels. The dashboard uses it for
request rate, 5xx rate, and p95 latency.

## Tracing

Set `MAILFLOW_OTEL_EXPORTER_OTLP_ENDPOINT` to an OTLP/HTTP endpoint (for example
`http://collector:4318`) to export traces. The endpoint must be HTTP or HTTPS and
is validated at startup. An empty value disables export; a collector that is
unavailable produces bounded exporter warnings and never fails a request. No
mail address or identifier is placed in a span name.

## Local stack

```sh
docker buildx build --platform linux/arm64 -t mailflow-api:dev app/
cd deploy/app
cp .env.example .env
docker compose up -d
```

| Service | Address |
|---|---|
| API metrics | `http://127.0.0.1:8080/metrics` |
| Prometheus | `http://127.0.0.1:9090/` |
| Grafana | `http://127.0.0.1:3000/` |

Prometheus scrapes `api:8080` every 15 seconds. Its `prometheus.yml` and the
Grafana provisioning files are copied into small derived images at deploy time;
the Compose project has no host bind mounts because the self-hosted runner's
checkout is not visible to the homelab Docker daemon. A config change therefore
needs a rebuild and redeploy. Grafana has a provisioned Prometheus datasource
and a read-only "Mailflow API" dashboard.
Anonymous viewer access is enabled only because the port is loopback; the
Compose gate fails if either observability port is published on all interfaces.
Prometheus history lives in the `mailflow-app-prometheus` volume and is not
shipped anywhere.

## Live check (2026-11-18)

Against the running local stack, `/metrics` answered `200` and contained
`http_route` values for `/v1/mailboxes`, `/v1/search`, `/healthz` and `/metrics`
after traffic, with no address or message identifier in the body. Prometheus
reported target `mailflow-api` as up, and Grafana's API returned the "Mailflow
API" dashboard with three panels. The unit test asserts that a request whose raw
path contains an address is recorded only under its route pattern.

A controlled traffic spike for this dashboard can be produced with the manual
load check in [`docs/load-testing.md`](load-testing.md). It uses the existing
metrics as the server-side view and does not add exporters or dashboard changes.

Out of scope: alerting, SLOs, log aggregation, tracing the mail server or worker,
remote metric storage, and high-cardinality labels.
