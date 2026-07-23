# OpenShell Deployment

NVIDIA OpenShell is an open-source runtime environment that securely executes autonomous AI agents using sandboxed environments and kernel-level isolation. It allows agents to access files, credentials, and external networks without the risk of system compromise or data exfiltration.

Astonish integrates OpenShell as a Helm subchart — a single `helm install` deploys the Astonish control plane and the OpenShell gateway together. Agent tool calls execute inside individually isolated sandboxes with granular policy enforcement and full audit trails.

## Prerequisites

### 1. Kubernetes Cluster

- Kubernetes 1.28+
- A RWX StorageClass (NFS, CephFS, EFS, Azure Files, or similar)
- Optional: Istio service mesh (simplifies TLS between control plane and gateway)

### 2. PostgreSQL

- PostgreSQL 15+ with the `pgvector` extension
- A database user with CREATE DATABASE permissions

### 3. Install the Agent Sandbox CRD

The Agent Sandbox CRD and controller must be installed cluster-wide before deploying Astonish with OpenShell:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/latest/download/manifest.yaml
```

Verify the CRD is installed:

```bash
kubectl get crd sandboxes.agents.x-k8s.io
```

### 4. Install Kyverno (Recommended for Production)

[Kyverno](https://kyverno.io) is a Kubernetes-native policy engine that Astonish uses to eliminate workspace PVC provisioning latency. Without it, every sandbox pod waits for a 2Gi PVC to provision before starting — adding **~35 seconds on Cinder/OpenStack** clusters and 5-15s on EBS gp2/gp3.

With Kyverno installed and `ephemeralWorkspace: true` set, a ClusterPolicy mutates sandbox pods at admission time to replace the PVC-backed workspace volume with a fast `emptyDir`. Sandbox startup drops to **3-5 seconds** regardless of StorageClass speed.

**When to install:**

- **Required** if you set `sandbox.openshell.ephemeralWorkspace: true` (recommended for production)
- **Not needed** if your cluster has fast block storage provisioning (local-path, NVMe-backed CSI < 2s) or if you need persistent workspace data across sandbox restarts (rare)

**Install Kyverno:**

```bash
# Add the Kyverno Helm repository
helm repo add kyverno https://kyverno.github.io/kyverno/
helm repo update

# Install Kyverno (v1.12+ required; v1.18+ recommended)
helm install kyverno kyverno/kyverno \
  --namespace kyverno \
  --create-namespace \
  --set admissionController.replicas=3 \
  --set backgroundController.replicas=2 \
  --wait
```

For high-availability production clusters, use 3 admission controller replicas. For dev/test, a single replica is fine:

```bash
# Minimal install for dev/test
helm install kyverno kyverno/kyverno \
  --namespace kyverno \
  --create-namespace \
  --wait
```

**Verify Kyverno is running:**

```bash
# All pods should be Running
kubectl -n kyverno get pods

# The admission webhook should be registered
kubectl get mutatingwebhookconfigurations | grep kyverno
```

Expected output:

```
kyverno-resource-mutating-webhook-cfg   ...   ...
```

**Important notes:**

- Kyverno is a **cluster-wide** tool. If it's already installed in your cluster (by another team or platform), skip this step — the Astonish ClusterPolicy will work with any existing Kyverno 1.12+ installation.
- Kyverno is **not** bundled as a subchart dependency. It must be installed separately before deploying Astonish with `ephemeralWorkspace: true`.
- The Astonish ClusterPolicy is **disposable** — it works around a limitation in OpenShell v0.0.63 (issues [#967](https://github.com/NVIDIA/OpenShell/issues/967), [#971](https://github.com/NVIDIA/OpenShell/issues/971)). Once upstream fixes workspace PVC injection, remove the policy by setting `ephemeralWorkspace: false`.

## Platform Initialization

### 1. Initialize the Database

Run from a machine with network access to your PostgreSQL instance:

```bash
./astonish platform init \
  --host <postgres-host> \
  --port 5432 \
  --password <postgres-admin-password>
```

This creates the platform database and prints the connection DSN. Save this DSN for the next steps.

### 2. Generate Secrets

Generate the master encryption key and JWT signing secret:

```bash
./astonish platform gen-secret   # → use as MASTER_KEY
./astonish platform gen-secret   # → use as JWT_SECRET
```

### 3. Create the Kubernetes Secret

Create the namespace and store the secrets:

```bash
kubectl create namespace astonish

