#!/usr/bin/env bash
# Disposable-cluster validation of infrastructure/k8s (WBS 5.3). Local and free:
# creates a k3d cluster (k3s ships traefik and an enforcing NetworkPolicy
# controller), imports the locally built images, applies the k3d-local overlay,
# checks the sandbox through the Ingress over TLS, runs the API pod-delete drill
# and a NetworkPolicy check, then deletes the cluster.
#
# Prerequisites: docker, k3d, kubectl, curl, jq, openssl, and the images
# railvanta-api:local and railvanta-web:local (docker compose build).
# Env: KEEP=1 keeps the cluster; CLUSTER, PORT override defaults.
# Test-only credentials and the throwaway TLS key live in a temp dir that is
# deleted on exit; nothing is written into the repository.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
CLUSTER="${CLUSTER:-railvanta-k8s-test}"
PORT="${PORT:-18443}"
HOST=railvanta.test
CTX="k3d-$CLUSTER"
NS=railvanta-sandbox
work="$(mktemp -d)"
K() { kubectl --context "$CTX" -n "$NS" "$@"; }
log() { printf '\n== %s\n' "$*"; }
die() { echo "FAIL: $*" >&2; exit 1; }

cleanup() {
  docker rm -f rv-seed >/dev/null 2>&1 || true
  rm -rf "$work"
  if [ "${KEEP:-0}" != 1 ]; then
    log "teardown"
    k3d cluster delete "$CLUSTER" || true
  fi
}
trap cleanup EXIT

curl_ingress() { # curl against the Ingress over TLS with the throwaway CA
  curl -sS --resolve "$HOST:$PORT:127.0.0.1" --cacert "$work/tls.crt" "$@"
}
api() { # method path [json]; operator API key, never printed
  local method=$1 path=$2 body=${3:-}
  if [ -n "$body" ]; then
    curl_ingress -X "$method" -H "Authorization: Bearer $OP" -H 'Content-Type: application/json' \
      -H "Idempotency-Key: k8s_$(openssl rand -hex 8)" -d "$body" "https://$HOST:$PORT$path"
  else
    curl_ingress -X "$method" -H "Authorization: Bearer $OP" "https://$HOST:$PORT$path"
  fi
}

log "1. cluster (k3d, k3s with traefik and the kube-router network policy controller)"
if ! k3d cluster list "$CLUSTER" >/dev/null 2>&1; then
  k3d cluster create "$CLUSTER" --servers 1 --agents 0 -p "127.0.0.1:$PORT:443@loadbalancer" --wait --timeout 240s
fi
k3d image import railvanta-api:local railvanta-web:local -c "$CLUSTER"
until kubectl --context "$CTX" -n kube-system get deployment/traefik >/dev/null 2>&1; do sleep 2; done # created by the k3s helm job
kubectl --context "$CTX" -n kube-system rollout status deployment/traefik --timeout=180s

log "2. throwaway TLS Secret and demo-seeded database (test time only)"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=$HOST" -addext "subjectAltName=DNS:$HOST" \
  -keyout "$work/tls.key" -out "$work/tls.crt" 2>/dev/null
# DEMO_MODE needs a loopback bind, so it cannot run in the cluster. Seed a
# database in a throwaway container and copy it into the PVC while the API is
# scaled to zero; the API is the only DB writer once it runs.
OP="kop1.$(openssl rand -hex 16)"
RD="krd1.$(openssl rand -hex 16)"
OT="kot1.$(openssl rand -hex 16)"
mkdir "$work/seed" && chmod 777 "$work/seed"
docker run -d --name rv-seed -v "$work/seed:/var/lib/railvanta" -e APP_ENV=sandbox -e DEMO_MODE=true \
  -e HTTP_ADDR=127.0.0.1:8080 -e DEMO_OPERATOR_KEY="$OP" -e DEMO_READER_KEY="$RD" \
  -e DEMO_OTHER_WORKSPACE_KEY="$OT" railvanta-api:local >/dev/null
for _ in $(seq 20); do docker logs rv-seed 2>&1 | grep -q 'sandbox API ready' && break; sleep 1; done
docker logs rv-seed 2>&1 | grep -q 'sandbox API ready' || die "seed container did not start"
docker stop rv-seed >/dev/null
docker rm rv-seed >/dev/null
[ -s "$work/seed/railvanta.db" ] || die "seed database missing"

