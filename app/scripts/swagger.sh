#!/usr/bin/env bash
# Generate a local Swagger view from the handler annotations and optionally
# serve it with an ephemeral loopback-only UI container.
#
# This is developer tooling. Generated files are ignored and are not part of
# the published contract or any deployment.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/app/docs/generated"
GO_IMAGE="${GO_IMAGE:-golang:1.27.4-alpine}"
SWAG_VERSION="${SWAG_VERSION:-v1.16.4}"
UI_IMAGE="${UI_IMAGE:-swaggerapi/swagger-ui:v5.20.1}"
PORT="${SWAGGER_PORT:-8088}"

mkdir -p "$OUT"
docker run --rm -v "$ROOT/app:/src" -w /src "$GO_IMAGE" \
  go run "github.com/swaggo/swag/cmd/swag@${SWAG_VERSION}" init \
  -g doc.go -d internal/api -o docs/generated \
  --outputTypes json,yaml

for file in swagger.json swagger.yaml; do
  test -s "$OUT/$file" || { echo "generation did not produce $file" >&2; exit 1; }
done

python3 - "$OUT/swagger.json" <<'PY'
import json, sys
expected = {
    '/healthz',
    '/v1/mailboxes', '/v1/mailboxes/{mailboxID}',
    '/v1/mailboxes/{mailboxID}/messages',
    '/v1/messages', '/v1/messages/{messageID}',
    '/v1/messages/{messageID}/reply',
    '/v1/messages/{messageID}/attachments',
    '/v1/messages/{messageID}/attachments/{position}',
    '/v1/drafts', '/v1/drafts/{draftID}/send',
    '/v1/search',
}
paths = set(json.load(open(sys.argv[1])).get('paths', {}))
missing = sorted(expected - paths)
if missing:
    print('generated spec is missing routes:', ', '.join(missing), file=sys.stderr)
    sys.exit(1)
print(f'generated spec covers {len(paths)} routes')
PY

if [ "${1:-}" = "--no-serve" ]; then
  echo "Spec written to $OUT (not serving)"
  exit 0
fi

if docker ps -a --format '{{.Names}}' | grep -qx mailflow-swagger-ui; then
  echo "container mailflow-swagger-ui already exists; stop it with: docker rm -f mailflow-swagger-ui" >&2
  exit 1
fi

docker run -d --name mailflow-swagger-ui --rm \
  -p "127.0.0.1:${PORT}:8080" \
  -e SWAGGER_JSON=/spec/swagger.json \
  -v "$OUT:/spec:ro" \
  "$UI_IMAGE" >/dev/null

echo "Swagger UI: http://127.0.0.1:${PORT}/"
echo "Stop it with: docker rm -f mailflow-swagger-ui"
