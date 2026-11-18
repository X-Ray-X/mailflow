# Tasks

## 1. Upstream surface

- [x] 1.1 Probe the pinned release with `bob@mailflow.test`: query `Email/query`
  with a text filter for a phrase known to be in a demo message, record which
  filter condition matches, whether bodies as well as subjects are searched,
  the ordering, and whether `anchor` paging works with the filter. Record
  commands, responses, failures, and unexercised cases in
  `docs/integration-surface.md`; verify every statement against the transcript.
- [x] 1.2 Add the observed filter to `app/internal/jmap/mail.go` and test the
  query payload with the fake server: verify the filter value is the caller's
  text, the page and anchor are passed, and no vendor property name leaks into
  a public type.

## 2. Adapter and contract

- [x] 2.1 Add a search method to `app/internal/mail` that validates the
  optional mailbox's account, reuses the page and cursor rules, and returns
  message summaries; verify unit tests cover a match, no matches, a foreign
  mailbox, an unknown mailbox, a stale cursor, and an upstream failure.
- [x] 2.2 Publish `GET /v1/search` with only `q`, `mailboxId`, `limit`, and
  `cursor` accepted; verify API tests cover success, empty query, unknown
  parameter, foreign mailbox, stale cursor, and the vendor-vocabulary scan of
  the response.

## 3. Live verification and close-out

- [x] 3.1 Update `docs/api.md` and `README.md` with the endpoint, its parameters,
  and the observed matching behavior; verify the documented examples against
  the running local stack with the demo credentials in `deploy/mail/.env`.
- [x] 3.2 Extend `app/scripts/verify-api.py` with a matching search, a query with
  no matches, a foreign mailbox, an unknown parameter, and a vendor scan of the
  new responses; verify the script exits successfully against
  `http://127.0.0.1:8081` and scans every new response.
- [x] 3.3 Run `./app/scripts/go.sh` and `openspec validate --all --strict`;
  verify formatting, vet, unit tests, and all specs pass, and state any check
  that could not run. Review `git status` and the full diff for secrets,
  unbounded reads, unhandled errors, and data-loss paths, then commit on the
  `mailflow-search` branch with the verification result in the message and
  verify the tree is clean.

## Workflow follow-up

- Archive the change after the acceptance scenarios have been observed to pass.
- Verify the archived result and sync any remaining delta.
