# Design

## Context

See `proposal.md` for motivation. The contract reads mail, sends, patches flags,
moves and soft-deletes messages, and manages mailboxes. The adapter
(`app/internal/mail`) translates between Mailflow types and JMAP, the HTTP layer
publishes only Mailflow vocabulary, and every identifier encodes its account
(`app/internal/ids`). Sending already claims an idempotency key in PostgreSQL
before calling the mail server.

The design rests on a probe of the pinned release
(`ghcr.io/stalwartlabs/stalwart:v1.1.0`, linux/arm64) on 2026-11-18, recorded in
`docs/integration-surface.md` by task 1.1. Observed with an unprivileged account:

- `Email/set` create makes a draft when the create names the drafts mailbox and
  sets `$draft`. Recipients, subject, and body may be empty; an empty draft was
  accepted.
- `Email/set` update accepts `keywords/*` and `mailboxIds` and refuses message
  content. `subject`, `to`, `textBody`, `bodyValues`, `bodyValues/0/value`,
  `header:Subject:asText`, and `blobId` each answered `notUpdated` with
  `invalidProperties` (or `invalid patch value`). Message content is immutable.
- `EmailSubmission/set` accepts an existing `emailId` and submits it. With
  `onSuccessUpdateEmail` the same message leaves drafts, appears in sent, clears
  `$draft`, and gains `$seen`; its identifier does not change.
- Reply headers must be set in header form on create:
  `header:In-Reply-To:asText` and `header:References:asText`. The derived
  `inReplyTo`/`references` properties were silently ignored on create, but the
  header form was stored, readable as `inReplyTo`/`references`, and survived
  submission.
- A submission with no recipients is `notCreated` with type `noRecipients`; an
  omitted envelope is accepted, and an explicit envelope including a Bcc
  recipient delivered to that recipient.
- `Email/get` does not serve `header:...` properties; the derived properties are
  the readable form. `messageId` is returned as a list of strings.

## Decisions

### Drafts are a separate resource, not a send flag

`POST /v1/drafts` creates a draft. The alternative was a `draft: true` field on
`POST /v1/messages`, which would put two different side-effect classes behind one
path: a draft is mailbox state, while a send is external and deduplicated. A
separate path keeps the idempotency requirement attached to the operation that
needs it, and lets draft input be optional where send input is not.

### A saved draft is not edited in place

The pinned release refuses every content update, so the service does not publish
an edit endpoint. Two alternatives were rejected. Keeping a stable public draft
identifier mapped to successive server messages would make the application a
second store of mailbox truth, which the architecture forbids. A replacement
endpoint that returns a new identifier would be surprising and would leave a
retry with the old identifier answering `not_found` after a successful call.

The documented workflow is: save a new draft, then delete the old one with the
existing message delete. The old draft goes to trash, so nothing is lost, and a
client that stops halfway leaves two visible drafts rather than silent data loss.
No public identifier is invented and no application state mirrors mail.

### Draft submission reuses the existing idempotency record

`POST /v1/drafts/{id}/send` claims the same `send_idempotency` row a send uses.
The request hash covers the operation, the draft identifier, and the draft's
current recipients, subject, and body, so the same key with a different draft or
different content is `conflict` rather than a silent second submission. A replay
returns the recorded Mailflow identifier and submits nothing. The record store
stays application state; no table is added.

Draft creation and reply composition do not require a key. They have no external
side effect, and a duplicate is visible and recoverable by deleting it. Adding a
second deduplication mechanism for mailbox state would widen the change without
protecting anything that cannot be corrected.

### Submission moves the existing message

The submission references the draft's server identifier and uses
`onSuccessUpdateEmail` to remove the drafts membership, add sent, clear `$draft`,
and set `$seen`. The message keeps its identifier, so a client that held the
draft's Mailflow identifier can read the sent message with it. Re-creating and
submitting the content instead would produce a second identifier and leave the
original draft behind.

The service reads the draft's To, Cc, and Bcc, checks every address against the
caller's domain, and sends an explicit envelope. The probe showed an omitted
envelope also works, but deriving it in the server would bypass the local-domain
rule for a Bcc added by another client. Bcc is read internally only; the public
draft request does not accept it, matching the send contract.

A submission is refused before the mail server is called when the message is not
a draft (`$draft` absent) or has no recipients, with `invalid_request`. A server
refusal maps to `send_rejected`, and the draft stays in drafts. No new error code
is needed.

### Reply composition is a server-side helper, not a client convention

`POST /v1/messages/{id}/reply` reads the original message's sender, subject,
`messageId`, and `references`, and creates a draft. The recipient is the original
sender, the subject gains one `Re: ` prefix when it does not already start with
one, and the threading headers are set in header form on create. References are
the original references followed by the original `messageId`, with duplicates
removed. The body is the caller's text.

The alternative was to expose the original's Message-ID and let the client set
threading. That would put a protocol header value in the public model and invite
clients to construct it, which the replaceability test forbids. Another
alternative was to quote the original body in the reply. Quoting format is a
product decision (attribution line, prefix, truncation, HTML later), so it stays
with the client; the service only carries the caller's text.

Threading is the one behavior that depends on a header form the pinned release
accepts but does not advertise as a settable property. The risk is that a future
release drops it; the reply would still be a valid draft but ungrouped. Tests
assert the exact create payload, the probe records the observed persistence, and
`docs/integration-surface.md` marks the header form as the verified one.

### The draft state stays internal

A draft is identified by the `$draft` keyword and the drafts mailbox role. Neither
is added to the public message shape: a client lists the drafts mailbox by role,
and submission checks the keyword. Exposing a `draft` boolean would be a
reasonable later addition, but it is not needed by any operation here, and the
versioned contract can add it additively.

## Risks and mitigations

- **A draft is submitted twice.** The idempotency claim happens before the mail
  server is called, and the recorded identifier is replayed. If recording fails
  after acceptance, success is reported and the failure logged, as sending does;
  the lease bounds the window.
- **A client edits a draft and loses the old copy.** No edit endpoint exists; the
  documented flow is create then delete, and delete is a soft delete to trash.
- **A draft authored elsewhere carries an unsupported recipient.** Submission
  validates every recipient before calling the server and refuses with
  `invalid_request`, leaving the draft intact.
- **The header form stops working in a later release.** The pinned release and
  the probe are recorded; tests fail loudly if the payload changes, and the reply
  remains a valid draft if the server ignores the headers.
- **Draft content is read for submission after a concurrent change.** The
  service reads the draft, validates it, and submits it; a concurrent edit by
  another client can change the draft between read and submit. The mail server is
  authoritative, and a mismatch is not silently repaired. This is the same
  read-then-act window every IMAP client has.

## Out of scope

In-place draft editing, reply-all, forward, quoting and HTML bodies,
attachments, signatures, scheduled send, and any change to the mail deployment
unit, database schema, or configuration.
