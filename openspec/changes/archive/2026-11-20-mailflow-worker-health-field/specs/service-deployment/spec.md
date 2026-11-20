# Spec Delta

## ADDED Requirements

### Requirement: Deploy health checks read real inspect fields

The application deploy health loop MUST read a container's restart count from
the top-level `RestartCount` field of `docker inspect`. A template error or an
empty value MUST NOT cause a healthy worker to be reported as unhealthy.

#### Scenario: The worker has not restarted

- **WHEN** `docker inspect` reports a top-level `RestartCount` of zero and the
  worker is running
- **THEN** the worker satisfies the health gate

#### Scenario: The inspect template is wrong

- **WHEN** a health template names a field that does not exist
- **THEN** the deploy does not treat the empty result as a successful check