kubectl create secret generic astonish-secrets \
  -n astonish \
  --from-literal=master-key="<master-key-from-step-2>" \
  --from-literal=jwt-secret="<jwt-secret-from-step-2>" \
  --from-literal=platform-dsn="<dsn-from-step-1>"
```

## Helm Values

Create a `values-openshell.yaml` file for your deployment. Below is a complete production-ready example.

### With Istio (Recommended)

When your cluster has Istio, the mesh handles encryption between the control plane and the OpenShell gateway. This is the recommended setup:

```yaml
# values-openshell.yaml (with Istio service mesh)

# ------------------------------------------------------------------
# Control-plane image
# ------------------------------------------------------------------
image:
  repository: ghcr.io/sap/astonish
  tag: latest
  pullPolicy: IfNotPresent

# ------------------------------------------------------------------
# Namespace layout
# "astonish" prefix yields:
#   control plane: astonish
#   sandbox:       astonish-sandbox
# ------------------------------------------------------------------
namespaces:
  prefix: astonish

# ------------------------------------------------------------------
# Secrets — reference the pre-created K8s Secret
# ------------------------------------------------------------------
secrets:
  existingSecret: "astonish-secrets"

# ------------------------------------------------------------------
# Platform database
# ------------------------------------------------------------------
config:
  storage:
    backend: postgres
    postgres:
      instanceSuffix: ""   # Optional: appended to database names

# ------------------------------------------------------------------
# Ingress
# ------------------------------------------------------------------
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: astonish.example.com
      paths:
        - path: /
          pathType: Prefix

# ------------------------------------------------------------------
# Service Mesh — Istio
# ------------------------------------------------------------------
mesh:
  provider: istio

# ------------------------------------------------------------------
# Sandbox — OpenShell backend
# ------------------------------------------------------------------
sandbox:
  enabled: true
  backend: openshell
  podSecurity: privileged  # OpenShell supervisor needs SYS_ADMIN, NET_ADMIN, SYS_PTRACE

  openshell:
    enabled: true
    # Eliminate workspace PVC latency via Kyverno pod mutation (requires Kyverno)
    ephemeralWorkspace: true
    # Gateway address — leave empty for auto-derived:
    #   "{release}-openshell.{namespace}.svc.cluster.local:8080"
    gateway:
      addr: ""
    # Sandbox container image (Astonish agent tooling + OpenShell supervisor)
    image:
      repository: ghcr.io/sap/astonish-sandbox-openshell
      tag: latest
    # Network egress policy for sandboxes
    # Available presets: "default", "code_hosting", "package_registries",
    #   "llm_apis", "tools", "system", "search", "cdn"
    # "default" enables all presets.
    networkPolicy:
      presets:
        - "default"
      # Add internal services your agents need to reach:
      extraEndpoints: []
      # extraEndpoints:
      #   - host: "internal-api.company.com"
      #     port: 443

  # Storage — required by chart validation (PVCs are created but unused by OpenShell)
  storage:
    storageClassName: "<your-rwx-storage-class>"  # e.g., nfs-client, efs-sc, cephfs

  # Seed job — disabled for OpenShell (no overlay layer to seed)
  seed:
    enabled: false

  # Resource limits per sandbox session
  limits:
    cpu: 2
    memory: "2Gi"
    processes: 500
  requests:
    cpuMillis: 100
    memoryMiB: 256

# ------------------------------------------------------------------
# OpenShell subchart values (forwarded to NVIDIA OpenShell Helm chart)
# ------------------------------------------------------------------
openshell:
  nameOverride: openshell

  server:
    # Istio handles encryption — gateway listens plaintext
    disableTls: true
    # Must match the computed sandbox namespace
    sandboxNamespace: "astonish-sandbox"
    # Must match sandbox.openshell.image above
    sandboxImage: "ghcr.io/sap/astonish-sandbox-openshell:latest"
    auth:
      # Istio AuthorizationPolicy handles identity
      allowUnauthenticatedUsers: true
    tls:
      clientCaSecretName: ""

  # PKI init job — MUST remain enabled (generates JWT signing keypair
  # for sandbox token issuance)
  pkiInitJob:
    enabled: true

  # Astonish manages its own NetworkPolicies
  networkPolicy:
    enabled: false
