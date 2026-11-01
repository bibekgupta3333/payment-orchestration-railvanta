#!/usr/bin/env bash
# Take an API-owned SQLite snapshot from the running Compose stack and copy the
# snapshot plus its JSON manifest (snapshot time, schema version, SHA-256,
# release) out to a host directory. The snapshot is made by `railvanta-api
# backup` (VACUUM INTO, consistent under write load); the live .db/-wal/-shm
# files are never copied. Usage: scripts/backup-db.sh [host-dir]   (default ./backups)
set -euo pipefail

dest="${1:-backups}"
in_volume=/var/lib/railvanta/backups
mkdir -p "$dest"

output="$(docker compose exec -T api railvanta-api backup --out-dir "$in_volume")"
echo "$output"
snapshot="$(printf '%s\n' "$output" | sed -n 's/^backup=//p')"
[ -n "$snapshot" ] || { echo "backup produced no snapshot path" >&2; exit 1; }

docker compose cp "api:$snapshot" "$dest/"
docker compose cp "api:$snapshot.json" "$dest/"
# Keep the data volume lean: the host copy is now authoritative.
docker compose exec -T api rm -f "$snapshot" "$snapshot.json"

name="$(basename "$snapshot")"
# Re-verify the copied bytes against the manifest checksum.
expected="$(sed -n 's/.*"sha256": "\([0-9a-f]*\)".*/\1/p' "$dest/$name.json")"
actual="$(shasum -a 256 "$dest/$name" | cut -d' ' -f1)"
[ "$expected" = "$actual" ] || { echo "checksum mismatch after copy" >&2; exit 1; }
echo "host_copy=$dest/$name"
