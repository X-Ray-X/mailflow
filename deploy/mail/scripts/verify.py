#!/usr/bin/env python3
"""Exercise the Mailflow mail infrastructure through standard protocol clients.

Uses only the Python standard library (imaplib, smtplib, ssl, email) so the
checks are the same protocols a desktop or mobile client speaks. Certificates
are verified against the internal CA with hostname checking left on: nothing
here disables verification.

Two phases:

  full         create state through real client sessions and assert the
               mailbox-access scenarios.
  persistence  assert that state recorded by `full` is still present after the
               stack was restarted or the container was recreated. Run by
               scripts/verify.sh, which performs the restart.

The script connects to --addr (default 127.0.0.1) but presents --sni-name as the
TLS server name, so certificate validation exercises the documented hostname
even when the test host has no DNS entry for it.
"""

import argparse
import base64
import email
import email.policy
import hashlib
import http.client
import imaplib
import json
import os
import re
import smtplib
import socket
import ssl
import sys
import time
from email.message import EmailMessage
from email.utils import make_msgid

TIMEOUT = 15


class CheckFailure(Exception):
    pass


def need(name):
    value = os.environ.get(name)
    if not value:
        sys.exit(f"error: environment variable {name} is not set")
    return value


class IMAP(imaplib.IMAP4):
    """IMAP4 over TLS with an explicit TLS server name for SNI and verification."""

    def __init__(self, addr, port, sni_name, context):
        self._sni_name = sni_name
        self._context = context
        self._addr = addr
        super().__init__(addr, port, timeout=TIMEOUT)

    def _create_socket(self, timeout):
        sock = socket.create_connection((self._addr, self.port), timeout)
        return self._context.wrap_socket(sock, server_hostname=self._sni_name)


class Submission(smtplib.SMTP_SSL):
    """SMTP submission over implicit TLS with an explicit TLS server name."""

    def __init__(self, addr, port, sni_name, context):
        self._sni_name = sni_name
        self._context = context
        super().__init__(addr, port, context=context, timeout=TIMEOUT)

    def _get_socket(self, host, port, timeout):
        sock = socket.create_connection((host, port), timeout)
        return self._context.wrap_socket(sock, server_hostname=self._sni_name)


class AdminConnection(http.client.HTTPSConnection):
    """HTTPS management connection that verifies the documented hostname."""

    def __init__(self, addr, port, sni_name, context):
        self._sni_name = sni_name
        super().__init__(addr, port, context=context, timeout=TIMEOUT)

    def connect(self):
        sock = socket.create_connection((self.host, self.port), self.timeout)
        self.sock = self._context.wrap_socket(sock, server_hostname=self._sni_name)


class Client:
    def __init__(self, addr, ports, sni_name, ca_file):
        self.addr = addr
        self.imaps_port = ports["imaps"]
        self.submission_port = ports["submission"]
        self.https_port = ports["https"]
        self.sni_name = sni_name
        self.context = ssl.create_default_context(cafile=ca_file)

    def imap(self, user, password):
        conn = IMAP(self.addr, self.imaps_port, self.sni_name, self.context)
        typ, _ = conn.login(user, password)
        if typ != "OK":
            raise CheckFailure(f"IMAP login failed for {user}")
        return conn

    def smtp(self):
        conn = Submission(self.addr, self.submission_port, self.sni_name, self.context)
        conn.ehlo()
        return conn

    def verify_imaps_certificate(self):
        conn = IMAP(self.addr, self.imaps_port, self.sni_name, self.context)
        cert = conn.sock.getpeercert()
        conn.logout()
        san = [v for k, v in cert.get("subjectAltName", ()) if k == "DNS"]
        return ", ".join(san)

    def verify_submission_certificate(self):
        conn = Submission(self.addr, self.submission_port, self.sni_name, self.context)
        cert = conn.sock.getpeercert()
        conn.close()
        san = [v for k, v in cert.get("subjectAltName", ()) if k == "DNS"]
        return ", ".join(san)


