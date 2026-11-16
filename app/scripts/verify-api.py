#!/usr/bin/env python3
"""Exercise the published read contract against a running Mailflow API.

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
    "textbody", "notfound", "querystate", "sessionstate", "anchor",
    "email/", "mailbox/", "jmap", "stalwart", "mail.mailflow.test",
]

DOCUMENTED_CODES = {
    "unauthorized", "not_found", "invalid_cursor", "invalid_request",
    "method_not_allowed", "service_unavailable", "internal",
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

    def call(self, path, creds=None, method="GET", base64_auth=True):
        request = urllib.request.Request(self.base + path, method=method)
        if creds is not None:
            token = base64.b64encode(f"{creds[0]}:{creds[1]}".encode()).decode()
            request.add_header("Authorization", "Basic " + token)
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                body = response.read()
                self.payloads.append((path, body))
                return response.status, response.headers, json.loads(body or b"{}")
        except urllib.error.HTTPError as err:
            body = err.read()
            self.payloads.append((path, body))
            return err.code, err.headers, json.loads(body or b"{}")


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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default=os.environ.get("MAILFLOW_BASE", DEFAULT_BASE))
    parser.add_argument("--env-file", default=DEFAULT_ENV)
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

    # --- methods and paths --------------------------------------------------
    status, headers, body = api.call("/v1/mailboxes", accounts["bob"], method="POST")
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
