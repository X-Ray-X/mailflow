# Design

## Context

See `proposal.md` for motivation. The service already publishes reads of
mailboxes, message lists, and message bodies, and translates JMAP wire types
inside `app/internal/jmap` so no vendor field reaches HTTP. The session document
observed on the pinned release advertises a `downloadUrl` template with
`accountId`, `blobId`, `name`, and `accept` parameters, but no attachment read
has been probed. The mail server owns attachment bytes; the application database
owns no mailbox state and the configuration has no attachment setting.

## Goals / Non-Goals

**Goals:**

- Publish attachment metadata and download through the existing message
  resource, with no new identifier prefix, database table, or configuration.
- Keep the caller's credentials and the mail server as the only authority for
  what may be read.
- Establish the upstream behavior by probe before relying on it.

**Non-Goals:**

- Uploading, sending, inline rendering, previewing, or modifying attachments.
- Caching attachment bytes in the application or mirroring mail state.
- Changing the text body or send contract.

## Decisions

### Address an attachment by message and position

`GET /v1/messages/{id}/attachments/{index}` uses the existing Mailflow message
identifier plus a zero-based position from the metadata list. The alternative
was a new `att1_` identifier carrying the server's blob id. That would add an
identifier prefix and a parse path for an object the service does not own, and
it would put a blob-derived value in the public model. A position is stable
because the pinned release refuses message content updates, so a message's
attachments cannot change under an identifier. If a later release allows content
edits, this change must be revisited before publishing positions as stable.

### Probe before choosing the upstream read

Task 1.1 records an upstream probe of `Email/get` with the `attachments`
property on a message known to have an attachment (the demo inbox already holds
one). It verifies the property name, the metadata fields returned, whether list
order is stable across reads, and whether the session's `downloadUrl` template
can be used with the caller's credentials. The adapter is written against the
observed shape; no untested JMAP property or vendor configuration is invented.

### The JMAP layer alone handles the download URL

`jmap.Session` gains the download template from the session document. The JMAP
client expands it for one blob and fetches the bytes with the caller's
credentials and the configured TLS settings. Before fetching, it checks that
the expanded URL is HTTPS and that its host matches the configured mail host; a
session document naming another host is treated as a server failure. The
alternative was returning the vendor URL to the client. That would bypass the
service as the only published API, expose a vendor address, and lose the
account and size rules. A second alternative — proxying through a generic URL
fetcher — would allow an untrusted URL to choose the destination.

### Download is bounded and streamed through the HTTP response

The adapter returns attachment metadata plus a byte reader bounded by a fixed
public limit chosen below the pinned server's advertised 50 MB attachment cap.
The API layer streams that reader to the client with `Content-Type`,
`Content-Disposition`, `Cache-Control: no-store`, and `X-Content-Type-Options:
nosniff`. If the reader exceeds the limit or the upstream fetch fails before a
complete response, the handler abandons the response instead of shipping a
truncated attachment. The alternative was reading the whole attachment into
memory first; it would make a large attachment an availability risk. The fixed
limit avoids a new environment variable in a change that promises no
configuration change, and is documented as part of the contract.

### Refusals reuse existing error codes

A malformed or foreign message identifier follows the service's existing
`not_found` rule; a malformed position is `invalid_request`; an unknown position
is `not_found`. An upstream outage or a failed size check is
`service_unavailable`, because the caller could retry after the mail server
recovers. No new code is added. The alternative — a distinct `attachment_too_large`
code — would enlarge the public error vocabulary for a case the contract can
already express.

## Risks / Trade-offs

- **The probe finds a different attachment shape.** Task 1.1 gates the adapter
  work; if the observed properties differ from the JMAP expectation, the design
  and spec are updated in the same change rather than implemented from memory.
- **A position changes when a message is edited elsewhere.** The pinned release
  refuses content updates. If that changes, a client could receive a different
  attachment for the same position; the risk is recorded with the pinned release
  in `docs/integration-surface.md` and the fetch always re-reads metadata.
- **A malicious filename drives the download header.** The API layer quotes and
  sanitises the filename into `Content-Disposition` and refuses control
  characters, so a vendor-supplied name cannot inject a header or a path.
- **A download fails mid-stream.** The handler cannot change the status after
  writing bytes, so it closes the connection and logs the failure; the client
  must treat an incomplete body as a failed download and retry.
- **A session template points at another host.** The HTTPS and host check turns
  that into a server failure rather than following an untrusted address.
