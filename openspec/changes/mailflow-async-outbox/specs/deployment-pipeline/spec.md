# Spec Delta

## ADDED Requirements

### Requirement: Application deployment includes the queue and worker

The `Deploy application` workflow MUST generate RabbitMQ credentials as
deployment secrets, deploy the broker and worker services with the application,
and verify that they are running. The mail container MUST remain unchanged.

#### Scenario: A commit is deployed

- **WHEN** the application deploy workflow finishes
- **THEN** the broker and worker containers are running and the run summary
  names them

#### Scenario: A broker credential is committed

- **WHEN** a pull request adds a RabbitMQ password to a tracked file
- **THEN** the secret gate fails and the change cannot merge
