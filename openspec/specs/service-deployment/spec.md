# service-deployment Specification

## Purpose

Defines the application as a deployment unit of its own, so deploying, restarting, or rolling it back can never recreate, stop, or upgrade the mail infrastructure.

## Requirements

### Requirement: The application is a separate deployment unit
The service SHALL deploy as its own Compose project from its own directory, sharing no container, volume, or configuration file with the mail infrastructure.

#### Scenario: Application deployment leaves mail untouched
- **WHEN** the service is deployed, restarted, or rolled back
- **THEN** the mail containers, configuration, and data volumes are unchanged

#### Scenario: Stopping the application does not stop mail
- **WHEN** the application stack is stopped
- **THEN** the mail server keeps serving clients

### Requirement: Configuration is validated at startup
The service SHALL read its configuration from the environment, validate it at startup, and refuse to start when a required value is missing or malformed.

#### Scenario: Missing configuration fails fast
- **WHEN** a required environment value is absent or malformed
- **THEN** the process exits with a non-zero status and names the offending value

#### Scenario: Valid configuration starts the service
- **WHEN** every required value is present and valid
- **THEN** the process starts and answers requests

### Requirement: Health is independent of the mail server
The service SHALL expose an unauthenticated health endpoint that reports whether the process can serve requests. A mail server outage SHALL NOT make the process report itself unhealthy, so a mail fault does not restart the application.

#### Scenario: Healthy process
- **WHEN** the service is running
- **THEN** the health endpoint reports success without credentials

#### Scenario: Mail outage does not fail health
- **WHEN** the mail server is unreachable
- **THEN** the health endpoint still reports success

### Requirement: Native arm64 execution
The service image SHALL run natively on arm64, the homelab architecture, without emulation.

#### Scenario: Image architecture
- **WHEN** the image built for deployment is inspected
- **THEN** it provides a linux/arm64 variant

### Requirement: Host deployment settings come from the environment
The pipeline SHALL take host-specific deployment settings, such as the published loopback port, from the deployment environment rather than from the commit or from a file left in the checkout, and SHALL write them into the generated deployment file.

#### Scenario: The published port is supplied by the environment
- **WHEN** the workflow deploys
- **THEN** the generated `deploy/app/.env` uses `MAILFLOW_API_PORT` from the production environment, or 8080 when it is unset

#### Scenario: An occupied port fails before anything starts
- **WHEN** another container already publishes the configured loopback port
- **THEN** the workflow reports failure, names the variable to change, and starts no application container

#### Scenario: The database password is supplied by the environment
- **WHEN** the workflow deploys
- **THEN** the generated `deploy/app/.env` carries the database password from the
  production environment, and neither the password nor a file containing it is
  committed

### Requirement: Deployment reports a verdict
The application pipeline SHALL reach a final verdict for every deployment, and SHALL verify after deploying that the service answers its health endpoint and that the mail infrastructure was not modified.

#### Scenario: Successful deployment
- **WHEN** the application workflow completes
- **THEN** it reports success, the health endpoint answers, and the mail containers are still running with unchanged configuration

#### Scenario: Failed deployment is reported
- **WHEN** the service fails to start or fails its health check
- **THEN** the workflow reports failure and leaves the previously deployed application state described in its summary

### Requirement: The application unit includes its database

The application Compose project SHALL contain the service and its database. The
database SHALL run arm64-native, keep its data in an application-owned volume,
and publish no host port. It SHALL share no container, network, or volume with
the mail project beyond the application's existing network attachment.

#### Scenario: The application project renders two services

- **WHEN** the application Compose project is configured
- **THEN** it names the service and the database, and no other service

#### Scenario: The database is not published

- **WHEN** the configured database service is inspected
- **THEN** it declares no host port mapping

#### Scenario: The database volume is application-owned

- **WHEN** the configured volumes and mounts are inspected
- **THEN** the database's volume is named for the application, and neither the
  mail data directory nor a mail volume is mounted

### Requirement: The structural gate covers the application project

The repository's structural check SHALL fail when the application project stops
being a separate unit, when the database publishes a port, when the state volume
is not application-owned, or when the mail data directory appears in it.

#### Scenario: A published database port fails the gate

- **WHEN** the application Compose file maps a host port for the database
- **THEN** the structural check fails and names the offending service

#### Scenario: A mail mount fails the gate

- **WHEN** the application Compose file mounts the mail data directory or a mail
  volume
- **THEN** the structural check fails

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

### Requirement: Application services use no host bind mounts

Every service in the application Compose project MUST obtain its configuration
from an image or a named volume. No service MAY bind-mount a path from the
checkout, because the self-hosted runner's checkout is not visible to the
homelab Docker daemon.

#### Scenario: A pull request adds a bind mount

- **WHEN** any application service declares a bind mount
- **THEN** the application Compose gate fails and names the offending service

#### Scenario: The observability stack starts on the runner

- **WHEN** the application deploy applies the Compose project
- **THEN** Prometheus and Grafana start with their configuration baked into
  their images, with no host path resolution

### Requirement: Deploy builds every custom image from the commit

The application deploy MUST build the API, web, Prometheus, and Grafana images
from the same commit and write their tags into the generated `.env`. It MUST NOT
rely on a registry or on files outside the checked-out source.

#### Scenario: A commit is deployed

- **WHEN** the deploy workflow finishes building
- **THEN** all four image tags carry the commit SHA and Compose renders with
  those tags
