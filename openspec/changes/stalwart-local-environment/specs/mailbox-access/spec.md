# Spec Delta

## Purpose

Defines how standard email clients authenticate to and use Mailflow mailboxes over IMAP and SMTP, and what behavior they can rely on when a mailbox is accessed from more than one client.

## ADDED Requirements

### Requirement: Standard client access
Mailboxes SHALL be accessible from unmodified standard email clients using IMAP for mailbox operations and SMTP for submission.

#### Scenario: Client completes account setup
- **WHEN** a user configures a standard client with the documented host, ports, and account credentials
- **THEN** the client connects, lists the account's mailboxes, and displays existing messages

#### Scenario: Sending succeeds
- **WHEN** a user submits a message to another local account from a standard client
- **THEN** the server accepts the message and reports successful submission to the client

### Requirement: Message body and attachment fidelity
Delivered messages SHALL preserve the sender's content, including MIME structure, HTML and plain-text alternatives, and attachments.

#### Scenario: Attachment survives delivery
- **WHEN** a message with an attachment is sent to another local account
- **THEN** the recipient's client can download the attachment with byte-identical content

### Requirement: Mailbox organization
Accounts SHALL have recognizable standard mailboxes, including an inbox and a record of sent messages, and clients SHALL be able to create, rename, and delete additional mailboxes.

#### Scenario: Custom mailbox round-trips
- **WHEN** a user creates a mailbox in one client, moves a message into it, and connects from a second client
- **THEN** the second client sees the mailbox and the moved message

### Requirement: Durable flags
Message flags that users can set in a client, including read state and flagged state, SHALL persist and SHALL be observable from other clients connected to the same account.

#### Scenario: Read state is shared between clients
- **WHEN** a message is marked read in one client
- **THEN** another client connected to the same account observes the message as read

#### Scenario: Flag state survives reconnect
- **WHEN** a client sets or clears a flag and later reconnects
- **THEN** the flag state is unchanged

### Requirement: Sent messages are recorded
A sent record SHALL exist for each message an account submits from a standard client, and it SHALL be visible from other clients connected to the same account. The submitting client is responsible for the record, as it is against any IMAP server: standard clients store their sent copy in the account's sent mailbox over IMAP, and the server SHALL accept and retain that copy. The server does not create the copy itself.

#### Scenario: Submitted message appears in sent mailbox
- **WHEN** a standard client submits a message and stores its sent copy in the sent mailbox
- **THEN** the message appears in that account's sent mailbox

#### Scenario: Sent record is visible from a second client
- **WHEN** a second client connects to the same account after a submission
- **THEN** it can see the submitted message in the sent mailbox

### Requirement: Local delivery is prompt and duplicate-free
A message addressed to an account on the local domain SHALL be delivered to that account's inbox once, without requiring a manual refresh or a restart of any component.

#### Scenario: Delivery is immediate
- **WHEN** a message is sent to another local account
- **THEN** the recipient's connected client receives it without restarting the server or client

#### Scenario: No duplicate copies
- **WHEN** the same message is delivered to a recipient
- **THEN** exactly one copy appears in the recipient's inbox

### Requirement: Client version compatibility
The documented setup SHALL be verified with at least two distinct client implementations, one of which SHALL be a desktop email application. A mobile client SHALL be documented but is not required for this change.

#### Scenario: Desktop application works
- **WHEN** a desktop email application is configured with the documented host, ports, and credentials
- **THEN** it connects with certificate verification enabled, lists the account's mailboxes, and its submitted message is retained in the sent mailbox

#### Scenario: A second implementation reads the same account
- **WHEN** a second, distinct client implementation connects to the same account
- **THEN** it authenticates and lists the same standard mailboxes with certificate verification enabled

#### Scenario: Mobile guidance exists
- **WHEN** a user wants to configure a mobile client over the tailnet
- **THEN** the documentation covers trusting the internal CA and resolving the hostname on that platform
