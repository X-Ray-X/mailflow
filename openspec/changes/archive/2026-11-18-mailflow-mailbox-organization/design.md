# Design

## Context

See `proposal.md` for motivation. The current contract reads mailboxes and
messages, sends plain-text messages, and patches `read`/`starred`. The adapter
(`app/internal/mail`) translates between Mailflow types and JMAP, and every
public identifier encodes the account it was issued to (`app/internal/ids`).
Mailbox truth stays in the pinned Stalwart release; the application database
holds only idempotency records.

The design rests on a probe of `Mailbox/set` and `Email/set` on the pinned
release, recorded in `docs/integration-surface.md`. Observed on 2026-11-18 with
an unprivileged account:

- `Mailbox/set` create, rename, and destroy work. Create accepts `parentId` and
  `isSubscribed`; `isSubscribed` defaults to `false` and persists when set.
- A duplicate name at the same parent is `notCreated` with type `alreadyExists`
  and an `existingId`. An empty name is `invalidProperties`. A nonexistent
  parent is `invalidProperties`.
- Destroying a mailbox that holds messages is `notDestroyed` with
  `mailboxHasEmail`; a mailbox with children is `mailboxHasChild`.
  `onDestroyRemoveEmails: true` destroys the mailbox **and its messages**.
- The server permits renaming and deleting standard mailboxes; `myRights`
  reports `mayRename` and `mayDelete` for inbox, sent, drafts, trash, and junk.
  A role can be set on a custom mailbox, but only to a role no other mailbox
  already holds.
- `Email/set` update with a full `mailboxIds` map replaces the membership in one
  call and preserves keywords. Adding a mailbox without removing the old one
  leaves the message in both. Moving to the current mailbox is a successful
  no-op. A nonexistent target is `notUpdated` with `invalidProperties`.

## Goals / Non-Goals

**Goals:**

- A small REST surface for creating, renaming, and deleting custom mailboxes.
- Moving a message between mailboxes as desired state, preserving flags and
  attachments, and composable with a flag change in one request.
- A delete that is recoverable: it moves a message to trash instead of erasing
  it.
- No operation that can silently remove messages or a standard mailbox.
- No new application state, configuration, or deployment unit.

**Non-Goals:**

- Hierarchical mailbox trees in the public model. Mailboxes created through the
  service are top-level; existing nested mailboxes are listed flat.
- Permanent deletion, emptying trash, retention policy, and bulk operations.
- Idempotency keys for mailbox operations.
- Drafts, replies, attachments, search, and push.

## Decisions

### 1. Mailbox management is resource-shaped, without an idempotency key

`POST /v1/mailboxes` creates, `PATCH /v1/mailboxes/{id}` renames, and
`DELETE /v1/mailboxes/{id}` removes. None takes an `Idempotency-Key`:

- Create is deduplicated by the mailbox name. A retry after an unknown outcome
  cannot make a second mailbox; it answers `already_exists`, and the caller can
  list to find the first one. This is the operation's stated deduplication.
- Rename is desired-state: the request names the resulting name, so a repeat is
  a no-op.
- Delete is idempotent in effect: a repeat answers `not_found` and removes
  nothing else.

Alternatives rejected: requiring an idempotency key for create (the name is the
natural key and is already unique); `POST /mailboxes/{id}/rename` action
endpoints (the previous write design chose state-based requests).

### 2. Moving a message is a `mailboxId` field on the existing message patch

`PATCH /v1/messages/{id}` gains an optional `mailboxId`. A patch must name at
least one of `read`, `starred`, or `mailboxId`. This keeps one state-based
endpoint for a message instead of adding `/move`, and lets one request move and
flag a message atomically.

Alternatives rejected: `POST /v1/messages/{id}/move` (an action endpoint, and a
retry is indistinguishable from a second action); `PUT /v1/messages/{id}/mailbox`
(a second resource for one field, and the message already has a PATCH).

### 3. A move replaces mailbox membership in one `Email/set` call

The adapter sends `{"mailboxIds": {"<target>": true}}`, which the probe showed
replaces the whole map. The message ends up in exactly the target mailbox. This
is one atomic call, leaves keywords untouched, and is a no-op when the target is
already the message's only mailbox.

Alternatives rejected: reading the current map and removing each old id (two
calls and a race); adding the target without removing the source (a copy, not a
move); `Email/import` (unverified and unnecessary).

### 4. Flags and membership share one upstream update

The existing `SetEmailKeywords` becomes a general email update that accepts
optional flag values and an optional target mailbox. It builds one `Email/set`
update containing per-keyword patches and, when present, the full `mailboxIds`
map. Per-keyword patches are kept: replacing the keyword map would drop
`$draft`, `$hasattachment`, or a keyword another client set.

Alternatives rejected: calling `SetEmailKeywords` then a separate move (a
failure between them leaves a half-applied request).

### 5. Delete is a soft delete to trash

`DELETE /v1/messages/{id}` resolves the account's trash mailbox by role and moves
the message there. It never destroys the message. A message whose only mailbox
is trash is left there and the request succeeds. Permanent deletion is
deliberately absent.

Alternatives rejected: `Email/set` destroy (permanent, unrecoverable, and the
probe confirms it works, which makes accidental data loss easy); no delete
endpoint at all (the client would have to find the trash role and issue a move,
which is easy to get wrong).

