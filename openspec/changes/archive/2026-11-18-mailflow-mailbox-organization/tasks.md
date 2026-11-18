# Tasks

## 1. Upstream JMAP surface

- [x] 1.1 Record the 2026-11-18 `Mailbox/set` and `Email/set` probe findings in `docs/integration-surface.md`, including subscription, role, duplicate, non-empty, child, and full-membership-replacement behavior; verify each statement against the probe transcript and that the section is dated and names anything still unexercised.
- [x] 1.2 Add `Mailbox/set` create, rename, and destroy to `app/internal/jmap/mail.go`, mapping `alreadyExists`, `invalidProperties`, `mailboxHasEmail`, `mailboxHasChild`, and `notFound` into adapter error classes; verify with unit tests using the existing fake-server pattern for success and each refusal.
- [x] 1.3 Replace `SetEmailKeywords` with one email update that accepts optional read/starred values and an optional target mailbox, sending per-keyword patches plus a full `mailboxIds` map; verify unit tests cover flag-only, move-only, combined, same-target no-op, and `notUpdated` mapping.
- [x] 1.4 Add mailbox create/rename/delete to `app/internal/mail` with trimmed 1–255-byte names, no control characters or `/`, case-insensitive uniqueness, `isSubscribed: true`, standard-mailbox protection, and the new error classes; verify unit tests for each rule and for cross-account identifiers.
- [x] 1.5 Add message move and soft delete to `app/internal/mail`: resolve the trash role, check the target mailbox belongs to the caller, send one update, and return the updated summary; verify unit tests show flags and attachment state survive, a move to the only current mailbox is a no-op, and a foreign target is `not_found`.

## 2. HTTP contract

- [x] 2.1 Add `POST /v1/mailboxes`, `PATCH /v1/mailboxes/{id}`, `DELETE /v1/mailboxes/{id}`, extend `PATCH /v1/messages/{id}` with `mailboxId`, and add `DELETE /v1/messages/{id}`; verify API tests cover success, unknown fields, empty patches, duplicate names, standard mailboxes, non-empty deletion, and cross-account identifiers, including the new `already_exists` and `mailbox_not_empty` codes.
- [x] 2.2 Update `docs/api.md` and `README.md` with the endpoints, request/response shapes, name rules, soft-delete semantics, and the two new error codes; verify every documented example against the running local stack with the credentials in `deploy/mail/.env`.
- [x] 2.3 Extend `app/scripts/verify-api.py` with create/rename/delete, duplicate and invalid names, standard-mailbox refusal, non-empty refusal, move with flags and attachment, delete-to-trash, restore, and cross-account checks; verify the script exits successfully against `http://127.0.0.1:8081` and scans every new response for vendor vocabulary.

## 3. Integration and close-out

- [x] 3.1 Verify standard-client visibility: create a mailbox and move a message through the HTTP API, then list the account over IMAP and confirm both are visible; record the command and result in the change's verification notes or `docs/integration-surface.md`.
- [x] 3.2 Run `./app/scripts/go.sh` and `openspec validate --all --strict`; verify formatting, vet, unit tests, and all specs pass, and state any check that could not run.
- [x] 3.3 Review `git status` and the full diff for secrets, unbounded retries, unhandled errors, and data-loss paths; commit on the `mailflow-mailbox-organization` branch with the verification result in the message and verify the tree is clean.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
