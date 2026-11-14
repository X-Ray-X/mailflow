#!/bin/sh
# Bootstrap the Mailflow mail infrastructure from an empty data directory.
#
# Idempotent: re-running on a bootstrapped server only re-applies the
# certificate, relaying, and account settings. It never deletes data.
#
# Requires: docker with the compose plugin, openssl, python3. HTTP calls run
# inside the mail container, which provides curl.
#
# Two phases, because Stalwart only exists in one of them at a time:
#   1. bootstrap mode - no config.json yet, plain HTTP on the setup port, and
#      only the x:Bootstrap/set management call is available.
#   2. normal mode   - after the config exists, the management API is served
#      over HTTPS on the HTTPS port and everything else can be configured.

set -eu

cd "$(dirname "$0")/.."

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ -f .env ] || die "missing .env; copy .env.example to .env and fill it in"
set -a
. ./.env
set +a

: "${STALWART_IMAGE:?STALWART_IMAGE must be set}"
: "${MAIL_DOMAIN:?MAIL_DOMAIN must be set}"
: "${MAIL_HOSTNAME:?MAIL_HOSTNAME must be set}"
: "${MAIL_BOOTSTRAP_PASSWORD:?MAIL_BOOTSTRAP_PASSWORD must be set}"
: "${MAIL_ACCOUNT_ALICE:?MAIL_ACCOUNT_ALICE must be set}"
: "${MAIL_ACCOUNT_ALICE_PASSWORD:?MAIL_ACCOUNT_ALICE_PASSWORD must be set}"
: "${MAIL_ACCOUNT_BOB:?MAIL_ACCOUNT_BOB must be set}"
: "${MAIL_ACCOUNT_BOB_PASSWORD:?MAIL_ACCOUNT_BOB_PASSWORD must be set}"

for cmd in docker openssl python3; do
  command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
done

DATA_DIR="${MAIL_DATA_DIR:-./data}"
CTR="mailflow-stalwart"

# The container's own listeners. The script reaches them with `docker exec`
# rather than through the published host ports: a containerized runner has its
# own loopback, so 127.0.0.1 there is not the host's.
SETUP_PORT=8080
HTTPS_PORT=443

DC="docker compose -f compose.yaml"
DCB="docker compose -f compose.yaml -f compose.bootstrap.yaml"
SETUP_API="http://127.0.0.1:${SETUP_PORT}/jmap/"
MGMT_API="https://127.0.0.1:${HTTPS_PORT}/jmap/"
CRED_FILE="${DATA_DIR}/secrets/admin.cred"

# --- filesystem ------------------------------------------------------------

# The image runs as uid/gid 2000. A bind mount created by root must be handed
# over, or the server cannot read the TLS key or write its data.
own_data() {
  if [ "$(id -u)" = "0" ]; then
    chown -R 2000:2000 "$DATA_DIR/etc" "$DATA_DIR/lib" "$DATA_DIR/tls"
  fi
}

mkdir -p "$DATA_DIR/etc" "$DATA_DIR/lib" "$DATA_DIR/tls" "$DATA_DIR/secrets"
chmod 700 "$DATA_DIR/secrets"
own_data

json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/\\t/g'
}

# --- TLS material ----------------------------------------------------------

TLS_NEW=no
gen_tls() {
  [ -f "$DATA_DIR/tls/fullchain.pem" ] && [ -f "$DATA_DIR/tls/privkey.pem" ] && return 0
  TLS_NEW=yes
  say "generating internal CA and server certificate for ${MAIL_HOSTNAME}"
  (
    cd "$DATA_DIR/tls"
    openssl req -x509 -newkey rsa:4096 -nodes -days 3650 \
      -keyout ca-key.pem -out ca.pem \
      -subj "/CN=Mailflow Internal CA" >/dev/null 2>&1
    openssl req -newkey rsa:2048 -nodes \
      -keyout privkey.pem -out server.csr \
      -subj "/CN=${MAIL_HOSTNAME}" >/dev/null 2>&1
    cat > san.cnf <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:${MAIL_HOSTNAME},DNS:${MAIL_DOMAIN}
EOF
    openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem \
      -CAcreateserial -out server.pem -days 825 -extfile san.cnf >/dev/null 2>&1
    cat server.pem ca.pem > fullchain.pem
    rm -f server.csr san.cnf ca.srl
    chmod 600 privkey.pem ca-key.pem
  )
}