```

### Without Istio

If you don't have a service mesh, the OpenShell gateway handles mTLS itself:

```yaml
# Differences from the Istio version:

# No mesh provider
mesh:
  provider: ""

# OpenShell subchart — gateway handles its own TLS
openshell:
  nameOverride: openshell

  server:
    # Gateway terminates mTLS itself
    disableTls: false
    sandboxNamespace: "astonish-sandbox"
    sandboxImage: "ghcr.io/sap/astonish-sandbox-openshell:latest"
    auth:
      # Without mesh identity, consider restricting access
      allowUnauthenticatedUsers: false
    tls:
      # PKI init job auto-generates certificates
      clientCaSecretName: ""

  pkiInitJob:
    enabled: true

  networkPolicy:
    enabled: false
```

All other values remain the same as the Istio version.

## Install

Deploy with Helm:

```bash
helm upgrade --install astonish deploy/helm/astonish \
  -n astonish --create-namespace \
  -f values-openshell.yaml
```

Wait for all pods to become ready:

```bash
kubectl -n astonish rollout status deployment/astonish-api
kubectl -n astonish rollout status deployment/astonish-worker
```

## Verify

Check that all components are running:

```bash
# Control-plane pods (api, worker, openshell gateway)
kubectl -n astonish get pods

# Sandbox namespace exists (empty until first agent session)
kubectl get ns astonish-sandbox

# Agent Sandbox CRD is installed
kubectl get crd sandboxes.agents.x-k8s.io

# OpenShell gateway logs (should show "listening on :8080")
kubectl -n astonish logs -l app.kubernetes.io/name=openshell --tail=20
```

## Access Studio

### Via Ingress

If you configured ingress, open `https://astonish.example.com` in your browser.

### Via Port-Forward

For local access without ingress:

```bash
kubectl -n astonish port-forward svc/astonish-api 9393:9393 &
open http://localhost:9393
```

The first user to register will become the organization owner with full admin access. A default organization and team are created automatically on first signup.

## Notes

### TLS and Service Mesh

| Setup | `mesh.provider` | `openshell.server.disableTls` | How encryption works |
|-------|-----------------|-------------------------------|---------------------|
| With Istio | `"istio"` | `true` | Mesh mTLS encrypts all traffic |
| Without mesh | `""` | `false` | Gateway terminates mTLS (auto-generated certs) |
| Dev/testing only | `""` | `true` | Plaintext (not for production) |

### Pod Security

OpenShell supervisor containers require elevated privileges (`SYS_ADMIN`, `NET_ADMIN`, `SYS_PTRACE`) for kernel-level isolation. Set `sandbox.podSecurity: privileged` to configure the sandbox namespace's Pod Security Admission accordingly.

### Storage Class

The chart requires a `storageClassName` for validation even though OpenShell does not use the overlay PVC system. The PVCs are created but remain unused. Set this to any available RWX StorageClass in your cluster.

Note: The workspace PVC that OpenShell creates per sandbox is separate from this storage class — it uses the cluster's default StorageClass. To eliminate workspace PVC latency, see [Workspace Storage (Ephemeral Mode)](#workspace-storage-ephemeral-mode) below.

### Network Policy Presets

By default, sandboxes can reach common external services (code hosting, package registries, LLM APIs, CDNs). The OpenShell supervisor enforces these rules at the network namespace level — the Kubernetes NetworkPolicy is permissive, and fine-grained enforcement happens inside the sandbox.

Available presets: `default` (all below), `code_hosting`, `package_registries`, `llm_apis`, `tools`, `system`, `search`, `cdn`.

Add internal services with `extraEndpoints`:

```yaml
sandbox:
  openshell:
    networkPolicy:
      presets:
        - "default"
      extraEndpoints:
        - host: "internal-api.company.com"
          port: 443
        - host: "*.internal.company.com"
          port: 443
```

For dynamic per-team/org/platform network rules managed through the Studio UI (rather than static config), see [Network Policy](../security/network-policy.md).

### Landlock Filesystem Policy

