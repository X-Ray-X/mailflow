# Spec Delta

## ADDED Requirements

### Requirement: Attachment metadata listing

The service SHALL list the attachments of a message the caller can read, giving
each attachment's filename, content type, size in bytes, and a position that
addresses it. The list SHALL be empty for a message with no attachments, and its
order SHALL be stable for the life of the message.

#### Scenario: A message with an attachment is listed

- **WHEN** a caller lists the attachments of a message that has one
- **THEN** the service answers `200` with that attachment's filename, content
  type, size, and position

#### Scenario: A message without attachments is listed

- **WHEN** a caller lists the attachments of a plain-text message
- **THEN** the service answers `200` with an empty attachment list

#### Scenario: A foreign message is refused

- **WHEN** a caller lists attachments of an identifier issued to another
  account
- **THEN** the service answers `not_found` and reveals nothing about the message

### Requirement: Attachment download

The service SHALL return the bytes of one addressed attachment with the
attachment's content type, a download disposition naming its file, and no
intermediary caching. A download SHALL be bounded in size; exceeding the bound
SHALL fail rather than stream an unbounded response.

#### Scenario: An attachment is downloaded

- **WHEN** a caller requests an attachment position that exists on a readable
  message
- **THEN** the service answers `200` with the attachment's bytes, content type,
  and download filename

#### Scenario: An unknown position is refused

- **WHEN** a caller requests a position the message does not have
- **THEN** the service answers `not_found` and creates no download

#### Scenario: A malformed position is refused

- **WHEN** a caller requests a position that is not a non-negative integer
- **THEN** the service answers `invalid_request` and creates no download

#### Scenario: A download cannot be fetched

- **WHEN** the mail server cannot supply the attachment bytes or exceeds the
  size bound
- **THEN** the service answers `service_unavailable` and returns no partial
  attachment

### Requirement: Attachment identifiers stay in Mailflow vocabulary

Positions and message identifiers SHALL be the only addressing values a caller
sees. The service SHALL NOT expose mail server blob identifiers, download URLs,
storage paths, or vendor object types in any attachment response.

#### Scenario: A response carries no vendor address

- **WHEN** a caller reads attachment metadata or downloads an attachment
- **THEN** no response field or header value contains a mail server blob
  identifier, download URL, or vendor object type

#### Scenario: A foreign attachment cannot be named

- **WHEN** a caller uses a message identifier from another account to address
  an attachment
- **THEN** the service answers `not_found` before contacting the mail server for
  the attachment bytes
