# Proposal

## Why

A client can list mailboxes and pages of messages but cannot find a message by
text. Without a published search endpoint a future web client would scan every
page itself or bypass the service with IMAP SEARCH. The pinned mail server's JMAP
filter behavior has not been probed, so the contract must rest on observed
behavior.

## What Changes

- Add `GET /v1/search`, returning message summaries for a caller-supplied text
  query, optionally restricted to one of the caller's mailboxes.
- Reuse the page size and cursor rules of message listing, so a search is bounded
  and can be paged with the same `limit` and `cursor` parameters.
- Keep the query as the caller's own text: no mail server filter names, vendor
  error codes, or Message-ID values appear in the request or response shape.
- Probe the pinned release for the exact filter condition, matching behavior,
  ordering, and paging before writing the adapter, and record the findings.
- Add no error codes, database tables, configuration, or deployment change.
  The existing `invalid_request`, `not_found`, `invalid_cursor`, and
  `service_unavailable` codes cover the refusal cases.

Out of scope: full-text indexing in the application, saved searches, search
across attachments or headers exposed as new fields, sorting choices, regex or
boolean query syntax, and any change to the mail deployment unit. Search results
are message summaries; reading a result uses the existing message endpoint.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `mail-api`: adds bounded, paged text search over the caller's mail with the
  same account scoping and vocabulary as message listing.

## Impact

- `app/internal/jmap`: extend the email query with the observed text filter and
  test the request shape.
- `app/internal/mail`: translate a search request into the upstream query and
  map the same page and cursor types listing uses.
- `app/internal/api`: one read route and its query-parameter validation.
- `app/scripts/verify-api.py` and `docs/`: live coverage and the contract.
- No dependency, configuration, database schema, or deployment change.
