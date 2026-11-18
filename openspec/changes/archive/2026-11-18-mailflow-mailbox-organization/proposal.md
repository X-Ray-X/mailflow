# Proposal

## Why

The contract can list and read mail, send, and change flags, but a user cannot
organize it: there is no way to create, rename, or remove a mailbox, move a
message between mailboxes, or delete a message. That makes the service unusable
as the backing API for a mail client, even though the mail server already owns
this behavior. The pinned release was probed for the exact semantics, so the
contract can be added without guessing.

## What Changes

- Add `POST /v1/mailboxes` to create a custom mailbox, `PATCH /v1/mailboxes/{id}`
  to rename one, and `DELETE /v1/mailboxes/{id}` to remove one.
- Protect standard mailboxes: inbox, sent, drafts, trash, and junk cannot be
  renamed or deleted through the service.
- Refuse to delete a mailbox that still holds messages or child mailboxes,
  rather than deleting its contents.
- Enforce a bounded, unique mailbox name. Creation is deduplicated by that name,
  so a retried create cannot make a second mailbox.
- Extend `PATCH /v1/messages/{id}` with `mailboxId`, so a message can be moved as
  a desired state, alone or together with its flags. Moving is idempotent and
  preserves flags and attachments.
- Add `DELETE /v1/messages/{id}` as a soft delete that moves the message to the
  account's trash mailbox. It does not permanently erase mail.
- Add stable error codes for a duplicate mailbox name and for a mailbox that is
  not empty. Vendor error types stay internal.
- Keep mailbox state in the mail server. No new application database state.

Out of scope: hierarchical mailbox trees in the public model, permanent deletion
or emptying the trash, bulk operations, drafts and replies, attachments, search,
push, public internet mail, and any change to the mail deployment unit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `mail-api`: adds mailbox management, message moves, soft deletion, the name
  and standard-mailbox rules, and the error codes those operations return. The
  existing message patch gains a `mailboxId` field, specified by the new move
  requirement.

## Impact

- `app/internal/api`: three mailbox routes, the extended message patch, the
  message delete route, request validation, and error mapping.
- `app/internal/mail`: mailbox create/rename/delete, message move, and soft
  delete in the adapter, with the standard-mailbox and name rules.
- `app/internal/jmap`: `Mailbox/set` create/update/destroy and one combined
  `Email/set` update for flags plus mailbox membership.
- `app/scripts/verify-api.py`: live coverage of the new operations, including
  standard-mailbox refusal, duplicate names, non-empty deletion, move fidelity,
  and cross-account identifiers.
- `docs/api.md`, `docs/integration-surface.md`, and `README.md`: the published
  endpoints, codes, and the probed upstream semantics.
- No dependency, configuration, database, or deployment change.
