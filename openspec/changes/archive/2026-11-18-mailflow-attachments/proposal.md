# Proposal

## Why

The published contract reports `hasAttachment` but gives a client no way to see
what is attached or read it, so a future web client would have to bypass the
service and speak IMAP. The pinned mail server's attachment surface has not been
probed yet, so the contract must be built on observed behavior rather than
assumed JMAP properties.

## What Changes

- Add `GET /v1/messages/{id}/attachments`, listing each attachment's filename,
  content type, size, and a stable position used to address it.
- Add `GET /v1/messages/{id}/attachments/{index}`, returning the bytes with the
  attachment's content type and a download disposition, bounded in size.
- Address attachments by their owning Mailflow message identifier plus a
  position. Mail server blob identifiers and download URLs never appear in a
  response; a caller cannot name another account's attachment.
- Probe the pinned release for the exact attachment read and download behavior
  and record the findings before writing the adapter.
- Add no error codes, database tables, configuration, or deployment change. The
  existing `not_found`, `invalid_request`, `service_unavailable`, and `internal`
  codes cover the refusal cases.

Out of scope: uploading or sending attachments, inline/`cid` rendering,
thumbnails, previewing, virus scanning, attachment search, changing or deleting
an attachment, and any change to the mail deployment unit. A message's text body
and send contract stay exactly as they are.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `mail-api`: adds read-only attachment metadata and download to the message
  resource, with account scoping and bounded, versioned behavior.

## Impact

- `app/internal/jmap`: read attachment metadata, fetch the session's download
  URL template, and download one blob with the caller's credentials.
- `app/internal/mail`: translate attachment metadata and expose a bounded
  download to the API layer.
- `app/internal/api`: two read routes and their wire types.
- `app/scripts/verify-api.py` and `docs/`: live coverage, the contract, and the
  observed upstream behavior.
- No dependency, configuration, database schema, or deployment change.
