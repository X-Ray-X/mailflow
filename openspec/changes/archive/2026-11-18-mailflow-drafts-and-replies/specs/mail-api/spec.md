# Spec Delta

## ADDED Requirements

### Requirement: Draft creation

The service SHALL save a caller-supplied plain-text draft in the caller's drafts
mailbox and return its Mailflow identifier. Recipients, subject, and body SHALL
be optional. Every recipient SHALL be an address in the caller's mail domain, and
the request SHALL be bounded in recipient count and body size. Draft creation
SHALL NOT require an idempotency key.

#### Scenario: A draft is saved

- **WHEN** a caller saves a draft with a recipient, a subject, and a body
- **THEN** the service answers with a Mailflow identifier, and the drafts mailbox
  lists the message with that identifier

#### Scenario: An incomplete draft is allowed

- **WHEN** a caller saves a draft with no recipient, no subject, and no body
- **THEN** the draft is saved and listed in the drafts mailbox

#### Scenario: A recipient outside the domain is refused

- **WHEN** a draft names an address outside the caller's mail domain
- **THEN** the service answers `invalid_request` and saves nothing

#### Scenario: Draft input is bounded

- **WHEN** a draft exceeds the recipient count or body size the contract allows
- **THEN** the service answers `invalid_request` and saves nothing

### Requirement: Draft submission

The service SHALL submit an existing draft by moving it from the caller's drafts
mailbox to sent, keeping its identifier, and SHALL require an `Idempotency-Key`.
A repeated key with the same request SHALL return the original result without
submitting again. A message that is not a draft, or a draft with no recipients,
SHALL be refused with `invalid_request` and SHALL remain in drafts. An unknown or
foreign identifier SHALL answer `not_found`.

#### Scenario: A draft is sent

- **WHEN** a caller submits a saved draft
- **THEN** the drafts mailbox no longer lists it, the sent mailbox does, and its
  identifier is unchanged

#### Scenario: A replay does not send twice

- **WHEN** the same draft submission is repeated with the same idempotency key
- **THEN** the original identifier is returned and no second message is submitted

#### Scenario: A key reused for a different request is refused

- **WHEN** a draft submission reuses a key already claimed by another request
- **THEN** the service answers `conflict` and submits nothing

#### Scenario: A message that is not a draft is refused

- **WHEN** a caller submits a message that is not in the drafts mailbox or no
  longer carries the draft state
- **THEN** the service answers `invalid_request` and the message stays where it is

#### Scenario: A draft with no recipients is refused

- **WHEN** a caller submits a draft that has no recipient
- **THEN** the service answers `invalid_request` and the draft remains in drafts

#### Scenario: A declined submission keeps the draft

- **WHEN** the mail server declines the submission
- **THEN** the service answers `send_rejected` and the draft remains in drafts

#### Scenario: A foreign draft is refused

- **WHEN** a caller submits an identifier issued to another account
- **THEN** the service answers `not_found` and nothing changes

### Requirement: Reply composition

The service SHALL create a draft reply to a message the caller can read. The
reply's sender SHALL be the caller, its recipient SHALL be the original sender,
its subject SHALL be the original subject prefixed once with `Re: `, and it SHALL
carry the original message's threading so a standard client groups the reply with
the conversation. The body SHALL be the caller-supplied text. The reply SHALL be
saved in drafts and returned as a Mailflow identifier.

#### Scenario: A reply is composed

- **WHEN** a caller replies to a message
- **THEN** a draft exists in the drafts mailbox addressed to the original sender

#### Scenario: The subject is prefixed once

- **WHEN** the original subject already starts with `Re: `
- **THEN** the reply's subject is the original subject unchanged

#### Scenario: Threading is carried

- **WHEN** a standard client reads the reply
- **THEN** it groups the reply with the original message's conversation

#### Scenario: The reply body is preserved

- **WHEN** a caller replies with a body
- **THEN** reading the draft returns that body unchanged

#### Scenario: A foreign message is not replied to

- **WHEN** a caller replies to an identifier issued to another account
- **THEN** the service answers `not_found` and creates nothing

#### Scenario: Reply input is bounded

- **WHEN** a reply body exceeds the size the contract allows
- **THEN** the service answers `invalid_request` and creates nothing