# --- helpers ---------------------------------------------------------------

# Run curl inside the mail service. Its setup and management listeners are on
# the container's loopback; the published host ports are for external clients.
curl_ctr() {
  docker exec "$CTR" curl "$@"
}

wait_for() { # url, seconds
  i=0
  while [ "$i" -lt "$2" ]; do
    if curl_ctr -sk -o /dev/null "$1"; then return 0; fi
    i=$((i + 1))
    sleep 2
  done
  return 1
}

# A malformed body is answered with a bare 400 and no hint about which brace is
# wrong, so validate locally and fail with the body instead.
json_check() {
  printf '%s' "$1" | python3 -c 'import json,sys; json.load(sys.stdin)' >/dev/null 2>&1 \
    || die "internal error: generated invalid JSON: $1"
}

jmap_using() {
  printf '{"using":["urn:ietf:params:jmap:core","urn:stalwart:jmap"],"methodCalls":[%s]}' "$1"
}

call() { # api-url, credentials, method-call-json
  body=$(jmap_using "$3")
  json_check "$body"
  curl_ctr -sS -k -u "$2" -H 'Content-Type: application/json' \
    --data-binary "$body" "$1"
}

# Extract the first value of a JSON string field.
field() {
  sed -n "s/.*\"$1\":\"\\([^\"]*\\)\".*/\\1/p" | head -n 1
}

# --- phase 1: bootstrap ----------------------------------------------------

if [ ! -f "$DATA_DIR/etc/config.json" ]; then
  say "no configuration found; starting in bootstrap mode"
  gen_tls
  own_data
  $DCB up -d
  wait_for "http://127.0.0.1:${SETUP_PORT}/jmap/session" 60 \
    || die "setup listener did not answer inside ${CTR}"

  RESP=$(call "$SETUP_API" "admin:${MAIL_BOOTSTRAP_PASSWORD}" \
    "[\"x:Bootstrap/set\",{\"update\":{\"singleton\":{
      \"defaultHostname\":\"$(json_escape "$MAIL_HOSTNAME")\",
      \"defaultDomain\":\"$(json_escape "$MAIL_DOMAIN")\",
      \"requestTlsCertificate\":false,
      \"generateDkimKeys\":false,
      \"dnsServer\":{\"@type\":\"Manual\"},
      \"tracer\":{\"@type\":\"Stdout\",\"ansi\":false,\"enable\":true,\"level\":\"info\"}
    }}},\"c1\"]")

  ADMIN_USER=$(printf '%s' "$RESP" | field username)
  ADMIN_SECRET=$(printf '%s' "$RESP" | field secret)
  [ -n "$ADMIN_USER" ] && [ -n "$ADMIN_SECRET" ] \
    || die "bootstrap did not return administrator credentials: $RESP"

  umask 077
  printf '%s:%s\n' "$ADMIN_USER" "$ADMIN_SECRET" > "$CRED_FILE"
  chmod 600 "$CRED_FILE"
  say "created administrator ${ADMIN_USER}"

  # Recreate without the bootstrap credential; the configuration now exists.
  $DC up -d --force-recreate
else
  say "configuration present; skipping bootstrap"
  [ -f "$CRED_FILE" ] \
    || die "configuration exists but ${CRED_FILE} is missing; restore it or remove ${DATA_DIR} (destroys all mail) and bootstrap again"
  gen_tls
  own_data
  $DC up -d
fi

wait_for "https://127.0.0.1:${HTTPS_PORT}/healthz/live" 60 \
  || die "HTTPS endpoint did not become healthy"

ADMIN=$(cat "$CRED_FILE")

# --- phase 2: certificate and accounts -------------------------------------

CERTS=$(call "$MGMT_API" "$ADMIN" '["x:Certificate/get",{},"c1"]')
need_reload=no
if printf '%s' "$CERTS" | grep -q '"list":\[\]'; then
  say "installing certificate object from ${DATA_DIR}/tls"
  RESP=$(call "$MGMT_API" "$ADMIN" \
    '["x:Certificate/set",{"create":{"c1":{
      "certificate":{"@type":"File","filePath":"/etc/stalwart/tls/fullchain.pem"},
      "privateKey":{"@type":"File","filePath":"/etc/stalwart/tls/privkey.pem"}
    }}},"c2"]')
  printf '%s' "$RESP" | grep -q '"created"' \
    || die "certificate installation failed: $RESP"
  need_reload=yes
