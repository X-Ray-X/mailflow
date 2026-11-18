# Spec Delta

## ADDED Requirements

### Requirement: Text search

The service SHALL answer a caller-supplied text query with message summaries
from the caller's mail. The query SHALL be required and non-empty, and the
result SHALL contain only messages the caller can read. An optional mailbox
identifier SHALL restrict results to that mailbox; an unknown, foreign, or
malformed mailbox identifier SHALL answer `not_found`.

#### Scenario: A caller searches their mail

- **WHEN** a caller requests a non-empty text query
- **THEN** the service answers `200` with matching message summaries in the
  same shape as a message listing

#### Scenario: A mailbox restricts the search

- **WHEN** a caller supplies one of their mailbox identifiers
- **THEN** only messages in that mailbox appear in the results

#### Scenario: An empty query is refused

- **WHEN** a caller omits the query or sends only whitespace
- **THEN** the service answers `invalid_request` and performs no search

#### Scenario: A foreign mailbox is refused

- **WHEN** a caller supplies a mailbox identifier issued to another account
- **THEN** the service answers `not_found` and reveals nothing about the mailbox

### Requirement: Bounded search pages

A search SHALL use the same page size and cursor rules as message listing.
`hasMore` SHALL always be present and `nextCursor` SHALL appear when another
page exists. A cursor that no longer resolves SHALL answer `invalid_cursor`.

#### Scenario: A search returns one page

- **WHEN** a caller searches without a limit
- **THEN** the service answers with at most the configured default page size
  and an accurate `hasMore`

#### Scenario: A search is paged

- **WHEN** a caller follows `nextCursor` from a previous search
- **THEN** the next page contains no message already returned

#### Scenario: A stale search cursor is refused

- **WHEN** a caller supplies a cursor whose message no longer exists
- **THEN** the service answers `invalid_cursor` and the caller restarts the search

### Requirement: Search stays in Mailflow vocabulary

The search response SHALL contain only message summaries. The request SHALL
accept only the contract's own parameters, and no response SHALL carry a mail
server filter name, query state, blob identifier, or other vendor value.

#### Scenario: An unknown request parameter is refused

- **WHEN** a caller supplies a parameter the contract does not define
- **THEN** the service answers `invalid_request` and performs no search

#### Scenario: A response carries no vendor value

- **WHEN** a caller reads a search result
- **THEN** the response contains no mail server filter or state value
