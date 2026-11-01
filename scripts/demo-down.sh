#!/usr/bin/env bash
# Stop the demo stack. `pnpm demo:down -v` also deletes the SQLite volume (all demo data) and
# the stored credentials, so the next `pnpm demo:up` starts clean.
set -euo pipefail
cd "$(dirname "$0")/.."
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.demo.yml)
if [ "${1:-}" = "-v" ]; then
  echo "WARNING: -v deletes the demo volume (all payments, journals, keys) and .demo/credentials.env." >&2
  "${COMPOSE[@]}" down -v
  rm -f .demo/credentials.env
else
  "${COMPOSE[@]}" down
  echo "Stopped. Data and .demo/credentials.env kept; 'pnpm demo:down -v' deletes them."
fi