else
  say "certificate object already present"
  # Regenerated files are only read at reload, even though the object exists.
  if [ "$TLS_NEW" = yes ]; then need_reload=yes; fi
fi

if [ "$need_reload" = yes ]; then
  # A newly created Certificate object is not used until the certificates are
  # reloaded or the server restarts. Reload instead of restarting.
  RESP=$(call "$MGMT_API" "$ADMIN" \
    '["x:Action/set",{"create":{"r1":{"@type":"ReloadTlsCertificates"}}},"c3"]')
  printf '%s' "$RESP" | grep -q '"created"' \
    || die "certificate reload failed: $RESP"
  say "certificate installed and reloaded"
fi

# Refuse recipients outside the hosted domain even for authenticated sessions.
# The default MtaStageRcpt expression allows authenticated users to relay, which
# is how a message to an external domain would leave this host. This deployment
# must never attempt external delivery, so relaying is refused for every session.
RESP=$(call "$MGMT_API" "$ADMIN" \
  '["x:MtaStageRcpt/set",{"update":{"singleton":{"allowRelaying":{"else":"false"}}}},"c1"]')
printf '%s' "$RESP" | grep -q '"updated"' \
  || die "could not disable relaying: $RESP"

# A settings singleton is stored immediately but only applied to the running
# server on reload.
RESP=$(call "$MGMT_API" "$ADMIN" \
  '["x:Action/set",{"create":{"r1":{"@type":"ReloadSettings"}}},"c4"]')
printf '%s' "$RESP" | grep -q '"created"' \
  || die "settings reload failed: $RESP"

DOMAINS=$(call "$MGMT_API" "$ADMIN" '["x:Domain/get",{},"c1"]')
# Domain objects carry no id field; the primary domain is the response's
# accountId, and account objects reference it as domainId.
DOMAIN_ID=$(printf '%s' "$DOMAINS" \
  | sed -n 's/.*"x:Domain\/get",{"accountId":"\([^"]*\)".*/\1/p' | head -n 1)
[ -n "$DOMAIN_ID" ] || die "could not determine the domain id: $DOMAINS"

printf '%s' "$DOMAINS" | grep -q "\"name\":\"$(json_escape "$MAIL_DOMAIN")\"" \
  || die "domain ${MAIL_DOMAIN} is not configured: $DOMAINS"

ACCOUNTS=$(call "$MGMT_API" "$ADMIN" '["x:Account/get",{},"c1"]')

ensure_account() { # name, password
  if printf '%s' "$ACCOUNTS" | grep -q "\"name\":\"$1\""; then
    say "account $1 exists"
    return 0
  fi
  say "creating account $1"
  RESP=$(call "$MGMT_API" "$ADMIN" \
    "[\"x:Account/set\",{\"create\":{\"u1\":{
      \"@type\":\"User\",
      \"name\":\"$(json_escape "$1")\",
      \"domainId\":\"$(json_escape "$DOMAIN_ID")\",
      \"credentials\":{\"0\":{\"@type\":\"Password\",\"secret\":\"$(json_escape "$2")\"}},
      \"roles\":{\"@type\":\"User\"},
      \"permissions\":{\"@type\":\"Inherit\"},
      \"encryptionAtRest\":{\"@type\":\"Disabled\"}
    }}},\"c1\"]")
  printf '%s' "$RESP" | grep -q '"created"' \
    || die "account creation failed for $1: $RESP"
}

ensure_account "$MAIL_ACCOUNT_ALICE" "$MAIL_ACCOUNT_ALICE_PASSWORD"
ensure_account "$MAIL_ACCOUNT_BOB" "$MAIL_ACCOUNT_BOB_PASSWORD"

say ""
say "mail infrastructure ready"
say "  hostname : ${MAIL_HOSTNAME}"
say "  domain   : ${MAIL_DOMAIN}"
say "  IMAPS    : ${MAIL_HOSTNAME}:${MAIL_IMAPS_PORT:-993}"
say "  SMTP     : ${MAIL_HOSTNAME}:${MAIL_SUBMISSION_PORT:-465}"
say "  accounts : ${MAIL_ACCOUNT_ALICE}@${MAIL_DOMAIN}, ${MAIL_ACCOUNT_BOB}@${MAIL_DOMAIN}"
say "  client CA: ${DATA_DIR}/tls/ca.pem"
