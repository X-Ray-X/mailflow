# Tasks

## 1. Upstream JMAP surface

- [x] 1.1 Record the 2026-11-18 draft and reply probe findings in `docs/integration-surface.md`, including draft creation with empty fields, content immutability, submission of an existing message, header-form threading, envelope and Bcc behavior, and the no-recipients refusal; verify each statement against the probe transcript and that the section is dated and names anything still unexercised.
- [x] 1.2 Add draft creation, draft reading, and submission of an existing message to `app/internal/jmap/mail.go`, including the header-form reply fields and the draft's recipients; verify with unit tests using the existing fake-server pattern that the create payload carries the headers, the submission references the existing identifier, and `noRecipients` maps to a refusal.
- [x] 1.3 Add draft creation, draft submission, and reply composition to `app/internal/mail`, with optional fields, the same recipient-domain and size bounds as send, a `$draft` check, an explicit envelope built from To/Cc/Bcc, one `Re: ` prefix, and threading references; verify unit tests cover each refusal, the envelope, the unchanged identifier, the single prefix, and a foreign message.

## 2. HTTP contract

- [x] 2.1 Add `POST /v1/drafts`, `POST /v1/drafts/{id}/send`, and `POST /v1/messages/{id}/reply`, reuse the idempotency claim for draft submission, and keep the existing error codes; verify API tests cover success, missing key, replay, conflict, in-flight, empty draft submission, not-a-draft, declined submission, foreign identifiers, and unknown fields.
- [x] 2.2 Update `docs/api.md` and `README.md` with the three endpoints, the draft lifecycle including the create-then-delete edit workflow, the reply rules, and the fact that draft creation is not deduplicated; verify every documented example against the running local stack with the credentials in `deploy/mail/.env`.
- [x] 2.3 Extend `app/scripts/verify-api.py` with draft create, draft send, replay, empty-draft refusal, reply composition, threading, and cross-account checks; verify the script exits successfully against `http://127.0.0.1:8081` and scans every new response for vendor vocabulary.

## 3. Integration and close-out

- [x] 3.1 Verify a reply draft over IMAP: create the reply through the HTTP API, then fetch its `References` and `In-Reply-To` headers and confirm the draft appears in Drafts; record the command and result in `docs/integration-surface.md`.
- [x] 3.2 Run `./app/scripts/go.sh` and `openspec validate --all --strict`; verify formatting, vet, unit tests, and all specs pass, and state any check that could not run.
- [ ] 3.3 Review `git status` and the full diff for secrets, unbounded retries, unhandled errors, and data-loss paths; commit on the `mailflow-drafts-and-replies` branch with the verification result in the message and verify the tree is clean.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
