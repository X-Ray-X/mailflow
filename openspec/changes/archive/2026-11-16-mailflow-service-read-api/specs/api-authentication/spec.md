# Spec Delta

## Purpose

Defines how a caller proves which mailbox it may read, and keeps the mail server as the only authority for credentials and account identity.

## ADDED Requirements

### Requirement: The mail server validates credentials
The service SHALL authenticate every request against the mail server's authentication. It SHALL NOT keep its own user store, password database, or cached credentials.

#### Scenario: Valid credentials are served
- **WHEN** a caller presents credentials that the mail server accepts
- **THEN** the request is served for the account those credentials belong to

#### Scenario: Invalid credentials are refused
- **WHEN** a caller presents credentials the mail server rejects
- **THEN** the service returns an authentication error and returns no mailbox or message data

### Requirement: Responses are scoped to the caller
A caller SHALL read only mailboxes and messages belonging to the account it authenticated as. The account SHALL be taken from the authenticated identity, never from a request parameter.

#### Scenario: Another account's message is not readable
- **WHEN** a caller requests an identifier that belongs to a different account
- **THEN** the service returns not-found and does not reveal whether the object exists

#### Scenario: A request cannot name another account
- **WHEN** a request supplies another account's address or identifier
- **THEN** the service ignores or rejects it and still reads only the caller's own mail

### Requirement: Authentication failures leak nothing
Authentication failures SHALL be reported as one Mailflow authentication error, without vendor error text and without revealing which part of the credential was wrong.

#### Scenario: Failure detail is withheld
- **WHEN** credentials are rejected
- **THEN** the response is an authentication error with a generic message and no vendor text

### Requirement: Credentials never reach logs
The service SHALL NOT write credentials, authorization headers, or tokens to logs, traces, or error responses.

#### Scenario: Logs hold no credentials
- **WHEN** requests carrying credentials are served
- **THEN** the service's logs contain no password, authorization header value, or token
