#!/usr/bin/env bash
# Secret scan (WBS 5.2.10) with a pinned gitleaks container (no host install).
#
#   scripts/secret-scan.sh              scan the Git history and the working tree
#   scripts/secret-scan.sh image NAME…  scan the filesystem of built images
#
# Exits non-zero on any finding. Findings are redacted in the output.
set -euo pipefail

# ghcr.io/gitleaks/gitleaks v8.28.0, pinned by index digest.
GITLEAKS="ghcr.io/gitleaks/gitleaks@sha256:cdbb7c955abce02001a9f6c9f602fb195b7fadc1e812065883f695d1eeaba854"
repo="$(cd "$(dirname "$0")/.." && pwd)"
run() {
  docker run --rm -v "$1:/scan:ro" -v "$repo/.gitleaks.toml:/gitleaks.toml:ro" "$GITLEAKS" "${@:2}" \
    --config /gitleaks.toml --no-banner --redact --exit-code 1
}

if [ "${1:-}" = "image" ]; then
  shift
  [ "$#" -gt 0 ] || { echo "usage: $0 image NAME [NAME...]" >&2; exit 2; }
  for image in "$@"; do
    dir="$(mktemp -d)"
    container="$(docker create "$image")"
    docker export "$container" | tar -x -C "$dir" 2>/dev/null || true
    docker rm "$container" >/dev/null
    echo "scanning image filesystem: $image"
    status=0
    run "$dir" dir /scan || status=$?
    rm -rf "$dir"
    [ "$status" -eq 0 ] || exit "$status"
  done
  exit 0
fi

echo "scanning committed history (of a clean clone)"
clone="$(mktemp -d)"
trap 'rm -rf "$clone"' EXIT
git clone --quiet --no-hardlinks "$repo" "$clone/repo"
run "$clone/repo" git /scan
echo "scanning working tree"
run "$repo" dir /scan
