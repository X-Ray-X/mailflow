# Integration surface

This file records the mail server's machine-facing interface as **observed**, so
the next change can design the Go service against facts instead of assumptions.
It is an internal document: vendor names, account ids, and JMAP vocabulary stay
here and must not reach the public Mailflow API.

Everything below was exercised against the running stack on
`ghcr.io/stalwartlabs/stalwart:v1.1.0` (linux/arm64). Anything not exercised is
listed under [Not verified](#not-verified).

## Endpoint and authentication

| Item | Value |
|---|---|
| Session | `GET https://mail.mailflow.test/jmap/session` |
| API | `POST https://mail.mailflow.test/jmap/` |
| Events | `GET https://mail.mailflow.test/jmap/eventsource/` |
| Local address | `https://127.0.0.1:443` (port bound to loopback) |
| Authentication | HTTP Basic with the account's full address and password |
| Content type | `application/json` on every POST |

Basic authentication was confirmed for three identities: `admin@mailflow.test`
(management calls), and the unprivileged accounts `alice@mailflow.test` and
`bob@mailflow.test` (mail calls). The session document exposes the same
`username` that authenticated.

The JMAP hostname must be the certificate's name. The stack publishes `443` on
loopback only, so a process on the host reaches it at `https://127.0.0.1:443`
while presenting `mail.mailflow.test` as the TLS server name, or after
`mail.mailflow.test` is resolvable to `127.0.0.1`. A container instead joins the
mail project's network (`mailflow-mail_default`) and dials
`https://mailflow-stalwart/`, still presenting `mail.mailflow.test` as the TLS
server name. That path was exercised on 2026-11-15 from inside the mail container
with `curl --resolve mail.mailflow.test:443:<mail container ip>` and the
configured CA: the session returned `200` with a verified certificate. There is
no plaintext management port once the server is configured; `8089` remains an
unauthenticated health/setup listener.

## Session document

`GET /jmap/session` with an account's credentials returns (abridged, observed):

```json
{
  "username": "bob@mailflow.test",
  "apiUrl": "https://mail.mailflow.test/jmap/",
  "downloadUrl": "https://mail.mailflow.test/jmap/download/{accountId}/{blobId}/{name}?accept={type}",
  "uploadUrl": "https://mail.mailflow.test/jmap/upload/{accountId}/",
  "eventSourceUrl": "https://mail.mailflow.test/jmap/eventsource/?types={types}&closeafter={closeafter}&ping={ping}",
  "state": "41423a47",
  "accounts": { "d": { "name": "bob@mailflow.test", "isPersonal": true, "isReadOnly": false } },
  "primaryAccounts": { "urn:ietf:params:jmap:mail": "d", "...": "d" },
  "capabilities": { "urn:ietf:params:jmap:core": { "...": "..." }, "urn:ietf:params:jmap:mail": {} }
}
```

Two details matter for the adapter:

- **The account id is server-assigned and opaque.** `bob` was `d`, the
  administrator was `b`. It is a routing key, not an identity: read it from
  `primaryAccounts` at runtime rather than deriving it from the account name or
  from any other account's id.
- **Every mailbox method requires `accountId` explicitly.** Omitting it is not
  implied from the session: the server answers
  `invalidArguments: The "accountId" property is required.`
- **State strings are opaque and change with data.** `state` and the values in
  `StateChange` events are only useful for comparison and for `Foo/changes`;
  they are not stable identifiers.

## Advertised capabilities

`capabilities` for an unprivileged account contained the following (sorted here
for readability):

```
urn:ietf:params:jmap:blob                    urn:ietf:params:jmap:principals
urn:ietf:params:jmap:calendars               urn:ietf:params:jmap:principals:availability
urn:ietf:params:jmap:calendars:parse         urn:ietf:params:jmap:quota
urn:ietf:params:jmap:contacts                urn:ietf:params:jmap:sieve
urn:ietf:params:jmap:contacts:parse          urn:ietf:params:jmap:submission
urn:ietf:params:jmap:core                    urn:ietf:params:jmap:vacationresponse
urn:ietf:params:jmap:emailpush               urn:ietf:params:jmap:webpush-vapid
urn:ietf:params:jmap:filenode                urn:ietf:params:jmap:websocket
urn:ietf:params:jmap:mail
urn:ietf:params:jmap:metadata
```

Mailflow needs only `core` and `mail`. The others are advertised and are not a
commitment: the adapter must use only what this document verifies.

Observed `urn:ietf:params:jmap:core` limits:

| Limit | Value |
|---|---|
| `maxSizeUpload` | 50,000,000 bytes |
| `maxSizeRequest` | 10,000,000 bytes |
| `maxConcurrentRequests` / `maxConcurrentUpload` | 4 |
| `maxCallsInRequest` | 16 |
| `maxObjectsInGet` / `maxObjectsInSet` | 500 |
| `collationAlgorithms` | `i;ascii-numeric`, `i;ascii-casemap`, `i;unicode-casemap` |

`urn:ietf:params:jmap:mail` advertised an empty object; its limits
(`maxMailboxesPerEmail` 100, `maxMailboxDepth` 10, `maxSizeMailboxName` 255,
`maxSizeAttachmentsPerEmail` 50,000,000) are under each account's
`accountCapabilities`.

## Request shape

Every call is one envelope with a `using` array and one or more method calls:

```json
{
  "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
  "methodCalls": [
    ["Mailbox/get", { "accountId": "d" }, "m1"],
    ["Email/query", { "accountId": "d", "filter": { "inMailbox": "a" }, "limit": 1 }, "q1"]
  ]
}
```

The response pairs each call with a `methodResponses` entry in the same order,
plus `sessionState`. Management calls use the same envelope with
`urn:stalwart:jmap` in `using` and `x:`-prefixed methods; those are
administrative and must never be exposed through the Mailflow API.

## Mailbox and message reads

The chain below was exercised end to end for `bob@mailflow.test`:

1. `Mailbox/get` returned the mailbox list. Each entry has `id`, `name`,
   `role` (`inbox`, `sent`, `drafts`, `trash`, `junk`), `totalEmails`,
   `unreadEmails`, and `myRights`. `INBOX` was `a`, the account `d`.
2. `Email/query` with `{"inMailbox":"a","limit":1}` returned
   `{"ids":[...],"queryState":...}`. The mailbox id must be looked up from
   `Mailbox/get` by `role`; names such as `Sent Items` are presentation, roles
   are the stable handle.
3. `Email/get` with those ids and
   `properties: ["subject","from","receivedAt","keywords","mailboxIds"]`
   returned, for one message:

```json
{
  "id": "eaaaaab",
  "subject": "Mailflow verification message",
  "from": [{ "name": null, "email": "alice@mailflow.test" }],
  "receivedAt": "2026-11-13T23:31:44Z",
  "keywords": { "$hasattachment": true },
  "mailboxIds": { "a": true }
}
```

`keywords` is where message state lives. The same messages read back as
`{"$hasattachment": true, "$flagged": true, "$seen": true}` — the flags set over
IMAP, in JMAP vocabulary. The Mailflow model must map these to its own terms
(read, starred, has attachment) rather than pass the vendor keywords through.
`mailboxIds` is a map of mailbox id to `true`, not a list.

The same data was independently visible over IMAP, including flags set by
another client, so the two protocols read one store.

### Paging, bodies and unknown identifiers (probed 2026-11-15)

Exercised against the same stack, with four messages in one mailbox and a page
size of two:

- **Paging works as the adapter needs it.** `Email/query` with
  `sort: [{property: "receivedAt", isAscending: false}]`, `limit: 2` and
  `calculateTotal: true` returned two ids with `total: 4` and `position: 0`. A
  second call with `anchor: <last id of page one>` and `anchorOffset: 1`
  returned the next two ids with `position: 2`, repeating nothing. A third call
  returned an empty `ids` list with `position: 4`. `position + len(ids) < total`
  is therefore a reliable "more messages exist" signal, so a page size need not
  be guessed from a short page.
- **An unknown anchor is an error, not a silent restart.** The server answered
  the method call with `{"type":"anchorNotFound","description":...}`. The adapter
  can turn that into its own invalid-cursor error instead of skipping mail.
- **An unknown mailbox id is not an error.** `Email/query` with
  `filter.inMailbox` set to an id that does not exist returned an empty `ids`
  list, so existence has to be asked for with `Mailbox/get` and its `notFound`.
- **An unknown message id is reported, not invented.** `Email/get` with an id
  that does not exist returned `list: []` and `notFound: ["..."]`.
- **Body values must be requested by name.** With an explicit `properties` list
  that omitted `bodyValues`, `Email/get` returned no bodies even with
  `fetchTextBodyValues: true`. Naming `bodyValues` in `properties` returned
  `bodyValues` keyed by part id. The default property set (when `properties` is
  omitted) includes bodies, but also `blobId`, `threadId`, `messageId`, and
  other vendor detail, which is why the adapter always sends an explicit list.
- **`maxBodyValueBytes` is honoured.** Setting it to 8 returned a shortened
  value with `isTruncated: true`.
- **Observed roles** are `inbox`, `sent`, `drafts`, `trash`, `junk`, and `none`
  for a mailbox created without a role. There is no `archive` role in the
  default set.
- **`Email/get` returned messages in the order of the requested ids** in this
  observation. The adapter still reorders by the query result rather than
  relying on it.

### Identifiers are per-account, not global (probed 2026-11-16)

Raw identifiers are short sequences assigned independently per account, so the
same identifier names different objects in different accounts. Observed on the
same stack:

- alice and bob each held mailboxes `a`, `b`, `c`, `d`, `e`, and `h` for their
  own standard mailboxes. Alice's sent mailbox `e` is not bob's sent mailbox `e`.
- In the scoping probe, alice's private message `2aaaaah` and bob's inbox message
  `2aaaaah` were different messages with different subjects. Bob's request for
  alice's identifier returned *his own* message rather than an error.
- `Mailbox/get` with another account's mailbox identifier returns this account's
  object with that identifier, not an error; `Email/query` with a foreign mailbox
  identifier returns empty `ids`; `Email/get` with a foreign message identifier
  returns this account's object when one exists with that identifier.
- A mailbox identifier that exists only in alice's account (`i`–`o` after
  creating seven extra mailboxes) returned `notFound` for bob, and a message
  identifier unique to alice likewise returned nothing for bob.

