# mail-api Specification

## Purpose

Publishes Mailflow's own HTTP contract for reading mailboxes and messages and for sending and updating messages, so consumers depend on Mailflow's vocabulary and identifiers instead of the mail server's protocols.

## Requirements

### Requirement: Versioned contract
The service SHALL publish its contract under a version path, for reads and for writes. Within a version, responses and endpoints SHALL only gain fields: an existing field SHALL NOT change meaning or disappear, and an existing request SHALL keep being accepted.

#### Scenario: A v1 request keeps working

- **WHEN** a client built against v1 repeats a request that was valid at v1
- **THEN** the request succeeds and every field the client read is still present with the same meaning

#### Scenario: A new write does not disturb a v1 reader

- **WHEN** a client uses the send or flag endpoints and another client reads the
  same message with requests that were valid before they existed
- **THEN** the read responses keep their previous shape and meaning

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

### Requirement: Message sending

The service SHALL accept a plain-text message for sending on behalf of the
authenticated account and SHALL return the Mailflow identifier of the sent
message. The sender SHALL be resolved from the authenticated account, never from
a value the caller supplies, and the caller SHALL NOT be able to send as another
account.

#### Scenario: A message is sent

- **WHEN** an authenticated caller sends a message to a local account with a
  subject and a text body
- **THEN** the service answers with the Mailflow identifier of the sent message

#### Scenario: The sent message is readable afterwards

- **WHEN** the sender fetches the identifier returned for a sent message
- **THEN** it reports the subject, the recipient, and a read state

#### Scenario: The sent message appears in the sent mailbox

- **WHEN** the sender lists the mailbox whose role is `sent`
- **THEN** the sent message is listed there

#### Scenario: The caller cannot choose another sender

- **WHEN** a send request names a sender other than the authenticated account
- **THEN** the service refuses it and sends nothing

### Requirement: Sending is deduplicated

Every send SHALL require a caller-supplied idempotency key. Within the key's
retention period, a repeated key with an identical request SHALL return the
original result without sending again; the same key with a different request
SHALL be refused; a duplicate that arrives while the first is still being
processed SHALL be refused as in progress. Keys SHALL be scoped to the calling
account.

#### Scenario: A retry does not send twice

- **WHEN** a caller repeats a send with the same key and the same request
- **THEN** the service returns the original message identifier, marks the
  response as a replay, and the recipient receives one message

#### Scenario: A key reused for a different request is refused

- **WHEN** a caller uses an existing key with a different recipient or body
- **THEN** the service answers `conflict` and sends nothing

#### Scenario: A duplicate while the first is in flight is refused

- **WHEN** a second send with the same key arrives before the first completes
- **THEN** the service answers `in_progress` and sends nothing

#### Scenario: Keys are per account

- **WHEN** two accounts use the same key value for their own sends
- **THEN** each send is processed independently

#### Scenario: A missing key is refused

- **WHEN** a send request carries no idempotency key
- **THEN** the service answers `invalid_request` and sends nothing

#### Scenario: Retention is bounded

- **WHEN** a key is older than the configured retention period
- **THEN** it no longer resolves an earlier send

### Requirement: Message flag updates

The service SHALL set a message's read and starred state to the values the
caller states and SHALL leave the message's other attributes unchanged. A flag
update SHALL name at least one of the two states, and a request naming an
unknown field SHALL be refused.

#### Scenario: Marking a message read

- **WHEN** a caller sets a message to read
- **THEN** subsequent reads report it read, and setting it read again reports the
  same result

#### Scenario: Starring a message

- **WHEN** a caller sets a message to starred
- **THEN** subsequent reads report it starred, and setting it unstarred clears it

#### Scenario: Other attributes survive a flag change

- **WHEN** a message with an attachment has its read state changed
- **THEN** it still reports that it has an attachment, and its body is unchanged

#### Scenario: An empty or unknown flag request is refused

- **WHEN** a flag request has neither `read` nor `starred`, or names another field
- **THEN** the service answers `invalid_request` and changes nothing

#### Scenario: A foreign message is not updated

- **WHEN** a caller sends a flag request for an identifier issued to another
  account
- **THEN** the service answers `not_found` and changes nothing

### Requirement: Send input is bounded

A send SHALL be bounded in recipient count and body size and SHALL refuse a
recipient outside the caller's mail domain, so the service cannot become a relay
to arbitrary addresses. A refused send SHALL NOT submit anything.

#### Scenario: A recipient outside the domain is refused

- **WHEN** a caller sends to an address outside the local mail domain
- **THEN** the service answers `invalid_request` and nothing is submitted

#### Scenario: Too many recipients

- **WHEN** a send names more recipients than the configured maximum
- **THEN** the service answers `invalid_request` and nothing is submitted

#### Scenario: The body is too large

- **WHEN** a send body exceeds the configured maximum
- **THEN** the service answers `invalid_request` and nothing is submitted

### Requirement: A refused send is reported honestly

When the mail server declines to accept a message, the service SHALL answer with
a Mailflow error that says the message was not accepted, SHALL NOT report it as
sent, and SHALL NOT retry by itself. The response SHALL carry no vendor error
text.

#### Scenario: The mail server declines the message

- **WHEN** the mail server refuses a submission
- **THEN** the service answers `send_rejected` and no sent message is reported

#### Scenario: A declined send is not retried

- **WHEN** a submission is declined and the caller does not retry
- **THEN** exactly one attempt was made

### Requirement: Reads survive a record-store outage

When the application database is unavailable after the service has started, the
service SHALL keep serving reads and flag updates, and SHALL refuse sends with
`service_unavailable` rather than submitting without a deduplication record.

#### Scenario: Sending fails closed

- **WHEN** the record store is unavailable and a caller sends a message
- **THEN** the service answers `service_unavailable` and nothing is submitted

#### Scenario: Reading still works

- **WHEN** the record store is unavailable
- **THEN** mailbox and message reads still answer successfully

### Requirement: Write failures use stable Mailflow codes

Write failures SHALL use the contract's single error shape with stable Mailflow
codes. A refused send SHALL use `send_rejected`; a key reused for a different
request SHALL use `conflict`; a duplicate still in flight SHALL use `in_progress`.

#### Scenario: A write error carries only a Mailflow code

- **WHEN** any write request fails
- **THEN** the body contains a stable Mailflow code and a message, and no vendor
  code, stack trace, or credential
