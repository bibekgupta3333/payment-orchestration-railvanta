# Kubernetes (optional)

Optional M6 sandbox deployment, after local and container delivery pass. Migration, probe and restore procedures are in [deployment](deployment.md).

## Resource relationship diagram

Solid arrows describe traffic, selection, or configuration. Labeled arrows distinguish workload ownership from storage binding; the diagram does not imply that every relationship is a Kubernetes owner reference.

```mermaid
flowchart TB
    Browser["Browser"]
    Controller["Installed Ingress controller"]
    subgraph NS["Namespace: railvanta-sandbox"]
        Ingress["Ingress: railvanta<br/>HTTPS host; /v1 and /"]
        TLS["Secret: railvanta-tls"]
        WebSvc["Service: railvanta-web<br/>ClusterIP :3000"]
        APISvc["Service: railvanta-api<br/>ClusterIP :8080"]
        Headless["Service: railvanta-api-headless<br/>clusterIP: None"]
        WebDeploy["Deployment: railvanta-web"]
        RS["ReplicaSet"]
        WebPods["Next.js pods<br/>initial replicas: 1"]
        APISet["StatefulSet: railvanta-api<br/>replicas: 1; serviceName: headless service"]
        APIPod["Pod: railvanta-api-0<br/>Go HTTP API and dispatcher"]
        PVC["PVC: data-railvanta-api-0"]
        Config["ConfigMap: railvanta-config<br/>sandbox settings"]
        Secret["Secret: railvanta-api-secrets<br/>server-only credentials"]
        SA["ServiceAccounts<br/>web and api; no API token automount"]
        NP["NetworkPolicies<br/>default deny plus explicit allowances"]
    end
    PV["PersistentVolume<br/>filesystem on block device / local disk"]
    SC["StorageClass<br/>compatible CSI or local provisioner"]
    Browser -->|HTTPS| Controller
    Controller -->|implements| Ingress
    Ingress -->|TLS certificate| TLS
    Ingress -->|"Prefix /v1"| APISvc
    Ingress -->|"Prefix /"| WebSvc
    WebSvc -->|selects| WebPods
    APISvc -->|selects| APIPod
    Headless -->|selects| APIPod
    WebDeploy -->|owns| RS
    RS -->|owns| WebPods
    APISet -->|owns| APIPod
    APISet -->|volumeClaimTemplates creates| PVC
    APIPod -->|mounts at /var/lib/railvanta| PVC
    PVC -->|binds| PV
    SC -->|provisions or selects backing storage| PV
    Config --> WebPods
    Config --> APIPod
    Secret --> APIPod
    SA --> WebPods
    SA --> APIPod
    NP -.->|constrains traffic| WebPods
    NP -.->|constrains traffic| APIPod
```

## Resource catalog

| Resource | Proposed specification | Reason |
| --- | --- | --- |
| Namespace | `railvanta-sandbox` | Scope sandbox resources and quotas |
| Ingress | HTTPS; `/v1` Prefix to API; `/` Prefix to frontend; no path stripping | Keep browser API traffic on the app origin |
| TLS Secret | Certificate for the selected sandbox hostname | TLS termination at the controller |
| Frontend Deployment | `railvanta-web`; initially one pod; port `3000` | Independently replaceable Next.js runtime |
| API StatefulSet | `railvanta-api`; exactly one pod; port `8080` | Stable pod identity and persistent claim association |
| Services | Frontend and API ClusterIP; API headless governing service | Internal routing and StatefulSet network identity |
| PVC | `data-railvanta-api-0`; filesystem volume; initial sizing after measurement | Persist SQLite database and sidecar files |
| ConfigMap | Sandbox mode, API path/port, internal API origin | Nonsecret runtime configuration |
| API Secret | Session material; later provider sandbox credentials | Prevent secrets entering frontend bundles |
| ServiceAccounts | Separate API and web accounts; token automount disabled | Workloads require no Kubernetes API access |
| NetworkPolicies | Deny by default, then permit listed flows | Bound ingress and egress using an enforcing CNI |
| ResourceQuota / LimitRange | Budget-approved namespace ceilings and defaults | Prevent accidental sandbox resource sprawl |