Consequences for the service: an identifier must carry the account it was issued
to, otherwise the same identifier would mean different things to different
callers. The service refuses a foreign identifier locally, before asking the
mail server. This is a wrong-answer problem, not a confidentiality problem: no
probe made another account's content readable.

### Sending and flag updates (probed 2026-11-17)

Exercised with `alice@mailflow.test` on the same pinned release. The account id
was `c`, the drafts mailbox `d`, and the sent mailbox `e` for this account; as
the earlier section records, those are per-account and must be looked up by role.

- **`Identity/get` needs the submission capability.** With only `core` and
  `mail` in `using`, the server answers
  `{"type":"unknownMethod","description":"Method Identity/get requires capability urn:ietf:params:jmap:submission ..."}`.
  The account advertises `urn:ietf:params:jmap:submission` and one identity:
  `{"id":"b","name":"alice@mailflow.test","email":"alice@mailflow.test"}`.
  Submission uses that identity's `id`; the address must come from here, not
  from the caller.
- **Creating a message from parts works.** `Email/set` create with
  `mailboxIds`, `keywords`, `to`, `subject`, `textBody` as a part with a
  `partId`, and `bodyValues` returned an id, blob id, thread id, and size. No
  MIME construction or upload is involved.
- **Create and submit can share one envelope** with a back-reference:
  `"emailId": "#p1"` refers to the create with client id `p1`.
  `onSuccessUpdateEmail` with `{"#s1": {...}}` applies to the submission.
