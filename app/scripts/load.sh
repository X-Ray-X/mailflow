#!/usr/bin/env bash
# Run the bounded, read-only load check. Credentials are read from the local
# mail .env and never printed; source is streamed into the image so there is no
# bind mount and no surviving container or result file.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VUS="${VUS:-10}"
DURATION="${DURATION:-1m}"
IMAGE="${K6_IMAGE:-grafana/k6:0.54.0}"
NETWORK="${MAILFLOW_NETWORK:-mailflow-app-data}"
BASE_URL="${BASE_URL:-http://api:8080}"

case "$VUS" in
  ''|*[!0-9]*) echo "VUS must be a number" >&2; exit 1 ;;
esac
if [ "$VUS" -lt 1 ] || [ "$VUS" -gt 50 ]; then
  echo "VUS must be between 1 and 50" >&2
  exit 1
fi

case "$DURATION" in
  *s) seconds="${DURATION%s}" ;;
  *m) seconds="$(( ${DURATION%m} * 60 ))" ;;
  *) echo "DURATION must look like 30s or 2m" >&2; exit 1 ;;
esac
case "$seconds" in
  ''|*[!0-9]*) echo "DURATION must be a whole number of seconds or minutes" >&2; exit 1 ;;
esac
if [ "$seconds" -lt 10 ] || [ "$seconds" -gt 300 ]; then
  echo "DURATION must be between 10s and 5m" >&2
  exit 1
fi

if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  echo "network $NETWORK does not exist; start the application stack first" >&2
  exit 1
fi

credentials="$(python3 - "$ROOT/deploy/mail/.env" <<'PY'
import sys
values = {}
for line in open(sys.argv[1]):
    line = line.strip()
    if line and not line.startswith('#') and '=' in line:
        key, value = line.split('=', 1)
        values[key] = value
print(f"{values['MAIL_ACCOUNT_BOB']}@{values['MAIL_DOMAIN']}")
print(values['MAIL_ACCOUNT_BOB_PASSWORD'])
PY
)"
user="$(printf '%s\n' "$credentials" | sed -n '1p')"
password="$(printf '%s\n' "$credentials" | sed -n '2p')"

cat "$ROOT/app/scripts/load/api.js" | docker run --rm -i \
  --network "$NETWORK" \
  -e "BASE_URL=$BASE_URL" \
  -e "VUS=$VUS" \
  -e "DURATION=$DURATION" \
  -e "MAILFLOW_USER=$user" \
  -e "MAILFLOW_PASSWORD=$password" \
  "$IMAGE" run -