The sandbox filesystem is controlled by Linux Landlock LSM. Astonish
configures the policy to allow:

- **Read-only:** `/usr`, `/bin`, `/sbin`, `/lib`, `/lib64`, `/etc`, `/opt`, `/dev/null`, `/dev/urandom`
- **Read-write:** `/sandbox`, `/tmp`, `/var/tmp`, `/home`, `/run`, `/dev/pts`

The `/dev/pts` path is required for PTY allocation (interactive shell
support). On kernel 6.10+, Landlock ABI v5 restricts `ioctl` on device
files — without this path explicitly listed, the `shell_command` tool's
PTY allocation would fail with `Permission denied`.

> **Note:** `/dev/ptmx` is typically a symlink and cannot be listed directly
> (the supervisor refuses to chown symlinks). PTY allocation works through
> `/dev/pts/ptmx` which is accessible via the `/dev/pts` directory entry.

**Extending the filesystem policy:**

```yaml
sandbox:
  openshell:
    filesystemPolicy:
      extraReadOnly:
        - /data/shared-models
      extraReadWrite:
        - /mnt/scratch
```

### Corporate CA / Trust Bundles

Requires the OpenShell gateway **≥ 0.0.81** (Astonish chart pins **0.0.86**)
for the PVC path. ConfigMap mode does not send PVC mounts through
`driver_config` (OpenShell’s schema is PVC-only) and instead relies on
Kyverno pod mutation — same idea as `ephemeralWorkspace`.

OpenShell’s egress proxy **MITMs** HTTPS (`CONNECT`), then opens a second
TLS session to the real upstream. Upstream verification uses Mozilla roots
plus the container system store at `/etc/ssl/certs/ca-certificates.crt` —
not the agent’s `SSL_CERT_FILE` (the supervisor overwrites that to
`/etc/openshell-tls/ca-bundle.pem`). Mounting a corp CA only under
`/etc/astonish-ca/...` is therefore **not enough** for internal HTTPS.

To trust corporate endpoints without rebuilding the sandbox image, mount a
**combined** PEM (system CAs + corporate roots) via `certBundles`. By
default (`installSystemTrust: true`) that file is dual-mounted at the
operator path **and** over `/etc/ssl/certs/ca-certificates.crt` so the
supervisor loads corp roots before PID 1 starts. At most one entry may
install into the system store.

#### ConfigMap + Kyverno (recommended on Cinder/EBS)

Prefer this when sandboxes schedule across nodes and the StorageClass is
block storage (Cinder, EBS, etc.). A shared PVC cannot multi-attach and
will fail with `FailedAttachVolume` at scale / during Helm upgrades.

**Requires Kyverno** in the cluster (same prerequisite as
`ephemeralWorkspace`).

```yaml
sandbox:
  openshell:
    # Default for chart-managed cert PVCs (every sandbox mounts the same claim).
    certBundleDefaults:
      accessMode: ReadWriteMany
    certBundles:
      - name: corp-root-ca
        source: configMap
        configMapName: astonish-corp-ca
        mountPath: /etc/astonish-ca/ca-bundle.crt
        subPath: ca-bundle.crt
        # installSystemTrust: true  # default — required for MITM upstream
        bootstrap:
          enabled: true
          url: "https://pki.example.com/corp-root-ca.crt"
          # accessMode: ReadWriteMany  # optional per-bundle override
```

Helm creates the ConfigMap; a post-install/upgrade Job downloads the org
CA, builds the combined PEM, and patches the ConfigMap. Kyverno injects
ConfigMap volume mounts into every sandbox pod in the sandbox namespace
and strips leftover cert PVC mounts during migration.

**Operational note:** the cert-bundle ClusterPolicy uses
`failurePolicy: Fail` (same as the `ephemeralWorkspace` emptyDir policy).
If Kyverno is unhealthy or cannot evaluate the policy, sandbox pod
creation in the sandbox namespace is rejected until Kyverno recovers.

#### PVC + RWX (OpenShell-native)

Use only when the StorageClass truly supports **ReadWriteMany**
(Manila, NFS, CephFS, EFS) — not Cinder/EBS. OpenShell mounts the claim
via `driver_config`. Legacy values with `claimName` and no
`configMapName` still select `source: pvc`.

