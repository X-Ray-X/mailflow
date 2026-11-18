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
