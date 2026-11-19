# Spec Delta

## Purpose

Defines how a bounded load check can exercise the published API without leaving
state behind or becoming a permanent part of the deployment. It exists so a
repeatable demo and a capacity check use the same safe path.

## ADDED Requirements

### Requirement: Load testing is manual and bounded

A load test SHALL be started only by an explicit manual trigger, never by a
pull request or a push. It MUST have a bounded duration, a bounded number of
virtual users, and a job timeout. Concurrent load runs SHALL NOT overlap.

#### Scenario: A developer starts a load test

- **WHEN** a developer triggers the load workflow with a duration and VU count
- **THEN** the run uses ceilings that reject larger values and stops at the job
  timeout even if the generator misbehaves

#### Scenario: A pull request is opened

- **WHEN** code changes arrive on a pull request
- **THEN** no load test runs; the normal CI quality gates run instead

### Requirement: A load run leaves no state behind

The load generator SHALL use an ephemeral container with no persistent volume,
no bind mount of the checkout, and no result files in the repository. It MUST
exercise only read endpoints and MUST NOT create, change, or delete mail,
idempotency records, outbox rows, or dashboard configuration.

#### Scenario: The run finishes

- **WHEN** the load container exits, successfully or not
- **THEN** no container, volume, or tracked file remains from the run

#### Scenario: A read endpoint is called

- **WHEN** the generator requests a message, search result, or attachment
- **THEN** the mail server's mailbox state is unchanged after the run

### Requirement: Credentials and results stay contained

Credentials SHALL come from the production GitHub Environment at run time and
SHALL NOT appear in tracked files, the image, the job summary, or logs. The
client-side k6 result SHALL be reported in the job summary; the server-side
result SHALL come from the existing Prometheus metrics and Grafana dashboard
over the same time window.

#### Scenario: The job summary is read

- **WHEN** a load run completes
- **THEN** the summary shows k6 checks and latency but no credential or mail
  content

#### Scenario: The dashboard is inspected

- **WHEN** an operator looks at the run window in Grafana
- **THEN** request rate, error rate, and p95 for the exercised routes reflect
  the load, without any load-test-specific dashboard change
