# Spec Delta

## Purpose

Publishes Mailflow's own HTTP contract for reading mailboxes and messages, so consumers depend on Mailflow's vocabulary and identifiers instead of the mail server's protocols.

## ADDED Requirements

### Requirement: Versioned read contract
The service SHALL publish its read contract under a version path. Within a version, responses SHALL only gain fields and endpoints: an existing field SHALL NOT change meaning or disappear.

#### Scenario: A v1 request keeps working
- **WHEN** a client built against v1 repeats a request that was valid at v1
- **THEN** the request succeeds and every field the client read is still present with the same meaning

### Requirement: Mailbox listing
The service SHALL list the caller's mailboxes, each with a Mailflow identifier, a display name, a Mailflow role, and unread and total message counts.

#### Scenario: The caller lists mailboxes
- **WHEN** an authenticated caller requests the mailbox list
- **THEN** each mailbox is returned with an identifier, a name, a role, and its counts

#### Scenario: Standard mailboxes are identifiable by role
- **WHEN** the mailbox list includes the standard mailboxes
- **THEN** inbox, sent, drafts, trash, and junk are distinguishable by role without relying on display names

### Requirement: Message listing
The service SHALL list the messages in one mailbox, newest first, with a Mailflow identifier, subject, sender, received time, read state, starred state, and whether the message has attachments.

#### Scenario: Messages are listed newest first
- **WHEN** a caller requests the messages of a mailbox holding several messages
- **THEN** the messages are returned in descending received-time order with those fields

#### Scenario: Unknown mailbox
- **WHEN** a caller requests messages for a mailbox that does not exist
- **THEN** the service returns a not-found error and no messages

### Requirement: Bounded pagination
Message listing SHALL be paginated with a maximum page size and SHALL report whether further messages exist, so a large mailbox cannot be returned in one response.

#### Scenario: Page size is bounded
- **WHEN** a caller asks for more messages than the maximum page size
- **THEN** the service returns at most the maximum and reports that more messages exist

#### Scenario: Following a page does not repeat messages
- **WHEN** a caller requests the next page of the same mailbox
- **THEN** no message already returned on an earlier page is returned again

#### Scenario: A page that reports more messages carries a cursor
- **WHEN** a response reports that further messages exist
- **THEN** it includes a cursor that fetches the next page, even if every message on that page was removed before it could be returned

### Requirement: Message content
The service SHALL return one message's subject, sender, recipients, received time, read and starred state, and a text body, without exposing raw MIME or vendor properties.

#### Scenario: Fetching a message
- **WHEN** the caller fetches a message it is allowed to read
- **THEN** the response contains the subject, sender, recipients, received time, flags, and a text body

### Requirement: Mailflow identifiers and vocabulary
Every public payload SHALL use Mailflow identifiers and Mailflow terms. Identifiers SHALL be opaque values that clients do not parse, SHALL be scoped to the account they were issued to, and payloads SHALL NOT contain vendor object types, method names, error codes, protocol flags, or the mail server's hostname.

#### Scenario: Identifiers are opaque and Mailflow-owned
- **WHEN** any response contains an identifier
- **THEN** it is a Mailflow-prefixed value that differs from the mail server's identifier and from the account name

#### Scenario: An identifier is only valid for the account it was issued to
- **WHEN** an identifier issued to one account is presented with another account's credentials
- **THEN** the service answers `not_found` for a mailbox or message identifier, and `invalid_cursor` for a cursor, without resolving it against the caller's own objects

#### Scenario: No vendor vocabulary in a payload
- **WHEN** any read response is inspected
- **THEN** it contains no vendor capability URN, method name, IMAP flag, or mail server error string

### Requirement: Bounded upstream failure
When the mail server is unreachable or slow, the service SHALL fail within a configured timeout and return a Mailflow error rather than waiting indefinitely or surfacing vendor detail.

#### Scenario: Mail server unreachable
- **WHEN** the mail server cannot be reached
- **THEN** the request fails with a Mailflow service-unavailable error within the configured timeout

#### Scenario: Mail server times out
- **WHEN** the mail server accepts a connection but does not answer in time
- **THEN** the request fails with the same Mailflow error and no vendor detail

### Requirement: One error shape
Every error response SHALL share one shape: a stable Mailflow error code and a human-readable message, with no stack trace, vendor code, or credential.

#### Scenario: Error responses are uniform
- **WHEN** any request fails
- **THEN** the body contains a stable code and a message and nothing else