- **A submission without `onSuccessUpdateEmail` leaves the message in Drafts.**
  The created email still had `mailboxIds: {"d": true}` and `keywords:
  {"$draft": true, "$seen": true}` after the submission succeeded. The success
  update is therefore required, not a convenience:

```json
["EmailSubmission/set", {
  "accountId": "c",
  "create": {"s1": {
    "identityId": "b",
    "emailId": "#p1",
    "envelope": {
      "mailFrom": {"email": "alice@mailflow.test"},
      "rcptTo": [{"email": "bob@mailflow.test"}]
    }
  }},
  "onSuccessUpdateEmail": {"#s1": {
    "mailboxIds/d": null,
    "mailboxIds/e": true,
    "keywords/$seen": true,
    "keywords/$draft": null
  }}
}, "c2"]
```

The response contained three method responses in order: the `Email/set` create
(`created.p1.id`), the `EmailSubmission/set` create (`created.s1.id`,
`undoStatus: "pending"`), and an `Email/set` update for the same email. A
subsequent `Email/get` showed the message in `{"e": true}` with
`keywords: {"$hasnoattachment": true, "$seen": true}` — out of Drafts, draft
cleared, and read. `$hasnoattachment` is a vendor keyword and must not reach a
public payload.
- **Per-keyword patches work and preserve unrelated keywords.** Patching
  `{"update":{"eaaaaab":{"keywords/$seen":false}}}` removed `$seen` and left
  `$hasattachment: true` in place. Setting the key to `false` is how a keyword
  is cleared; the map only lists keywords that are set. A full-map replacement
  is therefore unnecessary and would be destructive.
