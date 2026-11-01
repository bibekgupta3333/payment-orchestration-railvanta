#!/usr/bin/env bash
# Restore drill (WBS 5.2 gate): take a backup while payments are being created,
# restore it into a clean volume, start the stack on it and run the domain check.
# Run against a RUNNING, freshly created sandbox stack (`pnpm compose:up`); it
# seeds demo credentials with `railvanta-api seed-demo`, which refuses to reseed
# with different credentials, so use a fresh volume. Compose settings come from
# the usual COMPOSE_* / RAILVANTA_* environment.
#
#   scripts/restore-drill.sh [seconds-of-load=15] [concurrency=8]
set -euo pipefail

seconds="${1:-15}"
workers="${2:-8}"
origin="http://localhost:${RAILVANTA_PORT:-3000}"
work="$(mktemp -d)"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$work"' EXIT

operator="operator.$(openssl rand -hex 32)"
docker compose exec -T \
  -e "DEMO_OPERATOR_KEY=$operator" \
  -e "DEMO_READER_KEY=reader.$(openssl rand -hex 32)" \
  -e "DEMO_OTHER_WORKSPACE_KEY=other.$(openssl rand -hex 32)" \
  api railvanta-api seed-demo
method="00000000-0000-7000-8000-000000000121" # seeded mock method: always succeeds

load() {
  local id="$1" end=$((SECONDS + seconds)) n=0
  while [ "$SECONDS" -lt "$end" ]; do
    n=$((n + 1))
    curl -s -o /dev/null -w '%{http_code}\n' -X POST "$origin/v1/payments" \
      -H "Authorization: Bearer $operator" -H "Content-Type: application/json" \
      -H "Idempotency-Key: drill-$id-$n-$RANDOM" \
      -d "{\"amount_minor\":\"1000\",\"currency\":\"USD\",\"payment_method_id\":\"$method\",\"merchant_reference\":\"drill-$id-$n\"}" \
      >>"$work/codes.$id"
  done
}
for i in $(seq 1 "$workers"); do load "$i" & done

sleep 2
echo "== backup under load (t+2s) =="
scripts/backup-db.sh "$work/backups"
wait
accepted="$(cat "$work"/codes.* | grep -c '^202$' || true)"
echo "== load finished: $accepted payments accepted =="
sleep 3 # let in-flight dispatches settle
echo "== live database after load =="
docker compose exec -T api railvanta-api check

echo "== restore =="
snapshot="$(ls "$work"/backups/*.db)"
scripts/restore-backup.sh "$snapshot" --start
echo "== settling restored in-flight work, then re-checking =="
sleep 8
docker compose exec -T api railvanta-api check
echo "== drill passed: $(date -u +%Y-%m-%dT%H:%M:%SZ) release=$(sed -n 's/.*"version": "\(.*\)".*/\1/p' "$snapshot.json") =="