def check(name, fn):
    try:
        detail = fn()
        print(f"PASS  {name}" + (f"  [{detail}]" if detail else ""))
        return True
    except Exception as exc:  # noqa: BLE001 - every check failure is reported, not raised
        print(f"FAIL  {name}\n      {type(exc).__name__}: {exc}")
        return False


def uids_with_subject(conn, mailbox, subject):
    conn.select(f'"{mailbox}"')
    typ, data = conn.uid("SEARCH", None, "SUBJECT", f'"{subject}"')
    if typ != "OK":
        raise CheckFailure(f"search in {mailbox} failed: {data}")
    return [u.decode() for u in data[0].split()]


def fetch_raw(conn, uid):
    typ, data = conn.uid("FETCH", uid, "(RFC822)")
    if typ != "OK" or not data or not isinstance(data[0], tuple):
        raise CheckFailure(f"fetch of uid {uid} failed: {data}")
    return data[0][1]


def fetch_flags(conn, uid):
    typ, data = conn.uid("FETCH", uid, "(FLAGS)")
    if typ != "OK" or not data or not data[0]:
        raise CheckFailure(f"flag fetch of uid {uid} failed: {data}")
    return data[0].decode(errors="replace")


def wait_for(fn, seconds=15):
    deadline = time.time() + seconds
    result = fn()
    while not result and time.time() < deadline:
        time.sleep(0.5)
        result = fn()
    return result


def build_message(sender, recipient, message_id, subject, attachment):
    msg = EmailMessage()
    msg["From"] = sender
    msg["To"] = recipient
    msg["Subject"] = subject
    msg["Message-ID"] = message_id
    msg.set_content("Plain-text body written by the verification script.\n")
    msg.add_alternative(
        "<html><body><p>HTML body written by the verification script.</p></body></html>",
        subtype="html",
    )
    msg.add_attachment(
        attachment,
        maintype="application",
        subtype="octet-stream",
        filename="payload.bin",
    )
    return msg


def submit(client, sender, password, msg):
    conn = client.smtp()
    try:
        conn.login(sender, password)
        conn.send_message(msg)
    finally:
        try:
            conn.quit()
        except smtplib.SMTPException:
            pass


def message_parts(raw):
    parsed = email.message_from_bytes(raw, policy=email.policy.default)
    plain = parsed.get_body(preferencelist=("plain",))
    html = parsed.get_body(preferencelist=("html",))
    attachment = None
    for part in parsed.walk():
        if part.get_content_disposition() == "attachment":
            attachment = part.get_payload(decode=True)
    if plain is None or html is None or attachment is None:
        raise CheckFailure("delivered message no longer has the plain, HTML and attachment parts")
    return plain.get_content(), html.get_content(), attachment


def move(conn, uid, mailbox):
    typ, data = conn.uid("MOVE", uid, f'"{mailbox}"')
    if typ == "OK":
        return
    # Fall back to copy + delete for servers without MOVE.
    typ, _ = conn.uid("COPY", uid, f'"{mailbox}"')
    if typ != "OK":
        raise CheckFailure(f"copy to {mailbox} failed: {data}")
    conn.uid("STORE", uid, "+FLAGS", r"(\Deleted)")
    conn.expunge()


def _append_sent(conn, raw):
    typ, data = conn.append(
        '"Sent Items"',
        r"(\Seen)",
        imaplib.Time2Internaldate(time.time()),
        raw,
    )
    if typ != "OK":
        raise CheckFailure(f"APPEND to Sent Items failed: {data}")
    return "appended"


def external_recipient_refused(client, sender, password):
    conn = client.smtp()
    try:
        typ, reply = conn.login(sender, password)
        if typ != 235:
            raise CheckFailure(f"submission login failed: {typ} {reply!r}")
        code, reply = conn.mail(sender)
        if code != 250:
            raise CheckFailure(f"MAIL FROM was not accepted: {code} {reply!r}")
        code, reply = conn.rcpt("nobody@example.net")
        if 200 <= code < 300:
            raise CheckFailure(f"external recipient was accepted: {code} {reply!r}")
        if not 500 <= code < 600:
            raise CheckFailure(f"external recipient refused with a non-permanent code {code}: {reply!r}")
        return f"{code} {reply.decode(errors='replace')}"
    finally:
        try:
            conn.rset()
            conn.quit()
        except smtplib.SMTPException:
            pass


