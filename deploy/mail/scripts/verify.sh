#!/bin/sh
# Exercise the running mail infrastructure against the change's specs.
#
# Checks, in order:
#   1. the compose project renders, is pinned, and publishes only the
#      documented ports;
#   2. every mailbox-access scenario through real IMAP and SMTP clients,
#      with certificate verification enabled;
#   3. the same state after a stack stop/start and after the container is
#      recreated from the same pinned image;
#   4. the refusal of external recipients left no queued message behind.
#
# Requires: docker with the compose plugin, curl, openssl, python3.
# Run bootstrap.sh first. Never deletes data.

set -eu

cd "$(dirname "$0")/.."

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ -f .env ] || die "missing .env; copy .env.example to .env and fill it in"
set -a
. ./.env
set +a

DC="docker compose -f compose.yaml"
HTTPS_PORT="${MAIL_HTTPS_PORT:-443}"
STATE="${MAIL_DATA_DIR:-./data}/verify-state.json"

for cmd in docker curl python3; do
  command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
done

wait_healthy() { # seconds
  i=0
  while [ "$i" -lt "$1" ]; do
    if curl -sk -o /dev/null "https://127.0.0.1:${HTTPS_PORT}/healthz/live"; then return 0; fi
    i=$((i + 1))
    sleep 2
  done
  die "HTTPS endpoint did not become healthy"
}

say "=== configuration ==="
SERVICES=$($DC config --services | tr '\n' ' ')
[ "$SERVICES" = "stalwart " ] \
  || die "mail project must contain only the stalwart service, found: $SERVICES"
say "ok   compose project contains only the mail service"

IMAGES=$($DC config --images)
[ "$IMAGES" = "${STALWART_IMAGE}" ] \
  || die "compose resolves image '${IMAGES}', expected '${STALWART_IMAGE}'"
say "ok   pinned image is ${STALWART_IMAGE}"

RUNNING_IMAGE=$(docker inspect mailflow-stalwart --format '{{.Config.Image}}')
[ "$RUNNING_IMAGE" = "${STALWART_IMAGE}" ] \
  || die "running container uses '${RUNNING_IMAGE}', expected '${STALWART_IMAGE}'"
say "ok   running container uses the pinned image"

$DC ps --format json | python3 -c '
import json, sys
expected = {int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])}
published = set()
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    service = json.loads(line)
    for publisher in service.get("Publishers", []) or []:
        if publisher.get("PublishedPort"):
            published.add(int(publisher["PublishedPort"]))
if 25 in published:
    sys.exit("port 25 is published")
if published != expected:
    sys.exit(f"published ports {sorted(published)} != documented {sorted(expected)}")
print("ok   published ports are " + ", ".join(str(p) for p in sorted(published)))
' "${MAIL_IMAPS_PORT:-993}" "${MAIL_SUBMISSION_PORT:-465}" "${HTTPS_PORT}" "${MAIL_SETUP_PORT:-8080}"

wait_healthy 30
say ""
say "=== client scenarios ==="
LOG_MARK=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERIFY_LOGS="${TMPDIR:-/tmp}/mailflow-verify-$$.log"
STATUS=0

python3 scripts/verify.py --phase full --state "$STATE" || STATUS=1

# Read the log before the container is recreated below: recreation discards it.
$DC logs --since "$LOG_MARK" > "$VERIFY_LOGS" 2>&1 || true

say ""
say "=== stack stop and start ==="
$DC stop >/dev/null
$DC start >/dev/null
wait_healthy 60
python3 scripts/verify.py --phase persistence --state "$STATE" || STATUS=1

say ""
say "=== container recreation with the pinned release ==="
$DC up -d --force-recreate >/dev/null
wait_healthy 60
python3 scripts/verify.py --phase persistence --state "$STATE" || STATUS=1

say ""
say "=== external delivery attempt ==="
if grep -q 'smtp.relay-not-allowed' "$VERIFY_LOGS"; then
  say "ok   server refused the external recipient (smtp.relay-not-allowed)"
else
  say "FAIL no refusal event in the server log"
  STATUS=1
fi
rm -f "$VERIFY_LOGS"

if [ "$STATUS" -ne 0 ]; then
  say ""
  say "verification failed"
  exit 1
fi

say ""
say "verification passed"
