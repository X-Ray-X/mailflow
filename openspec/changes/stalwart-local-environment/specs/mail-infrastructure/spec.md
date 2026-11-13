# Spec Delta

## Purpose

Provides a reproducible, persistent, locally hosted mail server that Mailflow's clients and services rely on as the source of truth for mailboxes.

## ADDED Requirements

### Requirement: Reproducible deployment
The mail infrastructure SHALL be started from version-controlled configuration by a single documented command, with the mail server release pinned. A fresh checkout on a supported host SHALL produce an equivalent running environment without manual configuration edits.

#### Scenario: Fresh host reproduces the environment
- **WHEN** the documented start command runs on a host with the repository and container runtime available
- **THEN** the mail server starts with the configured domain and accounts, and completes client connections

#### Scenario: Pinned release is the running release
- **WHEN** the stack is running
- **THEN** the reported mail server version matches the version declared in the repository

### Requirement: Persistent mailbox state
Mailbox contents, account data, and server configuration SHALL survive container restart, container recreation, and host reboot.

#### Scenario: Restart preserves mail
- **WHEN** the stack is stopped and started again after messages have been delivered
- **THEN** all messages and their flags remain available to clients

#### Scenario: Recreation preserves mail
- **WHEN** the server container is recreated using the same pinned release and configuration
- **THEN** all messages, flags, and accounts remain available

### Requirement: Local-only domain
The mail server SHALL serve exactly one non-routable local domain and SHALL deliver mail between accounts on that domain without external DNS resolution or internet connectivity.

#### Scenario: Internal delivery without internet
- **WHEN** one account sends a message to another account on the local domain while the host has no internet access
- **THEN** the message is delivered to the recipient's mailbox

#### Scenario: No external egress
- **WHEN** a message is addressed to a recipient outside the local domain
- **THEN** the submission is rejected and no attempt is made to deliver it externally

### Requirement: Account provisioning
At least two accounts SHALL exist for demonstration, each able to authenticate independently. Account credentials SHALL NOT appear in version-controlled files.

#### Scenario: Accounts authenticate independently
- **WHEN** each provisioned account authenticates with its own credentials
- **THEN** authentication succeeds and each account sees only its own mailbox

#### Scenario: No credentials in the repository
- **WHEN** tracked files are inspected
- **THEN** no account passwords are present

### Requirement: Verified client TLS
Clients SHALL connect over TLS and validate the server certificate without disabling certificate verification.

#### Scenario: Client validates the certificate
- **WHEN** a client connects to the mail services using TLS
- **THEN** it validates the certificate successfully with verification enabled

#### Scenario: Advertised hostname matches the certificate
- **WHEN** a client connects using the hostname documented for local network or tailnet access
- **THEN** that hostname is covered by the presented certificate

### Requirement: Documented and minimal network exposure
Published ports and their purposes SHALL be documented, and only ports required for client access and administration SHALL be published.

#### Scenario: Port inventory matches documentation
- **WHEN** the stack is running
- **THEN** the published ports match the documented list

### Requirement: No open relay
The server SHALL require authentication for submission and SHALL NOT relay messages for unauthenticated clients.

#### Scenario: Unauthenticated submission is refused
- **WHEN** an unauthenticated client attempts to submit a message for delivery
- **THEN** the server refuses the submission

### Requirement: Isolated lifecycle from the application stack
The mail infrastructure SHALL run as a deployment unit separate from the Mailflow application so that application deployments cannot recreate, upgrade, or reset mail server containers or volumes.

#### Scenario: Application deployment leaves mail untouched
- **WHEN** the application stack is deployed, restarted, or rolled back
- **THEN** the mail server container, configuration, and volumes are unchanged

### Requirement: Target architecture support
The mail server SHALL run natively on arm64, the target homelab architecture.

#### Scenario: Runs on the target host
- **WHEN** the stack starts on the arm64 homelab host
- **THEN** the mail server runs without CPU emulation
