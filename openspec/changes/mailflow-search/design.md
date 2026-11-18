# Design

## Context

See `proposal.md` for motivation. The service already publishes mailbox and
message listing with a bounded page and an account-scoped cursor. The JMAP layer
builds an `Email/query` call for listing; the pinned release advertises JMAP mail
but the text filter shape, ordering, and anchor behavior have not been probed.
Stalwart owns all mail data and the application holds no message index.

## Goals / Non-Goals

**Goals:**

- Reuse the existing page, cursor, and summary types so search feels like listing.
- Keep the caller's account as the only scope; never derive it from a parameter.
- Establish the upstream filter behavior by probe before relying on it.

**Non-Goals:**

- Application-side indexing, caching of results, or stored searches.
- New sort modes, boolean/regex syntax, or searching attachment contents.
- Changing the message or listing contracts.

## Decisions

### Search is a query, not client-side filtering

`GET /v1/search` builds one upstream query and returns summaries. The
alternative was fetching every page and filtering in the service; it would make
a search as expensive as the whole mailbox and would put an unbounded read
behind a small public response. A second alternative — IMAP SEARCH — would
bypass the service as the only published API and expose another protocol's
vocabulary. A third — an application index — would make PostgreSQL a second
store of mailbox truth, which the architecture forbids.

### Probe the filter before choosing its shape

Task 1.1 queries the pinned release with a known phrase from a demo message and
records which `Email/query` filter condition matches, whether it searches bodies
as well as subjects, how results are ordered, and whether `anchor` paging works
with the filter. The adapter uses only the observed condition. If the observed
behavior differs from the expectation in this design, the design and spec are
updated in the same change.

### The search reuses listing's page and cursor

The same default and maximum page sizes apply, and the cursor anchors on the
last message identifier of the page. The alternative was a new search-specific
cursor prefix and page model; it would add a second set of rules for the same
operation without protecting anything. A stale cursor stays `invalid_cursor`,
matching listing.

### A mailbox filter is checked by the adapter

When `mailboxId` is supplied, the adapter parses it, checks its account segment
against the caller's session account, and verifies the mailbox exists before
building the upstream query. The alternative was passing the identifier straight
through; raw identifiers are per-account at the mail server, so a foreign
identifier could resolve to another caller's mailbox with the same number.

### Refusals reuse existing error codes

An empty query or an unknown parameter is `invalid_request`; a stale cursor is
`invalid_cursor`; a foreign or unknown mailbox is `not_found`; an upstream
outage is `service_unavailable`. No new code is needed. The alternative — a
distinct `search_failed` code — would enlarge the public vocabulary for a case
the contract can already express.

## Risks / Trade-offs

- **The observed filter does not support anchoring.** Task 1.1 gates the design;
  if `anchor` cannot be combined with the filter, the spec is amended to state
  that a search returns one bounded page rather than promising paging that does
  not work.
- **Text matching is broader or narrower than expected.** The probe records the
  observed matching on the demo corpus, and the public contract promises only
  "matching message summaries" with the probe as the reference, not a particular
  ranking or tokenisation.
- **A query returns too much work upstream.** Page size bounds the response, and
  the request timeout bounds the call; an oversized or slow query fails as
  `service_unavailable` instead of holding a connection open.
- **A foreign mailbox identifier collides at the mail server.** The adapter
  checks the encoded account before the upstream call, the same way listing and
  patches do.
