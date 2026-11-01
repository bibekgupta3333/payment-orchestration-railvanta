#!/usr/bin/env bash
# One-command demo launcher: start the stack (no build unless DEMO_BUILD=1), create throwaway
# demo credentials in .demo/credentials.env (git-ignored), seed them, print the API key,
# curl examples and the playground URL. Run from the repository root via `pnpm demo:up`.
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.demo.yml)
CRED_DIR=.demo
CRED_FILE="$CRED_DIR/credentials.env"
PORT="${RAILVANTA_PORT:-3000}"
BASE="http://localhost:${PORT}"

up_args=(up --detach --wait)
if [ "${DEMO_BUILD:-0}" = "1" ]; then
  up_args=(up --build --detach --wait)
  echo "DEMO_BUILD=1: building images (slow)."
fi
"${COMPOSE[@]}" "${up_args[@]}"

mkdir -p "$CRED_DIR"
chmod 700 "$CRED_DIR"
fresh=0
if [ ! -f "$CRED_FILE" ]; then
  fresh=1
  umask 077
  cat >"$CRED_FILE" <<CRED
DEMO_OPERATOR_KEY=oper.$(openssl rand -hex 16)
DEMO_READER_KEY=read.$(openssl rand -hex 16)
DEMO_OTHER_WORKSPACE_KEY=othr.$(openssl rand -hex 16)
DEMO_SESSION_TOKEN=$(openssl rand -hex 16)
DEMO_CSRF_TOKEN=$(openssl rand -hex 16)
CRED
fi
# Export the credentials: `docker compose exec -e NAME` forwards only exported variables.
set -a
# shellcheck disable=SC1090
. "$CRED_FILE"
set +a

seed_out=""
if ! seed_out=$("${COMPOSE[@]}" exec -T \
  -e DEMO_OPERATOR_KEY -e DEMO_READER_KEY -e DEMO_OTHER_WORKSPACE_KEY \
  -e DEMO_SESSION_TOKEN -e DEMO_CSRF_TOKEN \
  api railvanta-api seed-demo 2>&1); then
  echo "$seed_out" >&2
  if [ "$fresh" = "1" ]; then
    rm -f "$CRED_FILE"
    echo >&2
    echo "Seeding failed with newly generated credentials. Read the error above." >&2
    echo "If it says the database was already seeded with different credentials, the" >&2
    echo "volume holds credentials this machine does not have; reset it and start over:" >&2
    echo "  pnpm demo:down -v && pnpm demo:up    # WARNING: -v deletes all demo data" >&2
  else
    echo >&2
    echo "Seeding failed with the stored credentials. If the volume was recreated or seeded" >&2
    echo "by something else, reset: pnpm demo:down -v && rm -rf .demo && pnpm demo:up" >&2
  fi
  exit 1
fi
echo "seed-demo: $seed_out"

METHOD=00000000-0000-7000-8000-000000000121
cat <<EOF2

================ Railvanta demo is up ================
Base URL:       $BASE
Playground:     $BASE/dashboard/playground   (run: pnpm demo:open for a pre-signed-in browser)
Operator key:   $DEMO_OPERATOR_KEY
Credentials:    $CRED_FILE (throwaway, git-ignored)

# create a \$100.00 payment on the success method (...0121); amounts are minor units
export KEY=$DEMO_OPERATOR_KEY
curl -s -X POST $BASE/v1/payments \\
  -H "Authorization: Bearer \$KEY" -H "Idempotency-Key: demo-100" -H "Content-Type: application/json" \\
  -d '{"amount_minor":"10000","currency":"USD","payment_method_id":"$METHOD"}'

# replay the same key and body: byte-identical response, no second payment
curl -s -X POST $BASE/v1/payments \\
  -H "Authorization: Bearer \$KEY" -H "Idempotency-Key: demo-100" -H "Content-Type: application/json" \\
  -d '{"amount_minor":"10000","currency":"USD","payment_method_id":"$METHOD"}'

# same key, changed body: 409
curl -s -i -X POST $BASE/v1/payments \\
  -H "Authorization: Bearer \$KEY" -H "Idempotency-Key: demo-100" -H "Content-Type: application/json" \\
  -d '{"amount_minor":"99999","currency":"USD","payment_method_id":"$METHOD"}'

# list payments
curl -s -H "Authorization: Bearer \$KEY" $BASE/v1/payments
======================================================
EOF2
