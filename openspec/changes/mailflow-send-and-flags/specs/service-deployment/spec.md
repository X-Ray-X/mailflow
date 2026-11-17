# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