def unauthenticated_submission_refused(client, sender):
    conn = client.smtp()
    try:
        code, reply = conn.mail(sender)
        if 200 <= code < 300:
            raise CheckFailure(f"unauthenticated submission was accepted: {code} {reply!r}")
        if not 500 <= code < 600:
            raise CheckFailure(f"unauthenticated submission refused with {code}: {reply!r}")
        return f"{code} {reply.decode(errors='replace')}"
    finally:
        try:
            conn.rset()
            conn.quit()
        except smtplib.SMTPException:
            pass


def queue_depth(client, admin_credentials):
    """Number of messages waiting for outbound delivery."""
    user, secret = admin_credentials
    conn = AdminConnection(client.addr, client.https_port, client.sni_name, client.context)
    try:
        token = base64.b64encode(f"{user}:{secret}".encode()).decode()
        body = json.dumps({
            "using": ["urn:ietf:params:jmap:core", "urn:stalwart:jmap"],
            "methodCalls": [["x:QueuedMessage/get", {}, "c1"]],
        })
        conn.request("POST", "/jmap/", body=body, headers={
            "Content-Type": "application/json",
            "Authorization": f"Basic {token}",
        })
        response = conn.getresponse()
        payload = json.loads(response.read())
    finally:
        conn.close()
    if response.status != 200:
        raise CheckFailure(f"queue query failed: {response.status} {payload}")
    method = payload["methodResponses"][0][1]
    return len(method.get("list", []))


