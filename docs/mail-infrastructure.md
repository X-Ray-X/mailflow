# Mail infrastructure

The mail deployment is a single Stalwart service, run as its own Compose project.
It is the source of truth for mailboxes. Application deployments must never
recreate, upgrade, or reset it; see `openspec/changes/stalwart-local-environment/`.

**Verified state:** every claim below was checked on macOS arm64 with OrbStack,
against Stalwart `v1.1.0`, unless it appears under [Known gaps](#known-gaps).

## What runs

| Item | Value |
|---|---|
| Compose project | `mailflow-mail` (`deploy/mail/compose.yaml`) |
| Service | `stalwart`, container `mailflow-stalwart` |
| Image | `ghcr.io/stalwartlabs/stalwart:v1.1.0` |
| Image digest | index `sha256:f772aa6e…20837e`, linux/arm64 manifest `sha256:eebae3f4…c5211d` |
| Domain | `mailflow.test` (reserved, non-routable) |
| Hostname | `mail.mailflow.test` |
| Data | host bind mounts under the path in `MAIL_DATA_DIR` (local default `./data`) |
| Container user | `2000:2000` (`stalwart`) |

The image publishes a native `linux/arm64` manifest, so it runs without
emulation on the target arm64 homelab host. Verified with
`docker buildx imagetools inspect ghcr.io/stalwartlabs/stalwart:v1.1.0`.

### Host deployment (CI)

The deployment workflow runs on a self-hosted runner that is itself a
container. Compose bind mounts are resolved by the host Docker daemon, so the
data path must exist on the host at the same absolute path the runner sees. A
path that only exists inside the runner (for example, a checkout in the
runner's workspace) would silently mount a different, empty directory on the
host.

`MAIL_DATA_DIR` is therefore a required `production` environment variable, not
a tracked default: it names the absolute host path. The workflow fails before
touching the stack if it is unset, relative, or not on a host mount (it
compares the path's mount point with the runner workspace). It does not
hardcode the host clone location, so moving the checkout does not require a
code change.

The workflow runs `bootstrap.sh` with `sudo`, which creates the directory and
hands `etc/`, `lib/` and `tls/` to uid/gid `2000` so the container can read and
write them. Operations that read the TLS key or the data tree must use `sudo`.

The runner's loopback is not the host's, so the workflow does not call the
published setup or management ports directly. `bootstrap.sh`, the HTTPS wait,
and the certificate SAN check use `docker exec` into `mailflow-stalwart` and
the container's internal listeners (`8080` and `443`).

`MAIL_TAILNET_IP` is an optional `production` environment variable used only by
the deploy run summary; the service never reads it. It is a variable rather
than a secret because the summary prints it, and secrets would be masked.

## Prerequisites

- Docker with the Compose plugin (`docker compose version` succeeds).
- Free host ports `993`, `465`, and loopback `443` / `8089`.
- `curl`, `openssl`, and `python3` for `scripts/bootstrap.sh` and `scripts/verify.sh`.
- Outbound HTTPS on the **first** start only: the image downloads the WebUI and
  ASN/Geo data from GitHub. Internal mail delivery needs no internet connection
  and no external DNS. The spam classifier model is absent at first start and a
  training task is scheduled; filtering is not tuned or relied on here.

### Host readiness (verified 2026-11-14)

On the deployment host, `docker compose version` reports `2.26.1-4`,
`tailscale status` reports the host on the tailnet, and the configured
`MAIL_DATA_DIR` exists with `etc/`, `lib/` and `tls/` owned by `2000:2000`.
The host's tailnet address is supplied by the `MAIL_TAILNET_IP` environment
variable rather than tracked here.

## Files

```
deploy/mail/
  compose.yaml              # the deployment unit; no application services
  compose.bootstrap.yaml    # first-start overlay; adds a fixed bootstrap credential
  .env.example              # tracked variable names and safe defaults
  .env                      # local values, untracked
  data/
    etc/config.json         # DataStore config; losing this re-enters setup mode
    lib/                    # RocksDB: mail, accounts, settings
    tls/                    # internal CA, server certificate, private key
    secrets/admin.cred      # administrator credential written at bootstrap
  scripts/
    bootstrap.sh            # idempotent first start and account provisioning
    verify.sh, verify.py    # spec verification through real IMAP/SMTP clients
```

`deploy/mail/.env` and `deploy/mail/data/` are ignored by Git. No credential is
tracked.

## Configure

```sh
cd deploy/mail
cp .env.example .env
$EDITOR .env            # set passwords; keep the domain and hostname if unchanged
```

| Variable | Purpose |
|---|---|
| `STALWART_IMAGE` | Pinned release. Change only as a deliberate upgrade. |
| `MAIL_DOMAIN`, `MAIL_HOSTNAME` | Local domain and canonical hostname. |
| `MAIL_DATA_DIR` | Host data path; local default `./data`, CI requires an absolute host path. |
| `MAIL_IMAPS_PORT`, `MAIL_SUBMISSION_PORT` | Published client ports. |
| `MAIL_HTTPS_PORT`, `MAIL_SETUP_PORT` | Loopback admin/JMAP ports. |
| `MAIL_BOOTSTRAP_PASSWORD` | Fixed credential used only by the first start. |
| `MAIL_ACCOUNT_ALICE`, `MAIL_ACCOUNT_BOB` | Local parts of the demo accounts. |
| `MAIL_ACCOUNT_*_PASSWORD` | Account passwords (minimum 8 characters). |

## First start and account provisioning

```sh
cd deploy/mail
./scripts/bootstrap.sh
```

The script is idempotent. It never deletes data, and re-running it creates
nothing new: it re-applies the same certificate and relaying settings and
reports the accounts as already present.

1. Generates an internal CA and a server certificate for `MAIL_HOSTNAME` if
   `data/tls` has none.
2. Starts the bootstrap overlay. Stalwart has no `config.json` yet, so it serves
   only the setup listener (container port `8080`, published on
   `127.0.0.1:8089`). The script reaches it with `docker exec`, so it also
   works from the containerized CI runner, whose loopback is not the host's.
3. Calls `x:Bootstrap/set` with the hostname, the domain, **no ACME request**
   (`requestTlsCertificate: false`), no DKIM generation, a manual DNS server,
   and stdout logging. The reply carries the administrator username and one-time
   secret, which the script writes to `data/secrets/admin.cred` (mode 600).
4. Recreates the service **without** the bootstrap credential. The config now
   exists, so the server starts its normal listeners.
5. Creates the `Certificate` object from the files in `data/tls`, then runs the
   `ReloadTlsCertificates` action so the certificate is used without a restart.
6. Refuses relaying: sets `MtaStageRcpt.allowRelaying` to `false` and reloads
   settings. Without this, an authenticated user could send to any domain.
7. Creates accounts `alice` and `bob` through `x:Account/set`, if missing.

`requestTlsCertificate: false` matters. A `.test` domain cannot obtain a
Let's Encrypt certificate; leaving it on produced repeated
`acme.auth.error: invalidContact` errors before the certificate was installed
by hand.

Management calls in the script use `curl -k` on purpose: on a first start the
server has no certificate object until step 5 creates one, so there is nothing
to verify against yet, and the port is bound to host loopback. Mail clients are
the opposite case and always verify.

## Everyday operation

```sh
cd deploy/mail

docker compose -f compose.yaml ps          # status
docker compose -f compose.yaml logs -f     # logs
docker compose -f compose.yaml stop        # stop, keep container and data
docker compose -f compose.yaml start       # start the existing container
docker compose -f compose.yaml restart     # restart the existing container
docker compose -f compose.yaml up -d       # apply config changes, recreate if needed
```

Run every command with the Docker context that hosts the stack. On the author's
machine that is `DOCKER_CONTEXT=orbstack`.

Stopping the application stack later is a different Compose project and must not
be used to stop mail; and vice versa.

## Ports

Published, from `docker compose ps`:

| Host | Container | Purpose |
|---|---|---|
| `993` | `993` | IMAPS, client access |
| `465` | `465` | SMTP submission, implicit TLS |
| `127.0.0.1:443` | `443` | JMAP and management API over HTTPS, loopback only |
| `127.0.0.1:8089` | `8080` | Plain HTTP (setup wizard, live health), loopback only |

The container also listens on `25` (SMTP), `995` (POP3S), and `4190` (ManageSieve),
but those ports are **not published**. Port 25 is absent deliberately: nothing
outside the container network can deliver mail to it, and the server never
delivers externally because relaying is disabled.

## Hostname and TLS

The certificate has `subjectAltName = DNS:mail.mailflow.test, DNS:mailflow.test`
and is signed by the internal CA at `data/tls/ca.pem`.

Clients must trust that CA and resolve the hostname. Locally:

```sh
# Resolve the canonical name to the host running the stack.
echo "127.0.0.1 mail.mailflow.test" | sudo tee -a /etc/hosts

# Trust the CA once (macOS example; on Linux use the system trust store).
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain deploy/mail/data/tls/ca.pem
```

Do not disable certificate verification in a client: the contract is verified
TLS, and the internal CA exists so that verification can stay on.

On the homelab host, the preferred certificate would be the tailnet certificate
obtained via `tailscale cert`, which no client has to trust by hand. It is not
available on the current tailnet: `tailscale status --json` reports
`"CertDomains": null`, so HTTPS certificates are not enabled. The internal CA
is therefore the chosen certificate path for now; see
[Known gaps](#known-gaps).

## Accounts

| Account | Source of the password |
|---|---|
| `alice@mailflow.test` | `MAIL_ACCOUNT_ALICE_PASSWORD` |
| `bob@mailflow.test` | `MAIL_ACCOUNT_BOB_PASSWORD` |
| `admin@mailflow.test` | `data/secrets/admin.cred`, created at bootstrap |

`data/secrets/admin.cred` is the only copy of the administrator secret. Back it
up with the data directory. If it is lost while `config.json` exists, the script
stops with an explicit error rather than creating a second administrator.

## Connect a mail client

The server certificate is signed by the internal CA at `data/tls/ca.pem`. A
client must trust that CA and resolve the canonical hostname; it must not
disable certificate verification. The certificate covers
`mail.mailflow.test` and `mailflow.test`, not a bare IP address.

### Trust the internal CA

Copy only the public certificate to the client. Never copy `ca-key.pem` or
`privkey.pem`.

From the host:

```sh
docker cp mailflow-stalwart:/etc/stalwart/tls/ca.pem ./mailflow-ca.pem
```

Install it in the client's trust store:

- **Apple Mail / macOS:** `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain mailflow-ca.pem`
- **Thunderbird:** Settings → Privacy & Security → Certificates → Manage
  Certificates → Authorities → Import, then trust the CA. Thunderbird uses its
  own NSS store, so the macOS keychain alone may not be enough.
- **iOS:** install the profile, then enable full trust under Settings →
  General → About → Certificate Trust Settings.
- **Linux:** copy it to `/usr/local/share/ca-certificates/` and run
  `sudo update-ca-certificates`.

### Resolve the canonical hostname

The client connects to `mail.mailflow.test`, not to an IP. The name must
resolve to the host:

- **Desktop:** add `<host-address> mail.mailflow.test` to the hosts file.
- **Mobile:** Tailscale MagicDNS resolves `*.ts.net` names, not
  `mail.mailflow.test`. Add a split-DNS route for `mailflow.test` to a DNS
  server on the tailnet that has an A record for `mail.mailflow.test` →
  `<host-address>`.
- If you use a different hostname, the certificate must cover it.

### Configure the account

Use manual setup if the client guesses STARTTLS: both published client ports
expect implicit TLS.

| Setting | Value |
|---|---|
| IMAP | `mail.mailflow.test`, port `993`, SSL/TLS |
| SMTP submission | `mail.mailflow.test`, port `465`, SSL/TLS |
| Username | the full address, for example `alice@mailflow.test` |
| Password | `MAIL_ACCOUNT_ALICE_PASSWORD` or `MAIL_ACCOUNT_BOB_PASSWORD` |

### Verify

- The client connects without a certificate warning and lists the mailboxes.
- A message from one local account arrives in the other account's inbox.

From a machine that resolves the name:

```sh
openssl s_client -connect mail.mailflow.test:993 -servername mail.mailflow.test \
  </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -issuer -text \
  | grep -A1 'Subject Alternative Name'
```

macOS ships LibreSSL, whose `x509` does not support `-ext`; `-text` works on
both LibreSSL and OpenSSL.

## Configuration options and their sources

Every option was read from the documentation of the pinned release
(docs.stalw.art, which documents `v1.1.0`, the current stable release at the
time). Nothing is carried over from another release.

| Option or call | Value | Documentation |
|---|---|---|
| Image tag `v1.1.0` | pinned release | [Docker platform](https://stalw.art/docs/install/platform/docker), GitHub release `v1.1.0` |
| Volumes `/etc/stalwart`, `/var/lib/stalwart` | bind mounts | Docker platform, "Container details" |
| `STALWART_RECOVERY_ADMIN` | bootstrap overlay only | [Bootstrap mode](https://stalw.art/docs/configuration/bootstrap-mode), [Recovery mode](https://stalw.art/docs/configuration/recovery-mode) |
| `defaultHostname`, `defaultDomain` | hostname, domain | [Bootstrap object](https://stalw.art/docs/ref/object/bootstrap) |
| `requestTlsCertificate: false` | manual certificate | Bootstrap object |
| `generateDkimKeys: false` | no DKIM in scope | Bootstrap object |
| `dnsServer: {"@type":"Manual"}` | no external DNS | Bootstrap object |
| `tracer: {"@type":"Stdout",…}` | container logging | [Tracer object](https://stalw.art/docs/ref/object/tracer) |
| DataStore `RocksDb`, `/var/lib/stalwart/` | default | Bootstrap object, Docker platform |
| `x:Account/set` for users | `@type: User`, credentials, roles | [Managing accounts](https://stalw.art/docs/auth/accounts/manage) |
| `x:Certificate/set` with `File` values | `fullchain.pem`, `privkey.pem` | [Certificates](https://stalw.art/docs/server/tls/certificates), [Certificate object](https://stalw.art/docs/ref/object/certificate) |
| `ReloadTlsCertificates` action | apply a new certificate without restart | Certificates, [Actions](https://stalw.art/docs/management/tasks-actions/actions) |
| `MtaStageRcpt.allowRelaying: {"else":"false"}` | refuse every remote recipient | [RCPT TO stage](https://stalw.art/docs/mta/inbound/rcpt), [MtaStageRcpt](https://stalw.art/docs/ref/object/mta-stage-rcpt) |
| `ReloadSettings` action | apply a settings singleton | Actions |
| Listener defaults (993, 465, 443, 8080, 25, 995, 4190) | unchanged | [Network listeners](https://stalw.art/docs/server/listener), startup log |

Behavior observed in this environment and not assumed:

- A `.test` domain cannot use ACME; the order fails with `invalidContact`.
- `x:Domain/get` returns domains without an `id` field; the domain id is the
  response's `accountId`, and accounts reference it as `domainId`.
- A settings singleton is stored immediately but applied only on reload.
- IMAP `SEARCH HEADER Message-ID` returns nothing on this release; subject and
  body searches work.
- A newly created `Certificate` object is used only after
  `ReloadTlsCertificates` or a restart.
- The server does not create a sent copy of an SMTP submission. Standard clients
  append it to their Sent mailbox, which is what the spec now records.

## Verification

```sh
cd deploy/mail
./scripts/verify.sh
```

The script checks the deployment and exercises the specs through real IMAP and
SMTP clients with certificate verification on. It covers: certificate validation
on both client ports, independent account authentication, delivery to an open
connection, exactly one copy, MIME plain/HTML/attachment fidelity by SHA-256,
shared read and flagged state, reconnect persistence, a custom mailbox and a
moved message, the client's sent copy, refusal of an external recipient with no
queued message, refusal of unauthenticated submission, and survival of all of it
across a stack stop/start and a container recreation.

Last run: **33/33 checks passed** (21 full, 6 after restart, 6 after recreation).

### Spec scenario coverage

Status of every scenario in the change's two specs. "Protocol client" means the
stdlib IMAP/SMTP client in `scripts/verify.py`, which is a real client of both
protocols. Desktop results are from Thunderbird on macOS.

| Spec | Scenario | Status |
|---|---|---|
| mail-infrastructure | Fresh host reproduces the environment | PASS — `bootstrap.sh` from an empty `data/` |
| mail-infrastructure | Pinned release is the running release | PASS — image checked by `verify.sh` |
| mail-infrastructure | Restart preserves mail | PASS |
| mail-infrastructure | Recreation preserves mail | PASS |
| mail-infrastructure | Host reboot preserves mail | NOT VERIFIED — no reboot of a host was performed |
| mail-infrastructure | Internal delivery without internet | PARTIAL — local delivery passes, host-offline case untested |
| mail-infrastructure | No external egress | PASS — `550`, empty queue, refusal event |
| mail-infrastructure | Accounts authenticate independently | PASS |
| mail-infrastructure | No credentials in the repository | PASS — ignored `.env`, grep finds no password |
| mail-infrastructure | Client validates the certificate | PASS — protocol client and Thunderbird (macOS) |
| mail-infrastructure | Advertised hostname matches the certificate | PASS — SANs match the documented names |
| mail-infrastructure | Port inventory matches documentation | PASS |
| mail-infrastructure | Unauthenticated submission is refused | PASS — `503 5.5.1` |
| mail-infrastructure | Application deployment leaves mail untouched | NOT EXECUTED — no application stack exists yet; the boundary is a separate Compose project |
| mail-infrastructure | Runs on the target host | PASS — deployed on the arm64 host with the native image |
| mailbox-access | Client completes account setup | PASS — protocol client and Thunderbird (macOS) |
| mailbox-access | Sending succeeds | PASS — protocol client and Thunderbird (macOS) |
| mailbox-access | Attachment survives delivery | PASS — SHA-256 match |
| mailbox-access | Custom mailbox round-trips | PASS |
| mailbox-access | Mailbox rename | NOT VERIFIED — create and delete were exercised, rename was not |
| mailbox-access | Read state is shared between clients | PASS |
| mailbox-access | Flag state survives reconnect | PASS |
| mailbox-access | Submitted message appears in sent mailbox | PASS — client APPEND |
| mailbox-access | Sent record is visible from a second client | PASS |
| mailbox-access | Delivery is immediate | PASS — open connection received it |
| mailbox-access | No duplicate copies | PASS |
| mailbox-access | Two client families work | PARTIAL — Thunderbird (desktop) verified; mobile not configured |

## Backup and restore

Both are filesystem operations on `deploy/mail/data`. Stop the service first so
RocksDB is not writing during the copy. The CI deployment owns `etc/`, `lib/`
and `tls/` as uid/gid `2000`, so read them with `sudo`.

```sh
cd deploy/mail
docker compose -f compose.yaml stop
sudo tar -czf mailflow-mail-$(date +%Y%m%d-%H%M).tgz -C data .
docker compose -f compose.yaml start
```

Restore:

```sh
cd deploy/mail
docker compose -f compose.yaml stop
sudo rm -rf data
sudo mkdir data
sudo tar -xzf mailflow-mail-YYYYMMDD-HHMM.tgz -C data
# Recreate, do not just start: an existing container keeps its old mount.
docker compose -f compose.yaml up -d --force-recreate
```

Restore replaces the whole data directory, so the `rm -rf` is the point of the
operation: take the backup first, stop the service, and treat the removal as a
deliberate restore step that never belongs to a restart, deploy, or upgrade.

The recreate step is required. `docker compose start` reuses the container's
existing bind mount, so the server kept reading the directory that had been
moved aside, and the restored messages did not appear. With
`up -d --force-recreate`, a message deleted after the backup reappeared, with
its flags and mailbox intact. This was tested end to end.

## Upgrade

Upgrading is a change to the pinned release, not a side effect of a pull.

1. Back up `data` as above and keep it.
2. Read the new release's documentation for every option listed in this file.
3. Change `STALWART_IMAGE` in `.env` to the new exact tag.
4. `docker compose -f compose.yaml up -d` (recreates the container).
5. Run `./scripts/verify.sh`.

Rollback: set the previous tag and `up -d` again, then restore the backup if the
new release migrated data.

## Known gaps

- **Tailnet certificate** (`tailscale cert`) is unavailable because the tailnet
  has HTTPS certificates disabled (`"CertDomains": null`). The internal CA
  (`CN=Mailflow Internal CA`, SANs `mail.mailflow.test, mailflow.test`) is the
  chosen path for now. Enabling HTTPS certificates for the tailnet remains the
  preferred future path.
- **Mobile client** was not configured. Thunderbird on macOS was verified on
  2026-11-15 with both demo accounts and a delivered test message, so the
  desktop side is covered; the phone side of the two-client-family scenario
  remains open.
- **"No internet" delivery** was not tested with the host offline. Delivery uses
  the local domain and manual DNS, so no external lookup is involved, but that
  was not observed under an actual network cut.
- The first start downloaded the WebUI and ASN/Geo data from GitHub; a first
  start without internet would leave those resources missing, which does not
  affect mail protocols.
