# Spec Delta

## ADDED Requirements

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