```yaml
sandbox:
  openshell:
    certBundleDefaults:
      accessMode: ReadWriteMany
    certBundles:
      - name: corp-root-ca
        source: pvc
        claimName: astonish-corp-ca
        mountPath: /etc/astonish-ca/ca-bundle.crt
        subPath: ca-bundle.crt
        bootstrap:
          enabled: true
          url: "https://pki.example.com/corp-root-ca.crt"
```

Or pre-provision the PVC/ConfigMap yourself and omit `bootstrap`.

Astonish also sets trust env vars (`SSL_CERT_FILE`, etc.) to the operator
mount path for tools that honor them before OpenShell rewrites env, and
adds mount paths to the Landlock read-only set. Browser sessions still
inject the OpenShell MITM CA into the NSS DB via the browser launch script.

Mount paths must not be under `/sandbox` (workspace) or
`/etc/openshell` / `/etc/openshell-tls` (OpenShell-managed). Use a
*combined* bundle — replacing the system store with a corp-only PEM would
break public HTTPS.

#### Migrate PVC → ConfigMap (fix Multi-Attach on Garden/Cinder)

1. Switch values to `source: configMap` + `configMapName` (keep the old
   `claimName` in values if you want Kyverno to strip that claim from
   leftover pods).
2. Ensure Kyverno is installed.
3. `helm upgrade` — chart creates/preserves the ConfigMap; bootstrap Job
   patches the combined PEM; Kyverno policy is installed.
4. Delete leftover sandbox pods that still mount the old cert PVC (or wait
   for idle eviction).
5. Delete the old cert PVC once nothing mounts it.

Do **not** rely on Cinder “RWX” — kubelet may still fail Multi-Attach.

#### PVC multi-attach / upgrades (when staying on `source: pvc`)

Chart-managed claims default to **ReadWriteMany**. The StorageClass must
support multi-attach (Manila, NFS, CephFS, EFS) — not Cinder/EBS RWO. If you
must use RWO, set `bootstrap.accessMode: ReadWriteOnce` (or
`certBundleDefaults.accessMode`) and pin sandboxes to a single node.

Bound PVC `accessModes` are immutable. The chart uses Helm `lookup` to keep
an existing claim’s access mode so upgrades do not fail with
`Forbidden: spec is immutable` when the desired default is RWX but the live
PVC is still RWO. Fresh installs still get RWX.

**Migrate RWO → RWX:** drain/delete sandbox pods that mount the claim,
delete the PVC (re-bootstrapable from `bootstrap.url`), keep
`accessMode: ReadWriteMany`, then `helm upgrade` so the chart recreates RWX
and the bootstrap Job repopulates. Or use a new `claimName` and retire the
old PVC after sandboxes recycle.

Recycle existing sandboxes after enabling or changing `certBundles`
(idle eviction or new chat) so pods pick up the new mounts.

**Landlock enforcement mode:**

```yaml
sandbox:
  openshell:
    # "best_effort" (default) — degrades gracefully on kernels without Landlock
    # "hard_requirement" — fails sandbox startup if Landlock can't be enforced
    landlockCompatibility: "best_effort"
```

Use `hard_requirement` when debugging Landlock issues — it provides fast,
clear failure instead of silent degradation.

