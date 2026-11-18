# Spec Delta

## ADDED Requirements

### Requirement: Mailbox creation

The service SHALL create a top-level custom mailbox with the caller-supplied
name and return its Mailflow identifier. The name SHALL be trimmed, non-empty,
at most 255 bytes, free of control characters and `/`, and unique among the
caller's mailboxes ignoring case. A name already in use SHALL be refused with
`already_exists` and SHALL NOT create a second mailbox.

#### Scenario: A mailbox is created

- **WHEN** a caller creates a mailbox named `Projects`
- **THEN** the service answers with a Mailflow identifier, and the mailbox list
  reports it with role `custom`

#### Scenario: A standard client sees the new mailbox

- **WHEN** a mailbox is created through the service and a standard IMAP client
  lists the account's mailboxes
- **THEN** the client sees the mailbox without any extra subscription step

#### Scenario: A duplicate name is refused

- **WHEN** a caller creates a name already used by one of its mailboxes,
  ignoring case
- **THEN** the service answers `already_exists` and the account still holds one
  mailbox with that name

#### Scenario: An invalid name is refused

- **WHEN** a create names an empty or whitespace-only name, more than 255 bytes,
  a control character, or a `/`
- **THEN** the service answers `invalid_request` and creates nothing

#### Scenario: A retried create cannot duplicate

- **WHEN** a create is retried with the same name after an unknown outcome
- **THEN** at most one mailbox with that name exists, and the retry answers
  `already_exists`

### Requirement: Mailbox rename

The service SHALL rename a custom mailbox to the caller-supplied name, applying
the creation name rules and uniqueness. A standard mailbox SHALL NOT be renamed.
Renaming a mailbox to its current name SHALL change nothing and succeed. An
unknown or foreign mailbox SHALL answer `not_found`.

#### Scenario: A custom mailbox is renamed

- **WHEN** a caller renames `Projects` to `Work`
- **THEN** the mailbox list reports `Work` under the same identifier

#### Scenario: A standard mailbox is protected

- **WHEN** a caller renames an inbox, sent, drafts, trash, or junk mailbox
- **THEN** the service answers `invalid_request` and the name is unchanged

#### Scenario: A duplicate rename is refused

- **WHEN** a rename would collide with another mailbox's name, ignoring case
- **THEN** the service answers `already_exists` and nothing changes

#### Scenario: Renaming to the same name is a no-op

- **WHEN** a caller renames a mailbox to the name it already has
- **THEN** the service reports success and nothing changes

#### Scenario: A foreign mailbox is not renamed

- **WHEN** a caller renames an identifier issued to another account
- **THEN** the service answers `not_found` and changes nothing

### Requirement: Mailbox deletion

The service SHALL delete a custom mailbox that holds no messages and no child
mailboxes. It SHALL refuse a standard mailbox, a mailbox holding messages, and a
mailbox with children, so no message is removed as a side effect. A repeated
delete SHALL answer `not_found` and change nothing.

#### Scenario: An empty custom mailbox is deleted

- **WHEN** a caller deletes an empty custom mailbox
- **THEN** the service reports success and the mailbox list no longer contains it

#### Scenario: A standard mailbox is protected

- **WHEN** a caller deletes an inbox, sent, drafts, trash, or junk mailbox
- **THEN** the service answers `invalid_request` and the mailbox remains

#### Scenario: A mailbox holding messages is not deleted

- **WHEN** a caller deletes a custom mailbox that still holds messages
- **THEN** the service answers `mailbox_not_empty` and every message remains

#### Scenario: A mailbox with children is not deleted

- **WHEN** a caller deletes a mailbox that has child mailboxes
- **THEN** the service answers `mailbox_not_empty` and the children remain

#### Scenario: A repeated delete changes nothing

- **WHEN** a caller repeats a delete for a mailbox that is already gone
- **THEN** the service answers `not_found` and no other mailbox changes

### Requirement: Message moves

A message patch SHALL accept a `mailboxId` naming the caller's target mailbox.
After the request the message SHALL be in exactly that mailbox, with its flags
and attachments unchanged. Moving a message that is already in exactly that
mailbox SHALL change nothing and succeed. The target SHALL be a mailbox of the
caller.

#### Scenario: A message is moved

- **WHEN** a caller patches a message's `mailboxId` to `Projects`
- **THEN** listing `Projects` shows the message and listing the source mailbox
  does not

#### Scenario: Flags and attachments survive a move

- **WHEN** a starred message with an attachment is moved
- **THEN** it is still starred and still reports an attachment

#### Scenario: A move and a flag change share one request

- **WHEN** a patch names both `mailboxId` and `read`
- **THEN** the message is in the target mailbox and is read

#### Scenario: Moving to the current mailbox is a no-op

- **WHEN** a caller patches a message's `mailboxId` to the mailbox it is
  already only in
- **THEN** the service reports success and the message's membership is unchanged

#### Scenario: A foreign or unknown target is refused

- **WHEN** a patch names a mailbox identifier that is unknown or was issued to
  another account
- **THEN** the service answers `not_found` and the message stays where it was

#### Scenario: A patch names nothing

- **WHEN** a message patch has no `read`, `starred`, or `mailboxId`
- **THEN** the service answers `invalid_request` and changes nothing

### Requirement: Message soft deletion

The service SHALL delete a message by moving it to the caller's trash mailbox.
It SHALL NOT permanently erase the message. Deleting a message whose only
mailbox is trash SHALL change nothing and succeed. An unknown or foreign message
SHALL answer `not_found`.

#### Scenario: Deleting a message moves it to trash

- **WHEN** a caller deletes a message from a mailbox
- **THEN** it no longer appears in that mailbox and appears in trash

#### Scenario: Deleting is repeatable

- **WHEN** a caller deletes a message whose only mailbox is trash
- **THEN** the service reports success and the message is still in trash

#### Scenario: A deleted message can be restored

- **WHEN** a caller moves a message from trash to another mailbox
- **THEN** it appears in that mailbox

#### Scenario: A foreign message is not deleted

- **WHEN** a caller deletes an identifier issued to another account
- **THEN** the service answers `not_found` and nothing changes