### 6. Deleting a mailbox refuses anything that is not empty

The service deletes only a custom mailbox with no messages and no children. The
vendor offers `onDestroyRemoveEmails: true`, which the probe confirmed destroys
the messages; the service never sends it. A non-empty mailbox answers
`mailbox_not_empty` so the caller can move or delete its contents first.

Alternatives rejected: cascade deletion (silent data loss); automatically moving
messages to trash on mailbox deletion (surprising, and the trash may itself be
the target); deleting children with the parent (same problem).

### 7. Standard mailboxes are immutable through the service

The service refuses to rename or delete a mailbox whose role is not `custom`,
even though the vendor permits it. Standard mailboxes are how clients find
inbox, sent, drafts, trash, and junk; renaming or removing one would break that
contract and could lose the sent or trash destination.

Alternatives rejected: allowing it because the vendor does (the HTTP contract is
stricter on purpose); a separate `standard_mailbox` code (the request is simply
not valid for this account, which is what `invalid_request` already means).

### 8. Names are service-validated and unique ignoring case

A name is trimmed, must be 1–255 bytes, and must not contain control characters
or `/`. Uniqueness is checked against every mailbox in the account, ignoring
case, because the list is flat. The vendor only rejects an exact duplicate at
the same parent, so the service enforces the stricter rule and maps a vendor
`alreadyExists` race to `already_exists`.

Alternatives rejected: vendor-only rules (a flat list could show `Projects` and
`projects` as two indistinguishable entries); allowing `/` (it is the IMAP
hierarchy delimiter, so a flat name containing it would be ambiguous to standard
clients).

### 9. Created mailboxes are subscribed

The adapter sends `isSubscribed: true` on create. The probe showed the default is
`false`, and an unsubscribed mailbox can be hidden by standard clients, which
would break the mailbox-access requirement that a mailbox created in one client
is visible in another.

Alternative rejected: leaving subscription to the client (the mailbox would be
created but not reliably visible).

### 10. The public model stays flat

The mailbox list keeps its existing shape; no `parentId` is exposed. A nested
mailbox created by a standard client is listed flat and can still be renamed or
deleted by identifier. Deletion of a parent with children is refused.

Alternative rejected: exposing hierarchy now (it needs parent validation,
depth rules, and a tree-shaped contract; it is additive later).

### 11. Error mapping stays Mailflow-owned

New codes: `already_exists` (409) for a duplicate name, and `mailbox_not_empty`
(409) for a mailbox that holds messages or children. Invalid names and standard
mailboxes use `invalid_request`; unknown or foreign identifiers use `not_found`.
Vendor types (`alreadyExists`, `invalidProperties`, `mailboxHasEmail`,
`mailboxHasChild`) stay in the adapter and logs.

Alternatives rejected: reusing `conflict` for duplicates (it already means an
idempotency-key mismatch); `403` for standard mailboxes (there is no separate
authorization decision, only a rule).

### 12. No application state is added

Every mailbox and membership fact stays in Stalwart. Nothing is written to
PostgreSQL, no projection is introduced, and no configuration or deployment
change is needed. This keeps the replaceability and single-source rules intact.

## Risks / Trade-offs

- **Soft delete means trash grows and the API cannot erase mail.** → The
  contract states that delete is recoverable and permanent deletion is out of
  scope. A later change can add an explicitly confirmed empty-trash operation.
- **A full membership replacement collapses a message that was deliberately in
  several mailboxes.** → The contract defines a move as "exactly the target
  mailbox"; the read model already treats a message as belonging to the mailbox
  being listed. The limitation is documented.
- **Case-insensitive uniqueness is stricter than the mail server.** → The
  service checks first and maps a vendor `alreadyExists` race to
  `already_exists`; no duplicate is created either way.
- **A flat list hides hierarchy created by IMAP clients.** → Names remain
  readable and identifiers remain usable; hierarchy is an explicit non-goal and
  can be added additively.
- **A message can arrive in a mailbox between the emptiness check and the
  delete.** → The vendor refuses the destroy with `mailboxHasEmail`; the adapter
  maps it to `mailbox_not_empty`, and nothing is cascaded.
- **A create can succeed after the client sees a timeout.** → The name is
  unique, so a retry answers `already_exists` and the client lists the account;
  no second mailbox exists.
- **Standard-mailbox protection applies only to the HTTP contract.** → A client
  speaking IMAP directly can still rename or delete a standard mailbox. That is
  the mail server's behavior, outside this service; the HTTP contract does not
  add a second store or intercept IMAP.
- **A name containing `/` is refused by the service although the vendor might
  accept it.** → The restriction avoids IMAP hierarchy ambiguity in a flat
  contract; it is documented as a Mailflow rule.

## Migration Plan

1. Land the adapter, routes, validation, and tests together. The change is
   additive: existing reads, send, and flag patches keep their shapes.
2. No database, configuration, image, Compose, or deployment change. The mail
   unit is untouched.
3. Roll back by removing the new routes; mailboxes and messages created through
   them remain in the mail server and are still visible to standard clients.
   Nothing created by this change needs a data migration.

## Open Questions

None. Hierarchy and permanent deletion are explicit non-goals, not deferred
decisions that would change this design.
