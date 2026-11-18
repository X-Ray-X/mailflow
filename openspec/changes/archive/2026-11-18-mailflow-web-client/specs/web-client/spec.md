# Spec Delta

## Purpose

The first browser client for the published Mailflow contract. It demonstrates
that a client can perform everyday mail work through the HTTP API alone, without
knowing the mail server's protocols or identifiers.

## ADDED Requirements

### Requirement: The client uses only the published contract

The client SHALL make all mail requests through the versioned `/v1` API and
SHALL NOT speak IMAP, JMAP, or SMTP, read mail storage, or construct mail server
identifiers. No response field from the mail server SHALL be rendered or stored
by the client; only documented Mailflow fields SHALL appear in the UI model.

#### Scenario: A user reads and sends mail in the browser

- **WHEN** a user signs in, browses, searches, and sends a message
- **THEN** every request goes to the published API and no mail protocol is used
  by the client

#### Scenario: A vendor field appears in a response

- **WHEN** a response contains a field the contract does not document
- **THEN** the client ignores it and does not display or persist it

### Requirement: Credentials stay in memory

The client SHALL ask the user for mail credentials and hold them only in memory
for the current session. It SHALL NOT write them to browser storage, place them
in URLs, or include them in telemetry. Every request SHALL carry them as HTTP
Basic authentication.

#### Scenario: A user reloads the page

- **WHEN** the browser reloads after a successful session
- **THEN** the client asks for credentials again instead of restoring a password
  from storage

#### Scenario: A request is made

- **WHEN** the client calls a protected endpoint
- **THEN** the credentials travel in the Authorization header, never in the URL

### Requirement: Core mail flows

The client SHALL let a signed-in user list mailboxes and message pages, search
by text, read a message and its text body, list and download attachments, send a
plain-text message, save and send a draft, reply to a message, and patch read or
starred state, move a message, and delete it to trash.

#### Scenario: A user finds and reads a message

- **WHEN** a user searches for a phrase and opens a result
- **THEN** the client shows the message's subject, sender, recipients, and text
  body, and lists its attachments

#### Scenario: A user sends a message

- **WHEN** a user submits a compose form with a recipient, subject, and body
- **THEN** the client sends one request with a fresh idempotency key and shows
  the returned message identifier

#### Scenario: A user works on a draft

- **WHEN** a user saves a draft and later sends it
- **THEN** the client uses the draft identifier for submission and shows the
  same identifier after the message moves to sent

#### Scenario: A user replies to a message

- **WHEN** a user submits reply text
- **THEN** the client creates a reply draft and shows it in the drafts mailbox

### Requirement: Failures are reported honestly

The client SHALL surface the contract's error code and message for a failed
request and SHALL NOT claim an action succeeded when the response says it did
not. A replayed submission SHALL be shown as the original result, not as a new
message.

#### Scenario: A request fails

- **WHEN** the API answers with an error code
- **THEN** the client displays that code and message and leaves the current view
  unchanged

#### Scenario: A send is replayed

- **WHEN** the API marks a response as a replay
- **THEN** the client reports that the original result was returned

### Requirement: The client is served with the application

The client SHALL be static assets served by the application deployment unit over
loopback, and SHALL reach the API through a same-origin proxy. It SHALL NOT open
a mail port or change the mail deployment unit.

#### Scenario: A user opens the client

- **WHEN** a user visits the client's loopback address
- **THEN** the static application loads and its API calls use the same origin