log "3. apply manifests"
kubectl --context "$CTX" apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata: {name: $NS}
EOF
K create secret tls railvanta-tls --cert="$work/tls.crt" --key="$work/tls.key"
kubectl --context "$CTX" apply -k "$root/infrastructure/k8s/overlays/k3d-local"
K rollout status statefulset/railvanta-api --timeout=180s
K rollout status deployment/railvanta-web --timeout=180s
K get pods,pvc,svc,ingress,networkpolicy
echo "PVC access modes: $(K get pvc data-railvanta-api-0 -o jsonpath='{.spec.accessModes}')"

log "4. load the seeded database into the PVC (API scaled to 0)"
K scale statefulset/railvanta-api --replicas=0
K wait --for=delete pod/railvanta-api-0 --timeout=60s
K apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata: {name: seed-loader}
spec:
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext: {runAsNonRoot: true, runAsUser: 10001, runAsGroup: 10001, fsGroup: 10001, seccompProfile: {type: RuntimeDefault}}
  containers:
    - name: loader
      image: railvanta-api:local
      imagePullPolicy: IfNotPresent
      command: [sleep, "300"]
      securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
      volumeMounts: [{name: data, mountPath: /var/lib/railvanta}]
  volumes: [{name: data, persistentVolumeClaim: {claimName: data-railvanta-api-0}}]
EOF
K wait --for=condition=Ready pod/seed-loader --timeout=90s
K exec seed-loader -- sh -c 'rm -f /var/lib/railvanta/railvanta.db*'
ls -l "$work/seed"
for f in "$work"/seed/railvanta.db*; do # the database and any uncheckpointed -wal
  case "$f" in *-shm) continue ;; esac
  # kubectl cp (tar) copies binary files completely; `exec -i cat` truncated at 32 KiB.
  K cp "$f" "seed-loader:/var/lib/railvanta/$(basename "$f")"
done
K exec seed-loader -- ls -l /var/lib/railvanta
K delete pod seed-loader --wait=true
K scale statefulset/railvanta-api --replicas=1
K rollout status statefulset/railvanta-api --timeout=180s

log "5. sandbox through the Ingress over TLS"
for _ in $(seq 30); do
  code="$(curl_ingress -o /dev/null -w '%{http_code}' "https://$HOST:$PORT/" || true)"
  [ "$code" = 200 ] && break
  sleep 2
done
echo "GET / -> $code (web through Ingress, TLS verified against throwaway CA)"
[ "$code" = 200 ] || die "web not reachable through ingress"
for _ in $(seq 30); do # traefik may need a moment to see the new endpoints
  code="$(curl_ingress -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $OP" "https://$HOST:$PORT/v1/workspace" || true)"
  [ "$code" = 200 ] && break
  sleep 2
done
echo "GET /v1/workspace with operator key via Ingress /v1 -> $code"
[ "$code" = 200 ] || die "API not reachable through ingress"
echo "GET /v1/workspace without credentials -> $(curl_ingress -o /dev/null -w '%{http_code}' "https://$HOST:$PORT/v1/workspace")"
echo "GET /health/ready via ingress -> $(curl_ingress -o /dev/null -w '%{http_code}' "https://$HOST:$PORT/health/ready") (must not be the API health endpoint)"
method=00000000-0000-7000-8000-000000000121
create_payment() {
  api POST /v1/payments "{\"amount_minor\":\"25000\",\"currency\":\"USD\",\"payment_method_id\":\"$method\",\"merchant_reference\":\"k8s_$(openssl rand -hex 6)\"}" | jq -r .id
}
wait_status() { # id status
  for _ in $(seq 60); do
    [ "$(api GET "/v1/payments/$1" | jq -r .status)" = "$2" ] && return 0
    sleep 1
  done
  return 1
}
ids=()
for _ in 1 2 3; do ids+=("$(create_payment)"); done
for id in "${ids[@]}"; do wait_status "$id" succeeded || die "payment $id did not succeed"; done
echo "3 payments succeeded: ${ids[*]}"
ledger_count() { api GET '/v1/ledger/transactions?limit=100' | jq '.items | length'; }
before="$(ledger_count)"
echo "ledger transactions before pod delete: $before"

