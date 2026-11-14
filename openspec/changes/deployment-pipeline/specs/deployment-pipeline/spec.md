# Deployment pipeline

## Purpose

Defines the intended behaviour of the mail infrastructure continuous-delivery
pipeline: what must be true on every pull request, on every push to `main`, and
on the homelab host after a deployment.

## ADDED Requirements

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
