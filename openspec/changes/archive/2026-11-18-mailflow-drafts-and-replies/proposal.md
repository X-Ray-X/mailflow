# Proposal

## Why

The service can send a message and organize mail, but it cannot compose one over
time or answer a message. Every send is final: there is no draft to save, review,
or correct, and a client cannot start a reply without knowing how the mail server
threads messages. A mail client needs both, and the pinned release was probed on
2026-11-18 so the contract can be built on observed behavior instead of guesses.

## What Changes

- Add `POST /v1/drafts` to save a plain-text draft in the caller's drafts
  mailbox. Recipients, subject, and body are optional, so a draft can be started
  before it is complete.
- Add `POST /v1/drafts/{id}/send` to submit an existing draft. It requires an
  `Idempotency-Key`, reuses the send record store, and moves the message from
  drafts to sent on success. A draft with no recipients or a message that is not
  a draft is refused.
- Add `POST /v1/messages/{id}/reply` to create a draft reply to a message. The
  service sets the recipient from the original sender, prefixes the subject once,
  and carries the original message's threading headers so standard clients group
  the reply with the conversation.
- Keep draft content immutable, matching the pinned release: a saved draft's
  subject, recipients, and body cannot be edited in place. A client that changes
  a draft saves a new one and deletes the old one with the existing message
  delete. No new endpoint pretends otherwise.
- Add no error codes, database tables, configuration, or deployment change.
  Draft creation and replies are mailbox state, so they are not deduplicated;
  sending is the external side effect and keeps the existing deduplication.

Out of scope: editing a draft in place, reply-all, forward, quoted or HTML
bodies, attachments, signatures, scheduled send, drafts created through the
public API appearing differently from any other draft, and any change to the mail
deployment unit. Drafts authored by another client remain readable and sendable;
their content is not rewritten.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `mail-api`: adds draft creation, draft submission with the existing send
  deduplication, and reply composition, including recipient and threading rules
  and the refusal cases those operations introduce.

## Impact

- `app/internal/jmap`: create a draft with reply headers, read a draft's
  recipients and threading fields, and submit an existing message by identifier.
- `app/internal/mail`: draft creation, draft submission, and reply composition
  in the adapter, with recipient-domain, size, and draft-state checks.
- `app/internal/api`: three routes, their request and response shapes, and reuse
  of the idempotency claim for draft submission.
- `app/scripts/verify-api.py`: live coverage of create, send, reply, refusal, and
  replay.
- `docs/api.md`, `docs/integration-surface.md`, and `README.md`: the new
  endpoints, the draft lifecycle, and the probed upstream behavior.
- No dependency, configuration, database schema, or deployment change.