:::note Supervisor Version
OpenShell supervisor ≥ 0.0.70 is recommended for reliable PTY device
handling on kernel 6.10+. Earlier versions may have issues with device
path pre-opening during `prepare_filesystem()` (see OpenShell Issue #749).
Upgrade by bumping the subchart version in `Chart.yaml` and running
`helm dependency update`.
:::

### Namespace Convention

The `namespaces.prefix` value drives all namespace names:

| Setting | Result |
|---------|--------|
| `prefix: astonish` | Control plane: `astonish`, Sandboxes: `astonish-sandbox` |
| `prefix: astonish-prod` | Control plane: `astonish-prod`, Sandboxes: `astonish-prod-sandbox` |

The `openshell.server.sandboxNamespace` must match the computed sandbox namespace.

### Workspace Storage (Ephemeral Mode)

By default, the OpenShell gateway injects a 2Gi `ReadWriteOnce` PersistentVolumeClaim into every sandbox pod for the `/sandbox` workspace directory. On clusters with slow block storage provisioning (Cinder/OpenStack, EBS gp2), this PVC provisioning dominates sandbox startup time — **adding 15-35+ seconds** before the pod can start.

**How ephemeral mode works:**

When `sandbox.openshell.ephemeralWorkspace: true` is set and Kyverno is installed, the Astonish Helm chart deploys a `ClusterPolicy` that:

1. Intercepts sandbox Pod creation at admission time (after the Sandbox controller assembles the pod spec)
2. Finds the `workspace` volume (which references a PVC)
3. Replaces it with `emptyDir: { sizeLimit: "2Gi" }`

The pod starts immediately — no PVC provisioning wait. The `/sandbox` directory is backed by the node's filesystem (tmpfs or disk, depending on kubelet configuration).

**Trade-offs:**

| Aspect | With PVC (default) | With emptyDir (ephemeral) |
|--------|-------------------|--------------------------|
| Startup latency | 15-35s (Cinder) | 3-5s |
| Data persistence | Survives pod restart | Lost on pod restart |
| Storage accounting | Per-PVC billing | Part of node ephemeral storage |
| Cleanup | PVC deleted with Sandbox | Automatic (pod termination) |
| Capacity | Fixed 2Gi PVC | Shared node ephemeral storage (2Gi soft limit) |

For most use cases, ephemeral mode is preferred: chat sessions are short-lived, sandboxes are destroyed after idle timeout, and workspace data does not need to survive restarts.

**Enable ephemeral mode:**

```yaml
sandbox:
  openshell:
    ephemeralWorkspace: true  # Requires Kyverno (see Prerequisites §4)
```

**Verify the policy is active:**

```bash
# The ClusterPolicy should exist and report "Ready"
kubectl get clusterpolicy -l app.kubernetes.io/component=openshell-workspace

# Expected output:
# NAME                        ADMISSION   BACKGROUND   ...   MESSAGE
# astonish-sandbox-emptydir   true        true         ...   Ready
```

**Verify a sandbox pod uses emptyDir:**

```bash
# After creating a sandbox (or starting a chat), inspect the pod:
kubectl -n <sandbox-namespace> get pods
kubectl -n <sandbox-namespace> get pod <sandbox-pod> -o jsonpath='{.spec.volumes[?(@.name=="workspace")]}'

# Expected: {"emptyDir":{"sizeLimit":"2Gi"},"name":"workspace"}
# NOT:      {"name":"workspace","persistentVolumeClaim":{"claimName":"..."}}
```

**Troubleshooting:**

| Symptom | Cause | Fix |
|---------|-------|-----|
| Sandbox pods stuck `Pending` with volume error | Kyverno not installed or policy not deployed | Install Kyverno, run `helm upgrade` |
| `ClusterPolicy` shows `Ready: false` | Kyverno webhook not registered | Check `kubectl -n kyverno get pods`, restart if needed |
| Pod has PVC volume despite `ephemeralWorkspace: true` | Policy not matching (wrong namespace) | Verify `openshell.server.sandboxNamespace` matches `namespaces.prefix` + `-sandbox` |
| Kyverno logs show "failed to apply rule" | Kyverno version too old (< 1.12) | Upgrade Kyverno to 1.12+ |
| Pod starts but `/sandbox` writes fail with ENOSPC | Node ephemeral storage full | Increase node disk or reduce `sizeLimit` |

**Disabling ephemeral mode:**

To revert to PVC-backed workspace storage (e.g., if you need persistent workspace data):

```yaml
sandbox:
  openshell:
    ephemeralWorkspace: false  # Default — Kyverno policy not deployed
```

Run `helm upgrade` — the ClusterPolicy will be removed and new sandboxes will use PVC storage.

**Note on orphaned PVCs:** When ephemeral mode is active, the Sandbox controller still creates a PVC (from `volumeClaimTemplates` in the Sandbox CRD). This PVC remains `Pending` (never bound, since no pod references it) and is automatically cascade-deleted when the Sandbox is destroyed via its `ownerReference`. No manual cleanup is required.

## See Also

- [Sandboxes](../security/sandboxes.md) — security model and isolation architecture
- [Kubernetes Deployment](./kubernetes.md) — standard K8s deployment (without OpenShell)
- [Deployment Overview](./index.md) — choosing between deployment models
