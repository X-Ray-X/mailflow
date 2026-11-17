# Application database

The Go service keeps Mailflow application state in PostgreSQL. The database is
part of the **application** deployment unit (`deploy/app`), not the mail unit. It
holds idempotency records today and may later hold an outbox or projections. It
never holds message bodies, subjects, participants, mailbox membership, flags, or
credentials — the mail server remains the only source of mailbox state.

## Pinned release

| Item | Value |
|---|---|
| Image | `postgres:18.4-alpine` |
| Index digest | `sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15` |
| arm64 manifest | `sha256:337172f3d66e8a9298a194cb848d1758e905e3b00d21aec6f1e88e57bf06097b` |
| Platform | `linux/arm64` (native on the homelab host) |

Upgrading is a deliberate change: dump the database first, bump the pin in
`deploy/app/.env.example`, record the new digests here, and verify the service
starts and serves reads. The mail deployment unit is not involved.

## Layout

| Item | Value |
|---|---|
| Container | `mailflow-db` |
| Compose project | `mailflow-app` |
| Volume | `mailflow-app-db`, mounted at `/var/lib/postgresql` |
| Data directory | `/var/lib/postgresql/18/docker` (the image's `PGDATA`) |
| Network | `mailflow-app-data`, internal, joined by `api` and `db` |
| Published ports | none |

The mount path is the image's declared volume, not the pre-18 convention of
`/var/lib/postgresql/data`. PostgreSQL 18 keeps `PGDATA` one level below the
volume root, so mounting `/var/lib/postgresql/data` would put the data outside
the volume and lose it on the next container replacement.

The database has no host port and its network has no external route. It is
reachable only from the application container. Do not publish it, do not attach
the mail project to this network, and do not move mailbox data here.

## Lifecycle

- First start creates the volume and initialises the cluster; the service
  creates its table at startup.
- `docker compose up -d --remove-orphans` preserves the volume. An application
  deploy, restart, or rollback therefore keeps the idempotency records.
- `docker compose down --volumes` deletes them. It is never part of a deploy,
  a restart, or a cleanup step; treat it as data loss.
- Backups and restores are performed with `docker compose exec`, not by copying
  the volume.

## Backup and restore

Dump, on the host, from inside the database container:

```sh
cd deploy/app
docker compose exec -T db pg_dump -U mailflow -d mailflow -Fc > mailflow-app-$(date +%Y%m%d).dump
```

Restore into an empty or current database:

```sh
docker compose exec -T db pg_restore -U mailflow -d mailflow --clean --if-exists \
  < mailflow-app-YYYYMMDD.dump
```

Stop the service while restoring, or accept that a send in flight may record a
claim the restore then removes.

### What a stale restore means

The records expire after `MAILFLOW_IDEMPOTENCY_TTL` (default 24 hours) and the
janitor deletes them. WordPress-style restores are not the threat model here;
the actual consequence is narrow and should be stated plainly: if a dump is
restored from before a send, retrying that send with its old key is no longer a
replay and **can deliver a second time**. Restoring a backup does not corrupt
mail or lose mailbox state; it only forgets recent deduplication history. When
in doubt after a restore, clients should use a new idempotency key and treat the
previous attempt as unknown.

The server keeps its write-ahead log in the same volume. There is no replica and
no point-in-time recovery: losing the volume loses the records, and the honest
recovery is a restore from the last dump.

## What is stored

One table, `send_idempotency`:

| Column | Meaning |
|---|---|
| `account_name` | The authenticated local part with domain; stable, not the mail server's opaque account id |
| `key` | The caller's idempotency key, scoped by account |
| `request_hash` | SHA-256 of the validated send request, to detect a reused key with different content |
| `state` | `in_progress` or `succeeded` |
| `message_id` | The Mailflow message identifier returned to the caller on success |
| `claimed_at`, `completed_at`, `expires_at` | Claim time, success time, retention deadline |

No message content is stored. The hash is one-way and the identifier is opaque.
The table is created at startup under a session advisory lock, so two replicas
starting together do not race the DDL.

### Retention and the janitor

A successful record expires `MAILFLOW_IDEMPOTENCY_TTL` after it completed
(default 24 hours). An unfinished claim expires at the end of its lease
(default 2 minutes); a retry after that may take the key over, which bounds a
duplicate but cannot prove the first attempt did not happen. A record past its
expiry is treated as absent even before it is deleted, so the read path does not
depend on the sweep.

The service sweeps at startup and every 10 minutes. Each statement deletes at
most 1,000 expired rows and each sweep runs at most 100 statements, so a large
backlog cannot hold one long transaction. The sweep stops when the process
stops. Deleting a record is what makes an old key new again; see
[`docs/api.md`](api.md#idempotency-retention) for what that means to a client.

## Credentials

`MAILFLOW_DB_PASSWORD` comes from the `production` GitHub Environment and is
written into `deploy/app/.env` (mode 600) on every deploy; no tracked file holds
the real value. The service never logs the password or the assembled connection
string, and logs name variables rather than values.

Traffic between the two containers is not encrypted. It stays on the host's
private Docker bridge network and no port is published. Moving PostgreSQL off
the host would require TLS and is a separate change.