log "6. pod-delete drill"
old_uid="$(K get pod railvanta-api-0 -o jsonpath='{.metadata.uid}')"
K logs -f railvanta-api-0 >"$work/old.log" 2>&1 &
logger_pid=$!
ids2=()
for _ in 1 2 3; do ids2+=("$(create_payment)"); done # in flight when the pod is deleted
K delete pod railvanta-api-0 --wait=true
wait "$logger_pid" 2>/dev/null || true
K rollout status statefulset/railvanta-api --timeout=180s
new_uid="$(K get pod railvanta-api-0 -o jsonpath='{.metadata.uid}')"
[ "$old_uid" != "$new_uid" ] || die "pod was not replaced"
echo "old pod uid $old_uid, new pod uid $new_uid"
echo "old pod last log:  $(tail -n 1 "$work/old.log" | cut -c1-200)"
echo "new pod first log: $(K logs railvanta-api-0 | head -n 1 | cut -c1-200)"
# ReadWriteOncePod plus StatefulSet ordering: the replacement pod is created
# only after the old pod object is gone (`delete --wait` above returned first).
echo "new pod startup warnings: $(K logs railvanta-api-0 | grep -E 'stale submitted|error' | cut -c1-200 || true)"
for id in "${ids[@]}" "${ids2[@]}"; do wait_status "$id" succeeded || die "payment $id not succeeded after replacement"; done
echo "all 6 payments succeeded after replacement"
after="$(ledger_count)"
echo "ledger transactions after: $after (expected $((before + 3)))"
[ "$after" = "$((before + 3))" ] || die "ledger count mismatch: duplicate or missing postings"
dups="$(api GET '/v1/ledger/transactions?limit=100' | jq '[.items[].payment_id] | length - (unique | length)')"
echo "duplicate postings per payment: $dups"
[ "$dups" = 0 ] || die "duplicate postings"
echo "restarts after replacement: $(K get pod railvanta-api-0 -o jsonpath='{.status.containerStatuses[0].restartCount}')"

log "7. single-writer inventory"
echo "StatefulSet replicas: $(K get statefulset railvanta-api -o jsonpath='{.spec.replicas}'), ready: $(K get statefulset railvanta-api -o jsonpath='{.status.readyReplicas}')"
echo "Jobs: $(K get jobs -o name | wc -l | tr -d ' '), CronJobs: $(K get cronjobs -o name | wc -l | tr -d ' '), HPAs: $(K get hpa -o name | wc -l | tr -d ' ')"
echo "Pods mounting the data claim: $(K get pods -o json | jq -r '[.items[] | select(.spec.volumes[]?.persistentVolumeClaim.claimName == "data-railvanta-api-0") | .metadata.name] | join(",")')"
K get pv -o custom-columns=NAME:.metadata.name,RECLAIM:.spec.persistentVolumeReclaimPolicy,ACCESS:.spec.accessModes

log "8. NetworkPolicy enforcement (k3s kube-router controller); probe pods run nc -z"
probe() { # name labels(or empty) host port
  local labels=""
  [ -n "$2" ] && labels="--labels=$2"
  # shellcheck disable=SC2086
  K run "$1" $labels --image=railvanta-api:local --image-pull-policy=IfNotPresent --restart=Never --rm -i --quiet \
    --overrides='{"spec":{"automountServiceAccountToken":false,"securityContext":{"runAsNonRoot":true,"runAsUser":10001,"seccompProfile":{"type":"RuntimeDefault"}},"containers":[{"name":"'"$1"'","image":"railvanta-api:local","imagePullPolicy":"IfNotPresent","command":["nc","-z","-w","4","'"$3"'","'"$4"'"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}}]}}' \
    >/dev/null 2>&1 && echo allowed || echo blocked
}
web=app.kubernetes.io/name=railvanta-web
echo "web-labelled -> railvanta-api:8080 (DNS + web->API flow): $(probe np-web "$web" railvanta-api 8080) (expect allowed)"
echo "unlabelled   -> railvanta-api:8080:                       $(probe np-none1 '' railvanta-api 8080) (expect blocked)"
echo "unlabelled   -> railvanta-web:3000:                       $(probe np-none2 '' railvanta-web 3000) (expect blocked)"
echo "web-labelled -> railvanta-web:3000 (no such flow):        $(probe np-web2 "$web" railvanta-web 3000) (expect blocked)"
# The target below has no ingress policy, so a block can only come from egress.
echo "web-labelled -> traefik.kube-system:80 (egress only):    $(probe np-egress "$web" traefik.kube-system 80) (expect blocked)"
echo "unlabelled   -> traefik.kube-system:80 (default deny):    $(probe np-egress2 '' traefik.kube-system 80) (expect blocked)"
# Observed in k3d on Docker Desktop: connections to public IPs (1.1.1.1:80)
# from a default-deny pod still succeed, so external egress is NOT verified.
echo "default-deny pod -> 1.1.1.1:80 (informational):           $(probe np-ext '' 1.1.1.1 80) (external egress enforcement not relied upon)"

log "done"
