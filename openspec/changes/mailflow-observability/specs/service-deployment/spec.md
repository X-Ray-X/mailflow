# Spec Delta

## ADDED Requirements

### Requirement: The service exposes operational metrics

The service SHALL publish a `/metrics` endpoint on the loopback-only API
listener, in Prometheus exposition format, from a dedicated registry. Metrics
SHALL include HTTP request count and latency by route and status. The endpoint
SHALL NOT require credentials and SHALL NOT contain mail content, recipient
addresses, identifiers, or credentials in labels or output.

#### Scenario: An operator scrapes metrics

- **WHEN** a Prometheus server requests `/metrics` after API traffic
- **THEN** the response contains request counts and latency observations for
  instrumented routes and no mail content

#### Scenario: A request carries a mail address

- **WHEN** a request with a mail address in its path or body is instrumented
- **THEN** the metric labels use only the route pattern and status, never the
  address or identifier

### Requirement: Tracing is optional and bounded

The service SHALL export OpenTelemetry traces over OTLP HTTP when an endpoint is
configured, and SHALL start without a collector otherwise. Trace setup SHALL be
bounded at startup and SHALL NOT fail an otherwise healthy request when the
collector is unavailable.

#### Scenario: No collector is configured

- **WHEN** the service starts without an OTLP endpoint
- **THEN** it serves requests and metrics normally

#### Scenario: A collector is unreachable

- **WHEN** the configured OTLP endpoint cannot be reached
- **THEN** requests still succeed and no credential or mail content is logged

### Requirement: Dashboards are part of the application unit

The application Compose project SHALL include Prometheus and Grafana on
loopback-only ports, with a provisioned datasource and a dashboard showing
request rate, error rate, and latency. Neither container SHALL publish a port
beyond loopback or join the mail network.

#### Scenario: An operator opens the dashboard

- **WHEN** the application stack is running
- **THEN** Grafana shows the service dashboard with data scraped from the API

#### Scenario: A dashboard port is widened

- **WHEN** a pull request publishes Prometheus or Grafana on all interfaces
- **THEN** the application Compose gate fails
