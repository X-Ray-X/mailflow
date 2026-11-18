#!/usr/bin/env python3
"""Exercise the published contract against a running Mailflow API.

This is a verification tool, not a client: it deliberately inspects the two
segments of an identifier to report whether a cross-account request was the
awkward case (the same raw mail server identifier exists in both accounts).
Clients must treat identifiers as opaque.

Credentials come from the mail project's .env. Nothing is written back.

    ./app/scripts/verify-api.py --base http://127.0.0.1:8080
"""

import argparse
import base64
import json
import os
import sys
import time
import urllib.error
import urllib.request

DEFAULT_ENV = "deploy/mail/.env"
DEFAULT_BASE = "http://127.0.0.1:8080"

# Tokens that must never appear in a public payload. Mail content is ours, so a
# match here means the service leaked protocol or vendor detail, not that a
# message happened to discuss IMAP.
FORBIDDEN = [
    "urn:", "$seen", "$flagged", "$draft", "$hasattachment", "keywords",
    "blobid", "threadid", "messageid", "mailboxids", "accountid", "bodyvalues",
    "textbody", "notfound", "notcreated", "querystate", "sessionstate", "anchor",
    "email/", "mailbox/", "jmap", "stalwart", "mail.mailflow.test",
    "emailsubmission", "identity/", "invalidproperties", "onsuccessupdateemail",
    "alreadyexists", "mailboxhasemail", "mailboxhaschild", "issubscribed",
    "parentid", "mayrename", "maydelete", "ondestroyremoveemails",
    "norecipients", "forbiddenfrom",
]

DOCUMENTED_CODES = {
    "unauthorized", "not_found", "invalid_cursor", "invalid_request",
    "method_not_allowed", "service_unavailable", "internal",
    "conflict", "in_progress", "send_rejected", "already_exists",
    "mailbox_not_empty",
}


def read_env(path):
    values = {}
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                key, value = line.split("=", 1)
                values[key] = value
    return values


class Api:
    def __init__(self, base):
        self.base = base.rstrip("/")
        self.payloads = []

    def call(self, path, creds=None, method="GET", base64_auth=True, body=None, headers=None):
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(self.base + path, method=method, data=data)
        if creds is not None:
            token = base64.b64encode(f"{creds[0]}:{creds[1]}".encode()).decode()
            request.add_header("Authorization", "Basic " + token)
        request.add_header("Content-Type", "application/json")
        for name, value in (headers or {}).items():
            request.add_header(name, value)
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                body = response.read()
                self.payloads.append((path, body))
                return response.status, response.headers, json.loads(body or b"{}")
        except urllib.error.HTTPError as err:
            body = err.read()
            self.payloads.append((path, body))
            return err.code, err.headers, json.loads(body or b"{}")

    def raw(self, path, creds=None):
        """Fetch a binary response without trying to decode it as JSON."""
        request = urllib.request.Request(self.base + path)
        if creds is not None:
            token = base64.b64encode(f"{creds[0]}:{creds[1]}".encode()).decode()
            request.add_header("Authorization", "Basic " + token)
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                data = response.read()
                self.payloads.append((path, data))
                return response.status, response.headers, data
        except urllib.error.HTTPError as err:
            data = err.read()
            self.payloads.append((path, data))
            return err.code, err.headers, data


class Report:
    def __init__(self):
        self.failures = []

    def check(self, label, ok, detail=""):
        print(("PASS " if ok else "FAIL ") + label + (f" — {detail}" if detail else ""))
        if not ok:
            self.failures.append(label)


def b64(value):
    """Encode one identifier segment without padding (test only)."""
    return base64.urlsafe_b64encode(value.encode()).decode().rstrip("=")


def segments(identifier):
    """Split a Mailflow identifier into its two decoded segments (test only)."""
    prefix, _, payload = identifier.partition("_")
    parts = payload.split(".")
    if len(parts) != 2:
        return None, None
    decoded = [base64.urlsafe_b64decode(p + "=" * (-len(p) % 4)).decode() for p in parts]
    return decoded[0], decoded[1]


