#!/usr/bin/env bash
# Offline validation of infrastructure/k8s: render with kustomize, validate
# schemas with kubeconform (strict), and enforce the single-writer policy.
# Needs: kubectl (for `kubectl kustomize`) or kustomize, and kubeconform.
# Env: KUBECONFORM (binary, default: kubeconform), K8S_VERSION (default 1.35.0).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
kubeconform_bin="${KUBECONFORM:-kubeconform}"
k8s_version="${K8S_VERSION:-1.35.0}"

if command -v kustomize >/dev/null 2>&1; then
  render() { kustomize build "$1"; }
else
  render() { kubectl kustomize "$1"; }
fi

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

fail=0
for overlay in infrastructure/k8s/base infrastructure/k8s/overlays/k3d-local; do
  echo "== $overlay"
  render "$root/$overlay" >"$rendered"
  "$kubeconform_bin" -strict -summary -kubernetes-version "$k8s_version" -ignore-missing-schemas=false - <"$rendered" || fail=1
done
echo "== infrastructure/k8s/examples"
"$kubeconform_bin" -strict -summary -kubernetes-version "$k8s_version" "$root/infrastructure/k8s/examples" || fail=1

# Policy checks on the fully rendered overlay.
render "$root/infrastructure/k8s/overlays/k3d-local" >"$rendered"
check() { # description, command that must succeed
  if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi
}
check "no HorizontalPodAutoscaler" "! grep -q '^kind: HorizontalPodAutoscaler' '$rendered'"
check "no Job or CronJob" "! grep -Eq '^kind: (Job|CronJob)$' '$rendered'"
check "single StatefulSet replica" "grep -Eq '^  replicas: 1$' '$rendered' && [ \"\$(grep -c '^kind: StatefulSet' '$rendered')\" = 1 ]"
check "ReadWriteOncePod claim" "grep -q 'ReadWriteOncePod' '$rendered'"
check "service account tokens not mounted" "[ \"\$(grep -c 'automountServiceAccountToken: false' '$rendered')\" -ge 4 ]"
check "all containers drop ALL capabilities" "[ \"\$(grep -c -e '- ALL$' '$rendered')\" = 2 ]"
check "read-only root filesystems" "[ \"\$(grep -c 'readOnlyRootFilesystem: true' '$rendered')\" = 2 ]"
check "default-deny NetworkPolicy present" "grep -q 'name: default-deny-all' '$rendered'"
if [ "${REQUIRE_DIGESTS:-0}" = 1 ]; then
  check "every image is pinned by digest" "! grep -E '^ +image: ' '$rendered' | grep -vq '@sha256:'"
else
  echo "note REQUIRE_DIGESTS=1 not set: local tags allowed (reference deployment uses railvanta-*:local)"
fi

exit "$fail"
