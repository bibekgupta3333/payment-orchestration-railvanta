#!/usr/bin/env bash
# Restore a snapshot made by scripts/backup-db.sh into a NEW Docker volume.
#
#   scripts/restore-backup.sh <backup.db> [--start]
#
# 1. Fence: stop every service so nothing can write or auto-restart.
# 2. Preserve: the current data volume is never modified or deleted.
# 3. Restore into a clean volume with the matching API image
#    (`railvanta-api restore`: manifest SHA-256, schema version, clean target,
#    PRAGMA integrity_check, PRAGMA foreign_key_check, domain checks). A failure
#    removes the restored file and exits non-zero without touching Compose.
# 4. With --start, start the stack on the restored volume and re-run the domain
#    check against the live database. Without it, the command to run is printed.
#
# Roll back by starting again with RAILVANTA_DATA_VOLUME set to the preserved
# volume name printed below.
set -euo pipefail

backup="${1:-}"
start="${2:-}"
[ -f "$backup" ] && [ -f "$backup.json" ] || { echo "usage: $0 <backup.db> [--start]  (needs <backup.db>.json next to it)" >&2; exit 2; }
backup_dir="$(cd "$(dirname "$backup")" && pwd)"
backup_name="$(basename "$backup")"
api_container="$(docker compose ps -a -q api || true)"
mounted=""
if [ -n "$api_container" ]; then
  mounted="$(docker inspect "$api_container" --format '{{range .Mounts}}{{.Name}}{{end}}')"
fi
old_volume="${mounted:-${RAILVANTA_DATA_VOLUME:-railvanta_railvanta-data}}"
image="${RAILVANTA_API_IMAGE:-railvanta-api:local}"
new_volume="${old_volume%%-restored-*}-restored-$(date -u +%Y%m%dT%H%M%SZ)"

echo "fence: stopping all services"
docker compose stop
echo "preserved volume: $old_volume"
# Reuse Compose ownership labels so Compose adopts the volume without warnings.
project="$(docker volume inspect -f '{{index .Labels "com.docker.compose.project"}}' "$old_volume" 2>/dev/null || true)"
volume_label="$(docker volume inspect -f '{{index .Labels "com.docker.compose.volume"}}' "$old_volume" 2>/dev/null || true)"
labels=()
if [ -n "$project" ] && [ -n "$volume_label" ]; then
  labels=(--label "com.docker.compose.project=$project" --label "com.docker.compose.volume=$volume_label")
fi
docker volume create ${labels[@]+"${labels[@]}"} "$new_volume" >/dev/null

if ! docker run --rm --user 0:0 --cap-drop ALL --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
    -v "$new_volume:/var/lib/railvanta" -v "$backup_dir:/backup:ro" \
    --entrypoint sh "$image" -c \
    "railvanta-api restore --backup /backup/$backup_name --dest-dir /var/lib/railvanta && chown -R 10001:10001 /var/lib/railvanta && chmod 700 /var/lib/railvanta"; then
  docker volume rm "$new_volume" >/dev/null || true
  echo "restore FAILED; the old volume $old_volume is untouched and the stack is stopped" >&2
  exit 1
fi

echo "restored volume: $new_volume"
echo "start with: RAILVANTA_DATA_VOLUME=$new_volume docker compose up --detach --wait"
if [ "$start" = "--start" ]; then
  RAILVANTA_DATA_VOLUME="$new_volume" docker compose up --detach --wait
  RAILVANTA_DATA_VOLUME="$new_volume" docker compose exec -T api railvanta-api check
  echo "RAILVANTA_DATA_VOLUME=$new_volume"
fi