def full_phase(client, accounts, state_path, admin_credentials):
    alice_name, alice_password = accounts["alice"]
    bob_name, bob_password = accounts["bob"]
    domain = need("MAIL_DOMAIN")
    alice = f"{alice_name}@{domain}"
    bob = f"{bob_name}@{domain}"
    # HEADER searches on Message-ID are not reliable on this server, so the
    # unique marker lives in the subject, which is indexed and searchable.
    token = f"{int(time.time())}-{os.getpid()}"
    subject = f"Mailflow verification {token}"
    message_id = make_msgid(domain=domain)
    attachment = hashlib.sha256(b"mailflow-verification").digest() * 64  # 2048 bytes
    attachment_sha = hashlib.sha256(attachment).hexdigest()

    failures = 0
    results = []

    def run(name, fn):
        nonlocal failures
        ok = check(name, fn)
        results.append((name, ok))
        if not ok:
            failures += 1

    run("IMAPS presents a certificate valid for the documented hostname",
        lambda: client.verify_imaps_certificate())
    run("submission presents a certificate valid for the documented hostname",
        lambda: client.verify_submission_certificate())

    alice_conn = client.imap(alice, alice_password)
    bob_conn = client.imap(bob, bob_password)
    run("alice authenticates over IMAPS and lists mailboxes",
        lambda: _listing(alice_conn))
    run("bob authenticates over IMAPS and lists mailboxes",
        lambda: _listing(bob_conn))

    # Select the recipient's inbox before sending, so the message is observed on
    # a connection that was already open.
    bob_conn.select("INBOX")
    message = build_message(alice, bob, message_id, subject, attachment)
    submit(client, alice, alice_password, message)

    uids = wait_for(lambda: uids_with_subject(bob_conn, "INBOX", subject))
    run("recipient's open connection sees local delivery without reconnecting",
        lambda: uids and "yes" or _fail("no message appeared"))
    run("exactly one copy is delivered to the recipient inbox",
        lambda: _expect(len(uids) == 1, f"{len(uids)} copies"))
    if not uids:
        _write_state(state_path, subject, message_id, attachment_sha, "INBOX")
        return failures, results
    uid = uids[0]

    raw = fetch_raw(bob_conn, uid)
    run("delivered message keeps plain-text, HTML and attachment parts",
        lambda: _parts_ok(raw))
    run("downloaded attachment is byte-identical to the sent attachment",
        lambda: _attachment_ok(raw, attachment_sha))

    run("alice's mailbox does not contain the message addressed to bob",
        lambda: _expect(not uids_with_subject(alice_conn, "INBOX", subject),
                        "alice saw bob's message"))

    # Flags, observed from a second client connection.
    bob_conn.uid("STORE", uid, "+FLAGS", r"(\Seen \Flagged)")
    second = client.imap(bob, bob_password)
    second.select("INBOX")
    flags = fetch_flags(second, uid)
    run("a second client observes the read state set by the first",
        lambda: _expect("\\Seen" in flags, flags))
    run("a second client observes the flagged state set by the first",
        lambda: _expect("\\Flagged" in flags, flags))
    second.logout()

    third = client.imap(bob, bob_password)
    third.select("INBOX")
    reconnected = fetch_flags(third, uid)
    third.logout()
    run("flag state survives a client reconnect",
        lambda: _expect("\\Flagged" in reconnected and "\\Seen" in reconnected, reconnected))

    # Custom mailbox created by one client, moved message seen by another.
    if "Projects" not in _listing(bob_conn):
        bob_conn.create("Projects")
    fourth = client.imap(bob, bob_password)
    listing = _listing(fourth)
    run("a second client sees the mailbox created by the first",
        lambda: _expect("Projects" in listing, listing))
    move(bob_conn, uid, "Projects")
    run("the moved message is visible in the new mailbox from the second client",
        lambda: _expect(uids_with_subject(fourth, "Projects", subject), "not found in Projects"))
    run("the moved message is no longer in the inbox",
        lambda: _expect(not uids_with_subject(bob_conn, "INBOX", subject), "still in INBOX"))
    fourth.logout()

    # Sent record. Stalwart does not create this copy itself: standard clients
    # append the submitted message to their sent mailbox, and the server must
    # accept that copy and show it to other clients.
    run("the server accepts the client's sent copy (IMAP APPEND)",
        lambda: _append_sent(alice_conn, message.as_bytes()))
    sent = wait_for(lambda: uids_with_subject(alice_conn, "Sent Items", subject))
    run("the sent mailbox contains the submitted message",
        lambda: sent and "yes" or _fail("not in Sent Items"))
    alice_second = client.imap(alice, alice_password)
    run("a second client sees the sent record",
        lambda: _expect(uids_with_subject(alice_second, "Sent Items", subject), "not found"))
    alice_second.logout()

    # Refusals.
    run("an external recipient is refused at submission",
        lambda: external_recipient_refused(client, alice, alice_password))
    run("an unauthenticated submission is refused",
        lambda: unauthenticated_submission_refused(client, alice))
    run("the refused external message was never queued for delivery",
        lambda: _expect(queue_depth(client, admin_credentials) == 0,
                        f"{queue_depth(client, admin_credentials)} queued"))

    _write_state(state_path, subject, message_id, attachment_sha, "Projects")

    alice_conn.logout()
    bob_conn.logout()
    return failures, results


