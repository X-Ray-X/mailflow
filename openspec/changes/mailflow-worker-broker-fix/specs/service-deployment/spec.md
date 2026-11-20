# Spec Delta

## ADDED Requirements

### Requirement: Worker credentials must match the broker

The application deploy MUST build the worker's AMQP URL from the configured
`RABBITMQ_USER` and generated password. It MUST fail before starting services
if the configured user and the generated URL user differ.

#### Scenario: The broker user is changed

- **WHEN** an operator sets `RABBITMQ_USER` in the generated environment
- **THEN** the worker's AMQP URL uses that same username

#### Scenario: The URL user is inconsistent

- **WHEN** the generated URL would use a different username than the broker
- **THEN** the deploy fails with a message naming the mismatch

### Requirement: A restarting worker fails the deploy

The deploy health check MUST verify that the worker is running and has not
restarted. A container that is merely `Running` while cycling through restarts
MUST NOT satisfy the gate.

#### Scenario: The worker cannot authenticate

- **WHEN** the worker exits repeatedly after a broker authentication failure
- **THEN** the deploy fails and includes the worker's recent logs

#### Scenario: The worker is healthy

- **WHEN** the worker process stays up without restarting
- **THEN** the deploy proceeds to the mail-unchanged assertion and summary