def imap_reply_headers(subject, creds, ca_path, host="mail.mailflow.test"):
    """Fetch a reply draft's threading headers over IMAP (verification only).

    The host name is kept for certificate verification while the connection is
    patched to loopback, the same way a standard client resolves this mail host
    on the homelab network. Returns None when the local CA is not available.
    """
    if not os.path.exists(ca_path):
        return None
    import email.policy
    import imaplib
    import socket
    import ssl

    real_getaddrinfo = socket.getaddrinfo

    def to_loopback(name, port, *args, **kwargs):
        return real_getaddrinfo("127.0.0.1" if name == host else name, port, *args, **kwargs)

    socket.getaddrinfo = to_loopback
    try:
        context = ssl.create_default_context(cafile=ca_path)
        mailbox = imaplib.IMAP4_SSL(host, 993, ssl_context=context)
        try:
            mailbox.login(creds[0], creds[1])
            mailbox.select("Drafts")
            # A JMAP create can take a moment to appear in the IMAP index, so
            # bound the wait rather than failing on a race.
            deadline = time.monotonic() + 10
            while True:
                found = mailbox.search(None, "SUBJECT", '"' + subject + '"')
                seq = found[1][0].split()[-1] if found[0] == "OK" and found[1] and found[1][0].strip() else None
                if seq is not None:
                    break
                if time.monotonic() >= deadline:
                    return {}
                time.sleep(0.5)
            fetched = mailbox.fetch(seq, "(BODY.PEEK[HEADER])")
            raw = next((part for part in fetched[1] if isinstance(part, tuple)), None)
            if raw is None:
                return {}
            message = email.message_from_bytes(raw[1], policy=email.policy.default)
            return {name: message.get(name) for name in ("In-Reply-To", "References")}
        finally:
            try:
                mailbox.logout()
            except Exception:
                pass
    finally:
        socket.getaddrinfo = real_getaddrinfo


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default=os.environ.get("MAILFLOW_BASE", DEFAULT_BASE))
    parser.add_argument("--env-file", default=DEFAULT_ENV)
    parser.add_argument("--ca-file", default="deploy/mail/data/tls/ca.pem",
                        help="mail CA used for the optional IMAP threading check")
    args = parser.parse_args()

    env = read_env(args.env_file)
    domain = env["MAIL_DOMAIN"]
    accounts = {
        "bob": (f"{env['MAIL_ACCOUNT_BOB']}@{domain}", env["MAIL_ACCOUNT_BOB_PASSWORD"]),
        "alice": (f"{env['MAIL_ACCOUNT_ALICE']}@{domain}", env["MAIL_ACCOUNT_ALICE_PASSWORD"]),
    }
    api = Api(args.base)
    report = Report()

    # --- health and authentication -----------------------------------------
    status, _, body = api.call("/healthz")
    report.check("health needs no credentials", status == 200 and body.get("status") == "ok", f"status={status}")

    status, headers, body = api.call("/v1/mailboxes")
    report.check("missing credentials are refused", status == 401 and body.get("code") == "unauthorized",
                 f"status={status}")
    report.check("refusal advertises Basic", "Basic" in (headers.get("WWW-Authenticate") or ""),
                 headers.get("WWW-Authenticate") or "")

    status, _, body = api.call("/v1/mailboxes", (accounts["bob"][0], "definitely-wrong"))
    report.check("wrong password is refused", status == 401 and body.get("code") == "unauthorized",
                 f"status={status}")

    # --- mailboxes ----------------------------------------------------------
    boxes, first_message = {}, {}
    for name, creds in accounts.items():
        status, _, body = api.call("/v1/mailboxes", creds)
        boxes[name] = body.get("mailboxes") or []
        roles = sorted({box["role"] for box in boxes[name]})
        report.check(f"{name}: mailbox list", status == 200 and boxes[name], f"status={status} count={len(boxes[name])}")
        report.check(f"{name}: standard roles", {"inbox", "sent", "drafts", "trash", "junk"} <= set(roles), str(roles))
        report.check(f"{name}: identifiers are Mailflow mailbox ids",
                     all(box["id"].startswith("mb1_") for box in boxes[name]), "")
        report.check(f"{name}: identifiers carry no address",
                     not any(domain in box["id"] or name in box["id"] for box in boxes[name]), "")

    # The mail server numbers objects per account, so the standard mailboxes
    # collide. That is what makes the cross-account assertions meaningful.
    bob_inbox = next(b for b in boxes["bob"] if b["role"] == "inbox")
    bob_trash = next(b for b in boxes["bob"] if b["role"] == "trash")
    alice_inbox = next(b for b in boxes["alice"] if b["role"] == "inbox")
    bob_account, bob_raw_inbox = segments(bob_inbox["id"])
    alice_account, alice_raw_inbox = segments(alice_inbox["id"])
    report.check("the two accounts are distinct", bob_account != alice_account,
                 f"bob={bob_account} alice={alice_account}")
    report.check("the inbox identifiers collide at the mail server",
                 bob_raw_inbox == alice_raw_inbox,
                 f"bob={bob_raw_inbox} alice={alice_raw_inbox}")

    # --- paging -------------------------------------------------------------
    seen, received, cursor, pages = [], [], "", 0
    while True:
        path = f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=2"
        if cursor:
            path += f"&cursor={cursor}"
        status, _, page = api.call(path, accounts["bob"])
        report.check(f"bob: page {pages + 1}", status == 200, f"status={status}")
        if status != 200:
            break
        pages += 1
        batch = page.get("messages") or []
        seen.extend(item["id"] for item in batch)
        received.extend(item["receivedAt"] for item in batch)
        print(f"     page {pages}: {len(batch)} message(s), hasMore={page.get('hasMore')}")
        if not page.get("hasMore"):
            report.check("bob: the last page advertises no cursor", "nextCursor" not in page,
                         json.dumps(page.get("nextCursor")))
            break
        report.check(f"bob: page {pages} advertises a cursor", bool(page.get("nextCursor")), "")
        cursor = page.get("nextCursor")
        if pages > 10:
            report.check("bob: paging terminated", False, "more than ten pages")
            break

    report.check("bob: paged to exhaustion", len(seen) == bob_inbox["total"],
                 f"messages={len(seen)} total={bob_inbox['total']}")
    report.check("bob: no repeated message", len(seen) == len(set(seen)), f"unique={len(set(seen))}")
    report.check("bob: newest first", received == sorted(received, reverse=True), str(received))

    # --- one message body ---------------------------------------------------
    status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=1", accounts["bob"])
    summary = listing["messages"][0]
    status, _, detail = api.call(f"/v1/messages/{summary['id']}", accounts["bob"])
    report.check("bob: message detail", status == 200 and detail.get("subject") == summary["subject"],
                 f"status={status}")
    report.check("bob: body is present", bool(detail.get("text")), repr(detail.get("text"))[:60])
    report.check("bob: body is not truncated", detail.get("textTruncated") is False, "")
    report.check("bob: detail identifiers agree", detail.get("id") == summary["id"], "")

    # --- identifiers are scoped to their account ----------------------------
    # Both directions, with identifiers whose raw value exists in both accounts.
    status, _, body = api.call(f"/v1/mailboxes/{alice_inbox['id']}/messages", accounts["bob"])
    report.check("bob cannot use alice's inbox identifier",
                 status == 404 and body.get("code") == "not_found", f"status={status} body={body}")
    status, _, body = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages", accounts["alice"])
    report.check("alice cannot use bob's inbox identifier",
                 status == 404 and body.get("code") == "not_found", f"status={status} body={body}")

    # Alice's sent mailbox holds the probe messages; her inbox is empty.
    alice_sent = next(b for b in boxes["alice"] if b["role"] == "sent")
    status, _, alice_listing = api.call(f"/v1/mailboxes/{alice_sent['id']}/messages?limit=1", accounts["alice"])
    report.check("alice: sent listing", status == 200 and alice_listing.get("messages"), f"status={status}")
    alice_message = alice_listing["messages"][0]
    bob_raw_message = segments(summary["id"])[1]
    alice_raw_message = segments(alice_message["id"])[1]
    status, _, body = api.call(f"/v1/messages/{alice_message['id']}", accounts["bob"])
    report.check("bob cannot use alice's message identifier",
                 status == 404 and body.get("code") == "not_found", f"status={status} body={body}")
    print(f"     raw message ids: bob={bob_raw_message} alice={alice_raw_message} "
          f"collide={bob_raw_message == alice_raw_message}")
    status, _, body = api.call(f"/v1/messages/{summary['id']}", accounts["alice"])
    report.check("alice cannot use bob's message identifier",
                 status == 404 and body.get("code") == "not_found", f"status={status} body={body}")

    status, _, bob_page = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=2", accounts["bob"])
    cursor = bob_page["nextCursor"]
    status, _, body = api.call(f"/v1/mailboxes/{alice_inbox['id']}/messages?limit=2&cursor={cursor}",
                               accounts["alice"])
    report.check("a cursor is refused by another account",
                 status == 400 and body.get("code") == "invalid_cursor", f"status={status} body={body}")

    # --- malformed and unknown identifiers ---------------------------------
    status, _, body = api.call(f"/v1/mailboxes/{alice_inbox['id']}/messages?cursor=nonsense", accounts["alice"])
    report.check("a malformed cursor is a bad request",
                 status == 400 and body.get("code") == "invalid_cursor", f"status={status}")

    unknown_mailbox = f"mb1_{b64(alice_account)}.{b64('zzzzzz')}"
    status, _, body = api.call(f"/v1/mailboxes/{unknown_mailbox}/messages", accounts["alice"])
    report.check("an unknown mailbox is not found",
                 status == 404 and body.get("code") == "not_found", f"status={status}")

    unknown_message = f"msg1_{b64(alice_account)}.{b64('zzzzzz')}"
    status, _, body = api.call(f"/v1/messages/{unknown_message}", accounts["alice"])
    report.check("an unknown message is not found",
                 status == 404 and body.get("code") == "not_found", f"status={status}")

    status, _, body = api.call("/v1/messages/not-an-identifier", accounts["alice"])
    report.check("a malformed identifier is not found",
                 status == 404 and body.get("code") == "not_found", f"status={status}")

    # --- sending and flags --------------------------------------------------
    alice_creds, bob_creds = accounts["alice"], accounts["bob"]
    stamp = int(time.time() * 1000)
    subject = f"Mailflow verifier {stamp}"
    key = f"verify-{stamp}"
    send_body = {
        "to": [{"name": "Bob", "email": bob_creds[0]}],
        "subject": subject,
        "text": "Sent by app/scripts/verify-api.py.\n",
    }

    status, headers, sent = api.call("/v1/messages", alice_creds, method="POST",
                                     body=send_body, headers={"Idempotency-Key": key})
    sent_id = sent.get("id") or ""
    report.check("alice: send accepted",
                 status == 201 and sent_id.startswith("msg1_"), f"status={status} body={sent}")
    report.check("a first send is not a replay",
                 not (headers.get("Idempotency-Replayed") or ""),
                 headers.get("Idempotency-Replayed") or "")

    status, _, detail = api.call(f"/v1/messages/{sent_id}", alice_creds)
    report.check("alice: the sent copy is readable",
                 status == 200 and detail.get("subject") == subject, f"status={status}")

    status, headers, replay = api.call("/v1/messages", alice_creds, method="POST",
                                       body=send_body, headers={"Idempotency-Key": key})
    report.check("the same key replays the original result",
                 status == 201 and replay.get("id") == sent_id
                 and (headers.get("Idempotency-Replayed") or "") == "true",
                 f"status={status} id={replay.get('id')} "
                 f"replay={headers.get('Idempotency-Replayed')}")

    changed = dict(send_body, subject=subject + " (changed)")
    status, _, body = api.call("/v1/messages", alice_creds, method="POST",
                               body=changed, headers={"Idempotency-Key": key})
    report.check("a reused key with a different request is a conflict",
                 status == 409 and body.get("code") == "conflict", f"status={status} body={body}")

    status, _, body = api.call("/v1/messages", alice_creds, method="POST", body=send_body)
    report.check("a send without a key is refused",
                 status == 400 and body.get("code") == "invalid_request",
                 f"status={status} body={body}")

    foreign = dict(send_body, to=[{"email": "nobody@example.invalid"}])
    status, _, body = api.call("/v1/messages", alice_creds, method="POST", body=foreign,
                               headers={"Idempotency-Key": key + "-foreign"})
    report.check("a recipient outside the local domain is refused",
                 status == 400 and body.get("code") == "invalid_request",
                 f"status={status} body={body}")

    status, _, listing = api.call(f"/v1/mailboxes/{alice_sent['id']}/messages?limit=100",
                                  alice_creds)
    report.check("alice: the sent copy is in the sent mailbox",
                 status == 200 and any(m["id"] == sent_id for m in (listing.get("messages") or [])),
                 f"status={status}")

    status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=100",
                                  bob_creds)
    received = next((m for m in (listing.get("messages") or []) if m.get("subject") == subject), None)
    report.check("bob: the message arrived in the inbox",
                 status == 200 and received is not None, f"status={status}")

    if received is not None:
        received_id = received["id"]
        status, _, updated = api.call(f"/v1/messages/{received_id}", bob_creds, method="PATCH",
                                      body={"read": True, "starred": True})
        report.check("bob: flags can be set",
                     status == 200 and updated.get("read") is True
                     and updated.get("starred") is True, f"status={status} body={updated}")

        status, _, again = api.call(f"/v1/messages/{received_id}", bob_creds, method="PATCH",
                                    body={"read": True, "starred": True})
        report.check("a repeated flag patch is a no-op",
                     status == 200 and again.get("read") is True
                     and again.get("starred") is True, f"status={status} body={again}")

        status, _, body = api.call(f"/v1/messages/{received_id}", bob_creds, method="PATCH",
                                   body={"seen": True})
        report.check("an unknown flag field is refused",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        status, _, body = api.call(f"/v1/messages/{sent_id}", bob_creds, method="PATCH",
                                   body={"read": False})
        report.check("bob cannot flag alice's message",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")

    # A flag change must not disturb attachment state. If the mailbox holds no
    # attachment right now, the live task check covered this instead.
    attachment = next((m for m in (listing.get("messages") or []) if m.get("hasAttachment")), None)
    if attachment is not None:
        status, _, updated = api.call(f"/v1/messages/{attachment['id']}", bob_creds,
                                      method="PATCH", body={"read": True, "starred": True})
        report.check("a flag change preserves hasAttachment",
                     status == 200 and updated.get("hasAttachment") is True,
                     f"status={status} body={updated}")
    else:
        print("SKIP no attachment in bob's inbox to check flag preservation")

    # --- drafts and replies -------------------------------------------------
    bob_drafts = next(b for b in boxes["bob"] if b["role"] == "drafts")
    bob_sent = next(b for b in boxes["bob"] if b["role"] == "sent")

    def drafts_contains(identifier, mailbox=bob_drafts):
        status, _, listing = api.call(f"/v1/mailboxes/{mailbox['id']}/messages?limit=100", bob_creds)
        return status == 200 and any(m["id"] == identifier for m in (listing.get("messages") or []))

    created_draft_ids = []
    try:
        status, _, empty = api.call("/v1/drafts", bob_creds, method="POST", body={})
        empty_id = empty.get("id") or ""
        created_draft_ids.append(empty_id)
        report.check("bob: an empty draft can be saved",
                     status == 201 and empty_id.startswith("msg1_"), f"status={status} body={empty}")
        report.check("an empty draft is listed in drafts", drafts_contains(empty_id),
                     f"id={empty_id}")

        empty_key = f"verify-draft-{stamp}"
        status, _, body = api.call(f"/v1/drafts/{empty_id}/send", bob_creds, method="POST",
                                   headers={"Idempotency-Key": empty_key})
        report.check("an empty draft cannot be submitted",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")
        report.check("a refused draft stays in drafts", drafts_contains(empty_id), "")

        draft_subject = f"Draft verifier {stamp}"
        draft_body = {
            "to": [{"name": "Alice", "email": alice_creds[0]}],
            "subject": draft_subject,
            "text": "Saved by app/scripts/verify-api.py.\n",
        }
        status, _, draft = api.call("/v1/drafts", bob_creds, method="POST", body=draft_body)
        draft_id = draft.get("id") or ""
        created_draft_ids.append(draft_id)
        report.check("bob: a complete draft can be saved",
                     status == 201 and draft_id.startswith("msg1_"), f"status={status} body={draft}")
        report.check("a complete draft is listed in drafts", drafts_contains(draft_id), "")
        status, _, detail = api.call(f"/v1/messages/{draft_id}", bob_creds)
        report.check("a draft's body is readable",
                     status == 200 and detail.get("text") == draft_body["text"]
                     and detail.get("subject") == draft_subject,
                     f"status={status} subject={detail.get('subject')!r}")

        status, _, body = api.call(f"/v1/drafts/{draft_id}/send", bob_creds, method="POST")
        report.check("a draft submission needs a key",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        # The empty-draft refusal released this key, so the same key may now be
        # used for a submission that succeeds.
        status, headers, submitted = api.call(f"/v1/drafts/{draft_id}/send", bob_creds,
                                              method="POST",
                                              headers={"Idempotency-Key": empty_key})
        report.check("a draft can be submitted",
                     status == 201 and submitted.get("id") == draft_id,
                     f"status={status} body={submitted}")
        report.check("a first draft submission is not a replay",
                     not (headers.get("Idempotency-Replayed") or ""),
                     headers.get("Idempotency-Replayed") or "")

        status, headers, replay = api.call(f"/v1/drafts/{draft_id}/send", bob_creds,
                                           method="POST",
                                           headers={"Idempotency-Key": empty_key})
        report.check("a draft submission replay sends nothing",
                     status == 201 and replay.get("id") == draft_id
                     and (headers.get("Idempotency-Replayed") or "") == "true",
                     f"status={status} body={replay}")

        status, _, body = api.call(f"/v1/drafts/{empty_id}/send", bob_creds, method="POST",
                                   headers={"Idempotency-Key": empty_key})
        report.check("a key reused for another draft is a conflict",
                     status == 409 and body.get("code") == "conflict",
                     f"status={status} body={body}")

        status, _, body = api.call(f"/v1/drafts/{draft_id}/send", bob_creds, method="POST",
                                   headers={"Idempotency-Key": f"verify-again-{stamp}"})
        report.check("a sent message cannot be submitted again",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        status, _, body = api.call(f"/v1/drafts/{empty_id}/send", alice_creds, method="POST",
                                   headers={"Idempotency-Key": f"verify-foreign-{stamp}"})
        report.check("alice cannot submit bob's draft",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{empty_id}", alice_creds)
        report.check("alice cannot read bob's draft",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")

        report.check("a submitted draft leaves drafts", not drafts_contains(draft_id), "")
        report.check("a submitted draft appears in sent", drafts_contains(draft_id, bob_sent), "")
        status, _, detail = api.call(f"/v1/messages/{draft_id}", bob_creds)
        report.check("a submitted draft keeps its identifier and content",
                     status == 200 and detail.get("id") == draft_id
                     and detail.get("text") == draft_body["text"],
                     f"status={status}")

        reply_source = received["id"] if received is not None else summary["id"]
        reply_text = f"Reply verifier {stamp}"
        status, _, reply = api.call(f"/v1/messages/{reply_source}/reply", bob_creds,
                                    method="POST", body={"text": reply_text})
        reply_id = reply.get("id") or ""
        created_draft_ids.append(reply_id)
        report.check("bob: a reply can be composed",
                     status == 201 and reply_id.startswith("msg1_"), f"status={status} body={reply}")
        report.check("a reply is a draft in drafts", drafts_contains(reply_id), "")
        status, _, detail = api.call(f"/v1/messages/{reply_id}", bob_creds)
        expected_subject = "Re: " + subject
        report.check("a reply has the original sender, one prefix, and the caller's body",
                     status == 200 and detail.get("subject") == expected_subject
                     and detail.get("text") == reply_text
                     and len(detail.get("to") or []) == 1
                     and detail["to"][0]["email"] == alice_creds[0],
                     f"status={status} detail={detail}")

        status, _, second = api.call(f"/v1/messages/{reply_id}/reply", bob_creds,
                                     method="POST", body={"text": "second"})
        created_draft_ids.append(second.get("id") or "")
        status, _, second_detail = api.call(f"/v1/messages/{second.get('id')}", bob_creds)
        report.check("a reply to a reply does not add a second prefix",
                     status == 200 and second_detail.get("subject") == expected_subject,
                     f"status={status} subject={second_detail.get('subject')!r}")

        status, _, body = api.call(f"/v1/messages/{reply_source}/reply", alice_creds,
                                   method="POST", body={"text": "hi"})
        report.check("alice cannot reply to bob's message",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{alice_message['id']}/reply", bob_creds,
                                   method="POST", body={"text": "hi"})
        report.check("bob cannot reply to alice's message",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{reply_source}/reply", bob_creds,
                                   method="POST", body={"text": "hi", "quote": True})
        report.check("a reply with an unknown field is refused",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        imap_headers = imap_reply_headers(expected_subject, bob_creds, args.ca_file)
        if imap_headers is None:
            print("SKIP threading headers: local mail CA is not available")
        else:
            report.check("a reply draft carries In-Reply-To", bool(imap_headers.get("In-Reply-To")),
                         str(imap_headers))
            report.check("a reply draft carries References", bool(imap_headers.get("References")),
                         str(imap_headers))
    finally:
        for identifier in set(created_draft_ids):
            if identifier:
                api.call(f"/v1/messages/{identifier}", bob_creds, method="DELETE")

    # --- attachments --------------------------------------------------------
    status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=100", bob_creds)
    attachment_source = next(
        (m for m in (listing.get("messages") or [])
         if m.get("hasAttachment") and m.get("subject") == "Mailflow verification message"),
        None)
    if attachment_source is None:
        print("SKIP attachments: the demo attachment message is not in bob's inbox")
    else:
        status, _, meta = api.call(f"/v1/messages/{attachment_source['id']}/attachments", bob_creds)
        files = meta.get("attachments") or []
        report.check("bob: attachment metadata is listed",
                     status == 200 and len(files) == 1 and files[0].get("position") == 0
                     and files[0].get("filename") == "payload.bin",
                     f"status={status} files={files}")
        status, headers, data = api.raw(f"/v1/messages/{attachment_source['id']}/attachments/0", bob_creds)
        downloaded = status == 200 and (not files or len(data) == files[0]["size"])
        report.check("bob: an attachment downloads", downloaded,
                     f"status={status} bytes={len(data)}")
        report.check("a download names the file and is not cached",
                     "attachment" in (headers.get("Content-Disposition") or "").lower()
                     and (headers.get("Cache-Control") or "") == "no-store"
                     and (headers.get("X-Content-Type-Options") or "") == "nosniff"
                     and (headers.get("Content-Type") or "").startswith("application/"),
                     str(dict(headers)))
        status, _, body = api.call(f"/v1/messages/{attachment_source['id']}/attachments/abc", bob_creds)
        report.check("a malformed attachment position is refused",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{attachment_source['id']}/attachments/99", bob_creds)
        report.check("an unknown attachment position is not found",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{attachment_source['id']}/attachments", alice_creds)
        report.check("alice cannot list bob's attachments",
                     status == 404 and body.get("code") == "not_found",
                     f"status={status} body={body}")
        status, _, body = api.raw(f"/v1/messages/{attachment_source['id']}/attachments/0", alice_creds)
        report.check("alice cannot download bob's attachment",
                     status == 404 and json.loads(body).get("code") == "not_found",
                     f"status={status} body={body[:120]!r}")

    # --- search -------------------------------------------------------------
    status, _, page = api.call("/v1/search?q=Mailflow&limit=2", bob_creds)
    first = page.get("messages") or []
    report.check("bob: a text search returns summaries",
                 status == 200 and first and all(m["id"].startswith("msg1_") for m in first),
                 f"status={status} count={len(first)}")
    report.check("a search advertises another page", bool(page.get("nextCursor")) == bool(page.get("hasMore")),
                 f"hasMore={page.get('hasMore')} cursor={bool(page.get('nextCursor'))}")
    if page.get("hasMore") and page.get("nextCursor"):
        status, _, second = api.call("/v1/search?q=Mailflow&limit=2&cursor=" + page["nextCursor"], bob_creds)
        ids1 = {m["id"] for m in first}
        ids2 = {m["id"] for m in (second.get("messages") or [])}
        report.check("search paging does not repeat a message", status == 200 and not (ids1 & ids2),
                     f"second={len(ids2)}")

    status, _, body = api.call("/v1/search?q=verify-api", bob_creds)
    report.check("a search matches body text", status == 200 and bool(body.get("messages")),
                 f"status={status} count={len(body.get('messages') or [])}")
    status, _, body = api.call("/v1/search?q=zzzz-no-such-text", bob_creds)
    report.check("a search with no matches is empty",
                 status == 200 and body.get("messages") == [] and body.get("hasMore") is False,
                 f"status={status} body={body}")
    status, _, body = api.call("/v1/search", bob_creds)
    report.check("an empty query is refused",
                 status == 400 and body.get("code") == "invalid_request",
                 f"status={status} body={body}")
    status, _, body = api.call("/v1/search?q=Mailflow&inMailbox=x", bob_creds)
    report.check("an unknown search parameter is refused",
                 status == 400 and body.get("code") == "invalid_request",
                 f"status={status} body={body}")
    status, _, body = api.call("/v1/search?q=Mailflow&mailboxId=" + alice_inbox["id"], bob_creds)
    report.check("bob cannot search alice's mailbox",
                 status == 404 and body.get("code") == "not_found",
                 f"status={status} body={body}")
    status, _, body = api.call("/v1/search?q=Mailflow&mailboxId=" + bob_inbox["id"], bob_creds)
    report.check("a mailbox restriction works",
                 status == 200 and isinstance(body.get("messages"), list),
                 f"status={status}")

    # --- mailbox organization ----------------------------------------------
    # Every object created here is removed again, including on a failed check.
    org_name = f"Verify {stamp}"
    renamed_name = f"{org_name} Work"
    other_name = f"Verify Other {stamp}"
    created_box = created_other = None
    moved_id = None
    trashed_id = None
    try:
        status, _, created = api.call("/v1/mailboxes", bob_creds, method="POST",
                                      body={"name": org_name})
        created_box = created.get("id") or None
        report.check("bob: a mailbox can be created",
                     status == 201 and (created_box or "").startswith("mb1_"),
                     f"status={status} body={created}")

        status, _, listing = api.call("/v1/mailboxes", bob_creds)
        listed = next((b for b in (listing.get("mailboxes") or []) if b["id"] == created_box), None)
        report.check("the new mailbox is listed as custom",
                     status == 200 and listed is not None and listed.get("role") == "custom",
                     str(listed))

        status, _, body = api.call("/v1/mailboxes", bob_creds, method="POST",
                                   body={"name": org_name})
        report.check("a duplicate mailbox name is refused",
                     status == 409 and body.get("code") == "already_exists",
                     f"status={status} body={body}")

        status, _, body = api.call("/v1/mailboxes", bob_creds, method="POST",
                                   body={"name": org_name.upper()})
        report.check("a duplicate name is refused ignoring case",
                     status == 409 and body.get("code") == "already_exists",
                     f"status={status} body={body}")

        for bad_name in ("", "   ", "bad/name", "bad\nname", "x" * 256):
            status, _, body = api.call("/v1/mailboxes", bob_creds, method="POST",
                                       body={"name": bad_name})
            report.check(f"an invalid mailbox name is refused ({bad_name[:12]!r})",
                         status == 400 and body.get("code") == "invalid_request",
                         f"status={status} body={body}")

        status, _, body = api.call("/v1/mailboxes", bob_creds, method="POST",
                                   body={"name": f"Child {stamp}", "parentId": created_box})
        report.check("a mailbox request cannot name a parent",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        status, _, body = api.call(f"/v1/mailboxes/{created_box}", bob_creds, method="PATCH",
                                   body={"name": renamed_name})
        report.check("a custom mailbox can be renamed", status == 204,
                     f"status={status} body={body}")

        status, _, listing = api.call("/v1/mailboxes", bob_creds)
        listed = next((b for b in (listing.get("mailboxes") or []) if b["id"] == created_box), None)
        report.check("the renamed mailbox keeps its identifier",
                     listed is not None and listed.get("name") == renamed_name, str(listed))

        status, _, body = api.call(f"/v1/mailboxes/{created_box}", bob_creds, method="PATCH",
                                   body={"name": renamed_name})
        report.check("renaming to the current name succeeds", status == 204,
                     f"status={status} body={body}")

        status, _, created = api.call("/v1/mailboxes", bob_creds, method="POST",
                                      body={"name": other_name})
        created_other = created.get("id") or None
        report.check("a second custom mailbox can be created",
                     status == 201 and (created_other or "").startswith("mb1_"),
                     f"status={status} body={created}")

        status, _, body = api.call(f"/v1/mailboxes/{created_other}", bob_creds, method="PATCH",
                                   body={"name": renamed_name})
        report.check("a rename into a duplicate name is refused",
                     status == 409 and body.get("code") == "already_exists",
                     f"status={status} body={body}")

        status, _, body = api.call(f"/v1/mailboxes/{bob_inbox['id']}", bob_creds,
                                   method="PATCH", body={"name": "Nope"})
        report.check("a standard mailbox cannot be renamed",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        status, _, body = api.call(f"/v1/mailboxes/{bob_inbox['id']}", bob_creds, method="DELETE")
        report.check("a standard mailbox cannot be deleted",
                     status == 400 and body.get("code") == "invalid_request",
                     f"status={status} body={body}")

        # Move a message into the new mailbox, preserving flags and attachment
        # state, then refuse to delete the mailbox while it holds the message.
        moved_id = attachment["id"] if attachment is not None else (
            received["id"] if received is not None else summary["id"])
        status, _, moved = api.call(f"/v1/messages/{moved_id}", bob_creds, method="PATCH",
                                    body={"mailboxId": created_box, "starred": True})
        report.check("a message can be moved",
                     status == 200 and moved.get("id") == moved_id,
                     f"status={status} body={moved}")
        report.check("a move preserves flags", moved.get("starred") is True, str(moved))
        if attachment is not None:
            report.check("a move preserves attachment state",
                         moved.get("hasAttachment") is True, str(moved))

        status, _, listing = api.call(f"/v1/mailboxes/{created_box}/messages?limit=100", bob_creds)
        report.check("the moved message is in the target mailbox",
                     status == 200 and any(m["id"] == moved_id for m in (listing.get("messages") or [])),
                     f"status={status}")

        status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=100", bob_creds)
        report.check("the moved message left the source mailbox",
                     status == 200 and not any(m["id"] == moved_id for m in (listing.get("messages") or [])),
                     f"status={status}")

        status, _, again = api.call(f"/v1/messages/{moved_id}", bob_creds, method="PATCH",
                                    body={"mailboxId": created_box})
        report.check("moving to the current mailbox is a no-op",
                     status == 200 and again.get("id") == moved_id, f"status={status} body={again}")

        status, _, body = api.call(f"/v1/mailboxes/{created_box}", bob_creds, method="DELETE")
        report.check("a mailbox holding a message is not deleted",
                     status == 409 and body.get("code") == "mailbox_not_empty",
                     f"status={status} body={body}")

        status, _, restored = api.call(f"/v1/messages/{moved_id}", bob_creds, method="PATCH",
                                       body={"mailboxId": bob_inbox["id"]})
        report.check("a moved message can be moved back",
                     status == 200 and restored.get("id") == moved_id,
                     f"status={status} body={restored}")
        status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=100", bob_creds)
        report.check("the restored message is in the inbox",
                     status == 200 and any(m["id"] == moved_id for m in (listing.get("messages") or [])),
                     f"status={status}")
        moved_id = None

        status, _, body = api.call(f"/v1/mailboxes/{created_box}", bob_creds, method="DELETE")
        report.check("an empty custom mailbox is deleted", status == 204,
                     f"status={status} body={body}")
        created_box = None

        # Delete is a soft delete to trash, is repeatable, and is reversible.
        status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=100", bob_creds)
        candidates = [m["id"] for m in (listing.get("messages") or []) if m["id"] != moved_id]
        if candidates:
            trashed_id = candidates[0]
            status, _, body = api.call(f"/v1/messages/{trashed_id}", bob_creds, method="DELETE")
            report.check("a message can be deleted", status == 204,
                         f"status={status} body={body}")

            status, _, listing = api.call(f"/v1/mailboxes/{bob_trash['id']}/messages?limit=100", bob_creds)
            report.check("delete moves the message to trash",
                         status == 200 and any(m["id"] == trashed_id for m in (listing.get("messages") or [])),
                         f"status={status}")

            status, _, body = api.call(f"/v1/messages/{trashed_id}", bob_creds, method="DELETE")
            report.check("a repeated delete succeeds", status == 204,
                         f"status={status} body={body}")

            status, _, restored = api.call(f"/v1/messages/{trashed_id}", bob_creds, method="PATCH",
                                           body={"mailboxId": bob_inbox["id"]})
            report.check("a deleted message can be restored",
                         status == 200 and restored.get("id") == trashed_id,
                         f"status={status} body={restored}")
            status, _, listing = api.call(f"/v1/mailboxes/{bob_inbox['id']}/messages?limit=100", bob_creds)
            report.check("the restored message is in the inbox",
                         status == 200 and any(m["id"] == trashed_id for m in (listing.get("messages") or [])),
                         f"status={status}")
            trashed_id = None
        else:
            print("SKIP no second message in bob's inbox to check soft deletion")

        # Cross-account identifiers are refused before the mail server resolves
        # them, for mailboxes and for message moves and deletes.
        status, _, body = api.call(f"/v1/mailboxes/{alice_inbox['id']}", bob_creds,
                                   method="PATCH", body={"name": "Nope"})
        report.check("bob cannot rename alice's mailbox",
                     status == 404 and body.get("code") == "not_found", f"status={status} body={body}")
        status, _, body = api.call(f"/v1/mailboxes/{alice_inbox['id']}", bob_creds, method="DELETE")
        report.check("bob cannot delete alice's mailbox",
                     status == 404 and body.get("code") == "not_found", f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{summary['id']}", bob_creds, method="PATCH",
                                   body={"mailboxId": alice_inbox["id"]})
        report.check("bob cannot move a message into alice's mailbox",
                     status == 404 and body.get("code") == "not_found", f"status={status} body={body}")
        status, _, body = api.call(f"/v1/messages/{alice_message['id']}", bob_creds, method="DELETE")
        report.check("bob cannot delete alice's message",
                     status == 404 and body.get("code") == "not_found", f"status={status} body={body}")
    finally:
        if moved_id:
            api.call(f"/v1/messages/{moved_id}", bob_creds, method="PATCH",
                     body={"mailboxId": bob_inbox["id"]})
        if trashed_id:
            api.call(f"/v1/messages/{trashed_id}", bob_creds, method="PATCH",
                     body={"mailboxId": bob_inbox["id"]})
        for box_id in (created_box, created_other):
            if box_id:
                api.call(f"/v1/mailboxes/{box_id}", bob_creds, method="DELETE")

    # --- methods and paths --------------------------------------------------
    status, headers, body = api.call("/v1/mailboxes", accounts["bob"], method="PUT")
    report.check("a write method is refused",
                 status == 405 and body.get("code") == "method_not_allowed", f"status={status}")
    report.check("the refusal names the allowed methods", "GET" in (headers.get("Allow") or ""),
                 headers.get("Allow") or "")

    status, _, body = api.call("/v1/nothing-here", accounts["bob"])
    report.check("an unknown path is a JSON not-found",
                 status == 404 and body.get("code") == "not_found", f"status={status}")

    # --- replaceability scan (task 5.2) ------------------------------------
    for path, payload in api.payloads:
        text = payload.decode("utf-8", "replace").lower()
        for token in FORBIDDEN:
            if token in text:
                report.check(f"no vendor token {token!r} in {path}", False, payload[:200].decode("utf-8", "replace"))
        if '"code"' in text:
            code = json.loads(payload).get("code")
            report.check(f"documented error code in {path}", code in DOCUMENTED_CODES, str(code))

    print()
    print(f"payloads scanned: {len(api.payloads)}")
    print("FAILURES:", report.failures if report.failures else "none")
    return 1 if report.failures else 0


if __name__ == "__main__":
    sys.exit(main())