def persistence_phase(client, accounts, state_path):
    alice_name, alice_password = accounts["alice"]
    bob_name, bob_password = accounts["bob"]
    domain = need("MAIL_DOMAIN")
    alice = f"{alice_name}@{domain}"
    bob = f"{bob_name}@{domain}"

    with open(state_path, encoding="utf-8") as handle:
        state = json.load(handle)

    failures = 0
    results = []

    def run(name, fn):
        nonlocal failures
        ok = check(name, fn)
        results.append((name, ok))
        if not ok:
            failures += 1

    alice_conn = client.imap(alice, alice_password)
    bob_conn = client.imap(bob, bob_password)
    run("both accounts still authenticate after the stack change",
        lambda: _listing(alice_conn) and _listing(bob_conn) and "both accounts")

    mailbox = state["mailbox"]
    uids = uids_with_subject(bob_conn, mailbox, state["subject"])
    run(f"the message is still present in {mailbox}", lambda: _expect(uids, "missing"))
    if uids:
        flags = fetch_flags(bob_conn, uids[0])
        run("the read and flagged state survived",
            lambda: _expect("\\Seen" in flags and "\\Flagged" in flags, flags))
        raw = fetch_raw(bob_conn, uids[0])
        run("the attachment is still byte-identical after the stack change",
            lambda: _attachment_ok(raw, state["attachment_sha256"]))
    run("the custom mailbox is still listed",
        lambda: _expect(mailbox in _listing(bob_conn), _listing(bob_conn)))
    run("the sent record is still visible",
        lambda: _expect(uids_with_subject(alice_conn, "Sent Items", state["subject"]),
                        "not in Sent Items"))

    alice_conn.logout()
    bob_conn.logout()
    return failures, results


def _fail(message):
    raise CheckFailure(message)


def _expect(condition, detail):
    if not condition:
        raise CheckFailure(detail)
    return "ok"


def _listing(conn):
    typ, data = conn.list()
    if typ != "OK":
        raise CheckFailure(f"mailbox listing failed: {data}")
    names = []
    for item in data or []:
        if item is None:
            continue
        text = item.decode(errors="replace")
        # The mailbox name is the final quoted string; it may contain spaces.
        match = re.search(r'"([^"]*)"\s*$', text)
        if match:
            names.append(match.group(1))
    return ", ".join(names)


def _parts_ok(raw):
    plain, html, _ = message_parts(raw)
    if "Plain-text body" not in plain or "HTML body" not in html:
        raise CheckFailure("body parts are not the ones that were sent")
    return "plain + html + attachment"


def _attachment_ok(raw, expected_sha):
    _, _, attachment = message_parts(raw)
    actual = hashlib.sha256(attachment).hexdigest()
    if actual != expected_sha:
        raise CheckFailure(f"sha256 {actual} != {expected_sha}")
    return actual[:16]


def _write_state(path, subject, message_id, attachment_sha, mailbox):
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(
            {
                "subject": subject,
                "message_id": message_id,
                "attachment_sha256": attachment_sha,
                "mailbox": mailbox,
            },
            handle,
            indent=2,
        )
    os.chmod(path, 0o600)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=("full", "persistence"), required=True)
    parser.add_argument("--addr", default="127.0.0.1")
    parser.add_argument("--state", required=True)
    args = parser.parse_args()

    data_dir = os.environ.get("MAIL_DATA_DIR", "./data")
    client = Client(
        args.addr,
        {
            "imaps": int(os.environ.get("MAIL_IMAPS_PORT", "993")),
            "submission": int(os.environ.get("MAIL_SUBMISSION_PORT", "465")),
            "https": int(os.environ.get("MAIL_HTTPS_PORT", "443")),
        },
        need("MAIL_HOSTNAME"),
        f"{data_dir}/tls/ca.pem",
    )
    accounts = {
        "alice": (need("MAIL_ACCOUNT_ALICE"), need("MAIL_ACCOUNT_ALICE_PASSWORD")),
        "bob": (need("MAIL_ACCOUNT_BOB"), need("MAIL_ACCOUNT_BOB_PASSWORD")),
    }

    # Management credentials live in the data directory, never in the client
    # account environment. Only a read-only queue query uses them.
    with open(f"{data_dir}/secrets/admin.cred", encoding="utf-8") as handle:
        admin_credentials = tuple(handle.read().strip().split(":", 1))

    if args.phase == "full":
        failures, results = full_phase(client, accounts, args.state, admin_credentials)
    else:
        failures, results = persistence_phase(client, accounts, args.state)

    total = len(results)
    print()
    print(f"{total - failures}/{total} checks passed ({args.phase} phase)")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
