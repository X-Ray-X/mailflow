# deployment-pipeline Specification

## Purpose
Defines the intended behaviour of the mail infrastructure continuous-delivery
pipeline: what must be true on every pull request, on every push to `main`, and
on the homelab host after a deployment.

## Requirements

### Requirement: CI gate passes on a pull request

A pull request MUST pass the `CI — mail infrastructure` workflow
(`.github/workflows/ci.yml`).

#### Scenario: compose renders and only the mail service is present
- **WHEN** a pull request is opened or updated against `main`
- **THEN** the workflow renders `deploy/mail/compose.yaml`, finds only the
  `stalwart` service, and the compose-contract job reports success.

#### Scenario: pinned release is unchanged in CI
- **WHEN** a pull request changes the image reference in
  `deploy/mail/compose.yaml` or `.env.example`
- **THEN** the compose-contract job detects the change in the resolved image
  and fails the gate until the change is intentional.

### Requirement: push to main deploys the mail infrastructure

When a commit is pushed to `main`, the `Deploy mail infrastructure` workflow
(run on the self-hosted runner) MUST reach a final verdict.

#### Scenario: workflow reports a verdict
- **WHEN** the deployment job finishes
- **THEN** the job summary contains the deployed SHA and a verdict for the
  tailnet reachability.

### Requirement: no new ports are published

The deployment MUST NOT change the published port inventory.

#### Scenario: port inventory is unchanged
- **WHEN** a deployment is applied
- **THEN** the published ports are exactly 993, 465, 443 (loopback) and 8089
  (loopback), and no port 25 is published.

### Requirement: secrets never enter the repository

Account credentials and the bootstrap password MUST NOT appear in tracked
files, container images, or workflow logs.

#### Scenario: secrets are injected at deploy time
- **WHEN** the deployment workflow runs
- **THEN** the file `deploy/mail/.env` is produced from the `production`
  Environment secrets, is not committed, and is mode 600 on the runner.

### Requirement: the mail infrastructure remains reachable over Tailscale

After a deployment, clients on the tailnet MUST be able to reach the mail
server over the canonical hostname with a certificate that covers it.

#### Scenario: tailnet certificate covers the hostname
- **WHEN** the deployment workflow finishes
- **THEN** the step `verify published ports and tailnet certificate` reports
  success (the presented certificate contains `DNS:mail.mailflow.test`).

### Requirement: Pull requests run code-quality gates

The `CI — mail infrastructure` workflow MUST verify the Go service and the web
client on every pull request, using pinned toolchain images. It MUST run Go
formatting, vet, and unit tests; client dependency install, TypeScript build,
and unit tests; Python syntax checks for tracked scripts; and
`openspec validate --all --strict`. A failure in any gate MUST fail the workflow.

#### Scenario: Go code regresses on a pull request

- **WHEN** a pull request changes Go code that is unformatted, fails vet, or
  fails a unit test
- **THEN** the Go quality gate fails and the workflow reports the failing step

#### Scenario: Web client does not build

- **WHEN** a pull request changes client code that fails the TypeScript build or
  a unit test
- **THEN** the web quality gate fails and the workflow reports the failing step

#### Scenario: Specs and code disagree

- **WHEN** an OpenSpec change is left invalid or incomplete
- **THEN** the OpenSpec gate fails and the workflow reports the validation output

### Requirement: Application compose gate covers both services

The application Compose gate MUST assert that the project contains exactly the
API, database, and static client services; that every published port uses
loopback; that the database publishes nothing and mounts no host path; and that
no mail data directory is mounted. The client's frontend network MUST NOT be
internal so its loopback port can be published, and the data network MUST remain
internal.

#### Scenario: A new service is added to the application project

- **WHEN** a pull request adds a service beyond api, db, and web
- **THEN** the application compose gate fails until the declaration is updated

#### Scenario: The database becomes reachable from the host

- **WHEN** a pull request publishes a database port or bind-mounts its data
- **THEN** the application compose gate fails and names the broken invariant

### Requirement: The application deploy builds and verifies the client

The `Deploy application` workflow MUST build a web image tagged for the commit,
write its tag to the generated `.env`, and verify that the client is running
after the deploy. It MUST still verify that the mail container's identity, start
time, and image are unchanged.

#### Scenario: A commit is deployed

- **WHEN** the application deploy workflow finishes
- **THEN** both the API and web images carry the commit tag and the run summary
  names the client's loopback address

#### Scenario: The mail stack changes during an application deploy

- **WHEN** the mail container's identity, start time, or image differs after the
  deploy
- **THEN** the workflow fails and reports the before and after values

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