- **A refused submission is `notCreated`, not a method error.** Submitting an
  envelope whose recipient was `nobody@example.invalid` answered:

```json
{"notCreated": {"s1": {
  "type": "invalidProperties",
  "description": "Invalid e-mail address \"nobody@example.invalid\".",
  "properties": ["envelope"]
}}}
```

The description names the address and is vendor text; the service maps this to
its own rejection without copying it.
- **Destroying a just-created message works**, which makes compensating a failed
  submission feasible: `Email/set` destroy returned the id in `destroyed`.
- **`undoStatus: "pending"` is not used.** A submission can be undone in a short
  window after acceptance; the service treats acceptance as final and does not
  expose or call undo.

### Mailbox organization (probed 2026-11-18)

Exercised with `alice@mailflow.test` on the same pinned release. The account id
was `c`; identifiers below are this account's, so they are examples, not stable
values. The probe created, renamed, and destroyed mailboxes, moved a message,
and read the results back with `Mailbox/get` and `Email/get`.

- **`Mailbox/set` create, update (rename), and destroy all work.** Create takes
  `name`, an optional `parentId`, and an optional `isSubscribed`. Rename is
  `update` with `{"name": "..."}`. Destroy is `destroy: [id]` and removes the
  mailbox when it is empty.
- **`isSubscribed` defaults to `false`.** A mailbox created without it read back
  as `isSubscribed: false`; setting it to `true` persisted. An unsubscribed
  mailbox can be hidden by a standard client, so the adapter always sends
  `true` on create.
- **A duplicate name at the same parent is `notCreated`.** The entry has type
  `alreadyExists` and carries an `existingId`. Creating the same name under a
  different parent was not probed, so the vendor's exact scope is unrecorded.
- **An empty name is `notCreated` with `invalidProperties`.** The same type is
  returned when `parentId` names a mailbox that does not exist.
- **Destroying a non-empty mailbox is refused.** Without extra arguments the
  response is `notDestroyed` with type `mailboxHasEmail`. Destroying a mailbox
  that has children is `notDestroyed` with type `mailboxHasChild`.
- **`onDestroyRemoveEmails: true` destroys the messages with the mailbox.** The
  probe confirmed the mailbox and the message inside it both disappeared. The
  service never sends this flag.
- **The server permits changing or removing standard mailboxes.** `Mailbox/get`
  reports `mayRename` and `mayDelete` as `true` for inbox, sent, drafts, trash,
  and junk. The service did not exercise a rename or destroy on a standard
  mailbox; only the reported rights were observed.
- **A role is writable but unique.** Setting a custom mailbox's role to
  `archive` was accepted; setting it to `inbox` or `sent` was refused because
  another mailbox already held that role. Mailflow never writes a role.
- **A full `mailboxIds` map replaces membership in one call.** Sending
  `{"mailboxIds": {"<target>": true}}` moved the message: the source id was gone
  from the read-back map. Adding a target without removing the source left the
  message in both, which is a copy, not a move. Moving to the mailbox the
  message was already only in succeeded as a no-op.
- **Keywords survive a membership replacement.** The probe read the message
  before and after; `keywords` was unchanged by the move.
- **A nonexistent move target is `notUpdated` with `invalidProperties`.** The
  message stayed where it was. An unknown message id is reported the same way
  with type `notFound`.