Choose a CSI driver supporting `ReadWriteOncePod` when available and validated; otherwise enforce exclusive ownership operationally with `ReadWriteOnce` and storage fencing. `ReadWriteOnce` is a node-level access mode, not a one-pod lock. A PVC alone does not enforce the application's single-owner rule. Retain the volume across workload deletion and require explicit data-retention decisions. [Persistent volume access modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes)

StatefulSets associate stable identities with storage but do not make this single-replica service highly available. Keep the API replica count fixed at one and do not attach an API HPA. The frontend may scale after its session and caching behavior is tested. [StatefulSet documentation](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)

## Storage and scheduling

- Mount the whole `/var/lib/railvanta` directory. The SQLite owner needs write access for `railvanta.db`, `railvanta.db-wal`, and `railvanta.db-shm`.
- Use a block device mounted as a local filesystem or a true local volume. Do not use a multi-node shared filesystem for WAL. [SQLite WAL constraints](https://www.sqlite.org/wal.html)
- For node-local storage, use topology-aware scheduling and accept node-loss downtime. For reattachable volumes, validate attachment exclusivity and the storage provider's fencing procedure.
- Set storage reclaim and claim-retention behavior deliberately. Namespace deletion is not a backup plan.
- Do not create a separate worker Deployment, migration Job, backup CronJob, or sidecar that independently writes the live database. The API process runs the dispatcher and exposes an authenticated internal backup operation once implemented.

## Pod configuration

Use dedicated nonroot numeric users in both images. Set `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, dropped capabilities, and `seccompProfile: RuntimeDefault`. Use a read-only root filesystem, with explicit writable mounts for API data and any required Next.js runtime cache or temporary files. Set API `fsGroup` consistently with the image's group and test permission behavior with the chosen CSI driver. [Kubernetes security context documentation](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)

CPU/memory requests and limits remain to be measured; record initial values when manifests are implemented and refine them using load tests. Bound graceful termination to the API's request/provider timeout policy. Add startup, liveness, and readiness probes with the semantics in [deployment](deployment.md), rather than using an external provider as a liveness dependency.

## Network and secret boundaries

Allow these flows explicitly:

1. Ingress controller to frontend port `3000` and API port `8080`.
2. Frontend server to API port `8080` for server-side requests.
3. Workloads to cluster DNS; API to approved sandbox provider and backup endpoints when those features exist.
4. Monitoring components to private telemetry endpoints when monitoring is configured.

Basic NetworkPolicy does not express arbitrary HTTPS hostname allowlists; use a suitable egress gateway or CNI capability when domain restrictions are required. Keep probes and internal administrative endpoints out of public Ingress routing. Route only `/v1` publicly to the API; enforce authentication, workspace authorization, and request limits in the API independently of network policy. [NetworkPolicy capabilities](https://kubernetes.io/docs/concepts/services-networking/network-policies/)

Inject only server-required secrets into the API, grant operators narrowly scoped access, and redact logs. TLS Secret, application Secret, and ConfigMap are separate resources with separate purposes. Backups inherit the sensitivity of their source data.

## Lifecycle and failure handling

For initial startup, the sole API container applies versioned migrations before becoming ready or starting dispatch. A failed migration must fail startup without deleting the PVC. Use an explicit maintenance window for incompatible changes; never run concurrent migration processes against this SQLite database.

For ordinary replacement, become unready, stop claims, drain bounded work, close the database, and wait for the old process to exit before the replacement opens the volume. Do not force-delete a partitioned API pod and assume it has stopped. If the node cannot be contacted, fence the old node or volume owner using infrastructure controls before authorizing replacement. Record unresolved provider attempts for reconciliation. See [StatefulSet pod deletion safety](https://kubernetes.io/docs/tasks/run-application/force-delete-stateful-set-pod/).

A PodDisruptionBudget is not a second replica or storage failover mechanism. If one is later configured to block voluntary eviction, provide a maintenance procedure that deliberately permits downtime; otherwise node drains can remain blocked. For this sandbox, schedule API maintenance with an acknowledged outage rather than claiming zero-downtime rollout.

Before treating manifests as usable, validate service selectors, PVC binding, permissions, image digests, probe behavior, TLS routing, termination, and backup restoration on a test cluster. The diagram is a design reference and is not a `kubectl apply` artifact; the manifests that implement it are described below.

## Reference manifests (WBS 5.3)

`infrastructure/k8s/base` (Kustomize) holds the Namespace (`restricted` Pod Security label), ServiceAccounts (no token automount), ConfigMap, a placeholder Secret, the API StatefulSet with its two Services, the web Deployment and Service, the Ingress and the NetworkPolicies. `overlays/k3d-local` adds a `railvanta-retain` StorageClass for k3s local-path with `reclaimPolicy: Retain`; other clusters supply their own class of that name (or change `storageClassName`). `examples/tls-secret.example.yaml` is a placeholder; create the real `railvanta-tls` Secret with `kubectl create secret tls`, never from a committed file.

Choices worth knowing:

- **Probes use `exec`.** The API's config loader rejects any non-loopback `HEALTH_ADDR`, so kubelet `httpGet` cannot reach `127.0.0.1:8081`. Probes run busybox `wget` against `/health/startup`, `/health/live` and `/health/ready`. Web probes use `httpGet /`.
- **`terminationGracePeriodSeconds: 15`** equals the Compose `stop_grace_period`: the API's shutdown context is 10 seconds for HTTP, dispatcher drain and database close. Raise both together.
- **Access mode and reclaim.** The claim template requests `ReadWriteOncePod` (the k3s local-path provisioner honored it). Reclaim `Retain` is a property of the StorageClass/PV, not the PVC; the StatefulSet also sets `persistentVolumeClaimRetentionPolicy` to `Retain` for deletion and scale-down.
- **Images** default to `railvanta-api:local` and `railvanta-web:local`, the names `docker compose build` produces. Pin real digests at deploy time (never commit invented ones): `cd infrastructure/k8s/base && kustomize edit set image railvanta-api=REGISTRY/railvanta-api@sha256:DIGEST`, then `REQUIRE_DIGESTS=1 scripts/validate-k8s.sh`. The web image bakes `API_INTERNAL_ORIGIN=http://api:8080` into its Next.js rewrites at build time; the Ingress sends `/v1` straight to the API, so the rewrite is unused here, but rebuild with `--build-arg API_INTERNAL_ORIGIN=http://railvanta-api:8080` before relying on server-side calls from the web pod.
- **Ingress** has no `ingressClassName` (the default class applies) and the host `railvanta.test`; `PUBLIC_APP_ORIGIN` in the ConfigMap must match the public origin.
- **NetworkPolicies** allow controller namespaces `kube-system` (k3s traefik) and `ingress-nginx` to both services, web to API, web egress to API, and DNS egress. The API has no other egress because no provider or backup endpoint exists yet.
- **Not included:** ResourceQuota/LimitRange (budget numbers are not decided), PodDisruptionBudget, monitoring, and backup. No in-cluster backup path exists until the container backup command is merged; the drill is pending.
- **Sandbox credentials.** The container images do not seed demo credentials (demo mode needs a loopback bind). The smoke script seeds a throwaway database in a local container and copies it into the PVC while the API is scaled to zero. This is test tooling only.

Validation: `scripts/validate-k8s.sh` renders both Kustomize targets, runs `kubeconform -strict` and asserts the single-writer rules (one replica, no HPA, Job or CronJob, `ReadWriteOncePod`, dropped capabilities, read-only root filesystems, default-deny policy). `.github/workflows/k8s.yml` runs it with kubeconform `v0.7.0`. `scripts/k8s-smoke.sh` creates a k3d cluster, applies the overlay behind traefik with a self-signed certificate, exercises the Ingress over TLS, deletes the API pod, checks for duplicate postings and tears the cluster down. Results are in `CODECHANGELOG.md`. NetworkPolicy findings: ingress restrictions were enforced in k3s; egress restrictions were not observed to be enforced in the k3d-on-Docker-Desktop test, so use a CNI with verified egress support (for example Cilium or Calico) before relying on them.