- **Unexercised:** a create with a name that duplicates one under a different
  parent, a rename that only changes case, and a destroy of a mailbox that is
  both non-empty and has children (the first refusal is unrecorded). The service
  does not depend on any of these.

#### Standard-client visibility (verified 2026-11-18)

A mailbox created through the Mailflow API and a message moved into it were
listed over IMAPS as `bob@mailflow.test`, using Python's `imaplib` with the mail
project's CA at `deploy/mail/data/tls/ca.pem`. The check mapped
`mail.mailflow.test` to loopback for the connection while still verifying the
certificate against the hostname:

```python
ctx = ssl.create_default_context(cafile="deploy/mail/data/tls/ca.pem")
m = imaplib.IMAP4_SSL("mail.mailflow.test", 993, ssl_context=ctx)
m.login(user, password)
m.list()                       # showed the mailbox created over HTTP
m.select('"IMAP Check <stamp>"', readonly=True)
m.search(None, "ALL")          # returned the message moved over HTTP
```

`LIST` showed the new mailbox without a subscription step, `SELECT` opened it,
and `SEARCH ALL` returned the moved message. After the message was moved back
and the mailbox deleted through the API, a second `LIST` no longer showed it.
The mailbox's role was absent, so the public model reports it as `custom`.

## Push notifications

`eventSourceUrl` is a template with three parameters:

| Parameter | Observed behavior |
|---|---|
| `types` | `*` for all types, or a comma-separated list such as `Email,Mailbox` |
| `closeafter` | `state` keeps the stream open until the client closes it; `no` was also accepted; `1` was rejected with `400 Invalid parameters` |
| `ping` | Seconds between keep-alive comments, e.g. `30` |

A working subscription, with an unprivileged account's credentials:

```
GET https://mail.mailflow.test/jmap/eventsource/?types=*&closeafter=state&ping=30
```

The response is a `text/event-stream` that stays open; no extra request header
was needed beyond the `Authorization` used for the rest of the API.

Observed events, each with an `event: state` line:

```
data: {"@type":"StateChange","changed":{"d":{"Email":"sbq"}}}
data: {"@type":"StateChange","changed":{"d":{"Mailbox":"sbu"}}}
```

`changed` is keyed by account id; the inner keys are the object types whose
state changed, and the values are opaque state strings to feed into a
`Foo/changes` call. Two changes were triggered from IMAP and observed here
within seconds: clearing a flag on a message (`Email`) and creating a mailbox
(`Mailbox`). This is enough for the Go service to learn "something changed for
this account" without polling every mailbox.

## Not verified

Recorded as gaps, not as behavior:

- **WebSocket push** (`wss://mail.mailflow.test/jmap/ws`) is advertised in the
  session but was not opened. Only Server-Sent Events were exercised.
- **Web Push / VAPID** subscriptions were not created.
- **Upload and download** URLs were read from the session but not used: no
  attachment was fetched or stored through JMAP blobs. Attachment fidelity was
  verified over IMAP instead.
- **OAuth** was not used; only HTTP Basic. Whether an OAuth flow is available on
  this release is unrecorded.
- **Calendars, contacts, Sieve, vacation response, quota, metadata, filenode,
  principals** are advertised and untouched. Mailflow does not need them.
- **`Email/import`** was not exercised. The service creates messages from parts
  instead, so no upload or MIME construction path exists.
- **`EmailSubmission/get` and `EmailSubmission/changes`** were not called; only
  `set` was exercised, and the service does not need to read submission state.
- **`Email/changes`, `Mailbox/changes`** were not called; the state strings were
  only observed, not followed.

## Pinned release

| Item | Value |
|---|---|
| Image | `ghcr.io/stalwartlabs/stalwart:v1.1.0` |
| Index digest | `sha256:f772aa6ef8cd8018969fa491640f1436b0270eb14e2c219b5f37f1a77020837e` |
| arm64 manifest | `sha256:eebae3f465d0bba7932b713d0cd9f53a6dafdd61ad185084d25397f714c5211d` |

The surface above belongs to this release. A different release must be
re-observed before the adapter relies on it; see
[`mail-infrastructure.md`](mail-infrastructure.md) for the upgrade procedure.
