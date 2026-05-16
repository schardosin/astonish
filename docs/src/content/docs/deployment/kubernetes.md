---
title: Kubernetes Deployment
description: Deploy Astonish on Kubernetes with the official Helm chart
---

This guide walks through deploying Astonish on Kubernetes using the Helm chart at `deploy/helm/astonish/`. By the end you will have a running control plane (API + worker), a sandbox subsystem capable of launching isolated AI agent sessions, and the tooling to verify it all works.

## What gets deployed

The Helm chart creates the following resources in a single `helm install`:

| Resource | Namespace | Why it exists |
|----------|-----------|---------------|
| **Namespace** `astonish` | cluster-scoped | Houses the control plane (API servers, worker, config). Isolated from sandbox workloads. |
| **Namespace** `astonish-sandbox` | cluster-scoped | Houses sandbox pods, PVCs, and RBAC. Separate namespace enables distinct Pod Security Admission (PSA) policies. |
| **Deployment** `astonish-api` | `astonish` | Stateless HTTP/SSE API servers. Horizontally scalable. |
| **Deployment** `astonish-worker` | `astonish` | Background processor: scheduler, channels, fleet monitors. Single replica. |
| **ConfigMap** `astonish-config` | `astonish` | Rendered `config.yaml` — all runtime settings including sandbox backend configuration. Derived from chart values (single source of truth). |
| **Secret** `astonish-secrets` | `astonish` | Master key (AES-256 at-rest encryption), JWT signing key, Postgres DSN. |
| **ServiceAccount** | `astonish` | Identity for api/worker pods. Bound to a Role in the sandbox namespace so the control plane can create/exec/delete sandbox pods. |
| **Role + RoleBinding** | `astonish-sandbox` | Grants the control-plane SA permission to manage pods, PVCs, and exec in the sandbox namespace. Cross-namespace binding (SA in `astonish`, permissions in `astonish-sandbox`). |
| **PVC** `astonish-layers` | `astonish-sandbox` | RWX volume storing content-addressed layers (`@base/rootfs` + template layers). Mounted read-only into every sandbox pod. |
| **PVC** `astonish-uppers` | `astonish-sandbox` | RWX volume storing per-session upper layers. Written during sessions, read back on resume. |
| **Job** `astonish-sandbox-seed` | `astonish-sandbox` | Helm hook (`post-install`, `post-upgrade`). Seeds `@base/rootfs` on the layers PVC by tar-copying the sandbox base image's rootfs. Idempotent — short-circuits if content exists. |
| **DaemonSet** (optional) | `kube-system` | FUSE device plugin. Only deployed when `fuseDevicePlugin.enabled=true`. Advertises `/dev/fuse` as a schedulable resource so sandbox pods can use fuse-overlayfs without privileged mode. |
| **Service** `astonish-api` | `astonish` | ClusterIP exposing the API on port 9393. |
| **Ingress** (optional) | `astonish` | External access to Studio/API. Nginx sticky-session annotations for SSE affinity. |

## Prerequisites

- **Kubernetes cluster** (1.28+). Supported: self-managed (Gardener, Rancher, kubeadm, K3s, k0s), EKS, AKS, GKE with a controllable node pool.
  - **Not supported:** GKE Autopilot, EKS Fargate, AKS virtual nodes — these forbid DaemonSets and hostPath access needed by several overlay paths.
- **ReadWriteMany (RWX) StorageClass**. Examples: CephFS, NFS (nfs-subdir-external-provisioner), EFS, Azure Files, Manila (SAP Converged Cloud).
- **`kubectl`** 1.30+ and **`helm`** 3.8+ with cluster-admin access (the chart creates namespaces and cross-namespace RBAC).
- **Container images** accessible from the cluster:
  - `schardosin/astonish:<tag>` — control plane.
  - `schardosin/astonish-sandbox-base:<tag>` — sandbox pod base image.
- **PostgreSQL** — the platform database. Can be in-cluster or external. You need a connection DSN.

## Step 1: choose an overlay strategy

The sandbox backend composes an overlay filesystem inside each sandbox pod. Four strategies are available depending on your cluster's capabilities:

| Path | Best for | PSA profile | Overlay settings | Extras needed |
|------|----------|-------------|-----------------|---------------|
| **1. Device plugin** | Production clusters (containerd + runc) | `baseline` | `mode: fuse`, `fuseDeviceResource: smarter-devices/fuse` | FUSE device plugin DaemonSet (chart ships one) |
| **2. User namespace** | K8s 1.33+ with UserNamespacesSupport | `baseline` | `mode: kernel`, `hostUsers: false` | Nothing |
| **3. Privileged** | Dev/lab, nested LXC, "works in 5 minutes" | `privileged` | `mode: fuse`, `privileged: true` | Nothing |
| **4. Sysbox** | Clusters already running Sysbox | `baseline` | `mode: kernel`, `runtimeClassName: sysbox-runc` | Sysbox installed |

**Recommendation:** Use **Path 1** for production and **Path 3** for development.

### How the overlay works

Every sandbox pod mounts two shared PVCs:
- **layers** (`astonish-layers`) — read-only, content-addressed layer store. Contains `@base/rootfs` (seeded by the Helm hook Job) and template-specific layers added later.
- **uppers** (`astonish-uppers`) — read-write, per-session scratch space.

The pod's entrypoint script composes these into a union filesystem using either `fuse-overlayfs` (Paths 1 and 3) or kernel `mount -t overlay` (Paths 2 and 4), then `chroot`s into the result. The AI agent's tool calls execute inside that chroot.

## Step 2: build a values file

Start from the dev example and adjust for your environment:

```bash
cp deploy/helm/astonish/values-dev-proxmox.yaml deploy/helm/astonish/values-myenv.yaml
```

### Overlay settings per path

```yaml
# Path 1 — FUSE device plugin (production)
sandbox:
  podSecurity: baseline
  overlay:
    mode: fuse
    privileged: false
    fuseDeviceResource: smarter-devices/fuse
  storage:
    storageClassName: ceph-filesystem   # your RWX class
fuseDevicePlugin:
  enabled: true

# Path 2 — user namespaces (K8s 1.33+)
sandbox:
  podSecurity: baseline
  overlay:
    mode: kernel
    hostUsers: false
  storage:
    storageClassName: <your-rwx-class>

# Path 3 — privileged (dev/lab)
sandbox:
  podSecurity: privileged
  overlay:
    mode: fuse
    privileged: true
  storage:
    storageClassName: nfs-client

# Path 4 — Sysbox
sandbox:
  podSecurity: baseline
  overlay:
    mode: kernel
    runtimeClassName: sysbox-runc
  storage:
    storageClassName: <your-rwx-class>
```

### Required secrets

The chart requires three secret values. Pods will CrashLoopBackOff without them.

| Value | Env var | Purpose |
|-------|---------|---------|
| `masterKey` | `ASTONISH_MASTER_KEY` | AES-256 key-encryption-key (KEK). Encrypts all credentials and secrets stored in the platform database. **64 hex characters (32 bytes).** |
| `jwtSecret` | `ASTONISH_JWT_SECRET` | Signing key for access and refresh tokens. **64 hex characters (32 bytes).** |
| `platformDSN` | `ASTONISH_PLATFORM_DSN` | PostgreSQL connection string for the platform database (output of `platform init`). |

#### Generating masterKey and jwtSecret

Each value must be exactly 64 hex characters (32 random bytes). Use any of these equivalent methods:

```bash
# Option A: openssl (universally available)
openssl rand -hex 32

# Option B: astonish CLI helper (no openssl dependency; works in kubectl run)
astonish platform gen-secret

# Option C: Python one-liner
python3 -c 'import secrets; print(secrets.token_hex(32))'
```

Generate **two separate values** — one for `masterKey`, one for `jwtSecret`. Never reuse the same value for both.

#### Providing secrets to the chart

**Method 1 — In your values file** (simpler, fine for dev/PoC):

```yaml
secrets:
  masterKey: "a3f1...64 hex chars..."
  jwtSecret: "b7c2...64 hex chars..."
  platformDSN: "postgres://postgres:yourpass@pg-host:5432/astonish_a1b2c3_platform?sslmode=prefer"
```

For git-tracked values files, encrypt with [SOPS](https://github.com/getsops/sops), [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets), or your secret-management workflow of choice.

**Method 2 — Pre-create a Kubernetes Secret** (recommended for production):

```bash
kubectl create namespace astonish   # if not yet created

kubectl -n astonish create secret generic astonish-secrets \
  --from-literal=master-key="$(openssl rand -hex 32)" \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=platform-dsn="postgres://postgres:yourpass@pg-host:5432/astonish_a1b2c3_platform?sslmode=prefer"
```

Then in your values file, reference the existing secret and leave the inline fields empty:

```yaml
secrets:
  existingSecret: "astonish-secrets"
  masterKey: ""
  jwtSecret: ""
  platformDSN: ""
```

The chart skips creating its own Secret resource and the deployments reference your pre-created one. The Secret must contain keys named `master-key`, `jwt-secret`, and `platform-dsn`.

:::caution[masterKey is permanent — do not rotate without re-encryption]
The `masterKey` encrypts all stored credentials (API keys, OAuth tokens, provider secrets) in the platform database. **There is no automatic re-encryption command today.** If you lose or rotate this key without first decrypting and re-encrypting all stored secrets, those credentials become permanently unrecoverable.

Treat `masterKey` as permanent infrastructure — back it up securely and never change it unless you are prepared to re-add all stored credentials.
:::

#### Rotation guidance

| Secret | Rotation impact | Procedure |
|--------|----------------|-----------|
| `jwtSecret` | All active sessions are invalidated; users must re-login. | Generate new value → update Secret → restart pods. Safe to rotate periodically. |
| `masterKey` | **All encrypted credentials become unreadable.** | Do **not** rotate. If compromised: decrypt all credentials with old key, rotate key, re-encrypt. No automated tool exists yet. |
| `platformDSN` | Pods reconnect to PG on restart. | Update the DSN value → restart pods. No data loss. |

### Security note

The example `values-dev-proxmox.yaml` ships with placeholder secrets. **Never deploy those to a real environment.** Generate fresh keys with `openssl rand -hex 32` (or `astonish platform gen-secret`) for each installation.

## Step 3: initialize the platform database

Before installing the Helm chart, the platform database must be initialized. The `astonish platform init` command connects to your PostgreSQL server, generates a unique instance suffix, creates the database (`astonish_<suffix>_platform`), sets up required PostgreSQL roles (`astonish_platform_admin`, `astonish_app`), and runs all platform-level schema migrations.

The command prints the exact values to add to your Helm values file — no manual suffix or DSN construction needed.

**Required PostgreSQL privileges:** The user must have `CREATEDB` and `CREATEROLE` privileges on the target PostgreSQL server (typically a superuser or a role granted these). After initialization succeeds, running pods only need standard DML access — see the privilege downgrade note below.

### Option A: run from your workstation

Use this when your workstation (or CI runner) has network access to the PostgreSQL server.

```bash
# Install or build the astonish binary (if not already available):
go install github.com/schardosin/astonish@latest
# or: make build

# Run platform init:
astonish platform init --host pg-host --user postgres --password 'yourpass'
```

**Note:** The hostname must resolve from where you run the command. If PostgreSQL is only reachable from inside the cluster (e.g., a ClusterIP service), use Option B instead.

### Option B: run inside the cluster

Use this when PostgreSQL is only reachable from within the cluster network (e.g., an in-cluster StatefulSet or a VPC-internal endpoint).

```bash
kubectl run astonish-platform-init --rm -it --restart=Never \
  -n astonish --image=schardosin/astonish:dev \
  --command -- astonish platform init \
  --host pg-host --user postgres --password 'yourpass'
```

The pod runs `platform init`, prints the result, and is deleted automatically (`--rm`).

**If the namespace doesn't exist yet**, create it first (`kubectl create namespace astonish`) or run the pod in the `default` namespace.

### Optional flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `5432` | PostgreSQL port |
| `--sslmode` | `prefer` | SSL mode (`disable`, `prefer`, `require`) |
| `--suffix` | auto-generated | Fixed instance suffix (e.g., `prod1`) instead of random |

All flags also accept standard PG environment variables: `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`.

### Expected output

```
=== Astonish Platform Init ===

Connecting to PostgreSQL at pg-host:5432... OK
Generating instance suffix... a1b2c3
Creating database astonish_a1b2c3_platform... OK
Verifying platform store... OK

Platform initialized successfully.

  Database: astonish_a1b2c3_platform
  Server:   pg-host:5432
  Suffix:   a1b2c3

Add to your Helm values file:

  secrets:
    platformDSN: "postgres://postgres:yourpass@pg-host:5432/astonish_a1b2c3_platform?sslmode=prefer"
  config:
    storage:
      postgres:
        instanceSuffix: "a1b2c3"
```

Copy the `secrets.platformDSN` and `config.storage.postgres.instanceSuffix` values directly into your `values-myenv.yaml`, then proceed to Step 4.

### Privilege downgrade (optional, recommended for production)

After `platform init` succeeds, the running api/worker pods do not need `CREATEDB` or `CREATEROLE` — they only perform DML on existing tables. For least-privilege:

1. Create a restricted PostgreSQL role (e.g., `astonish_runtime`) with `LOGIN` and `CONNECT` on the platform database, but no `CREATEDB`.
2. Grant it the same table-level privileges that `platform init` granted to `astonish_app`.
3. Update `secrets.platformDSN` in your values file to use the restricted role's credentials.
4. Keep the elevated-privilege credentials in a secure location for future `platform init` runs (upgrades with new migrations).

This is optional — many teams keep a single set of credentials for simplicity. The trade-off is that compromised pods could theoretically create databases (but not drop them without explicit `DROP` grants).

## Step 4: install

This section explains every command and why it's needed.

### 4.1 Preview the rendered manifests

```bash
helm template astonish deploy/helm/astonish \
  -n astonish \
  -f deploy/helm/astonish/values-myenv.yaml
```

**Why:** Renders the chart locally without contacting the cluster. Catches all 5 fail-hard validations (namespace DNS-1123, Release.Namespace match, storageClassName required, valid PSA profile, valid overlay mode) before you commit to a real install. If `helm template` succeeds, the install will not fail at template time.

**What to check:** Scan the output for your expected namespace names, image tags, PVC sizes, and PSA labels. Pipe to `less` or redirect to a file for inspection.

### 4.2 Install the chart

```bash
helm install astonish deploy/helm/astonish \
  -n astonish --create-namespace \
  -f deploy/helm/astonish/values-myenv.yaml
```

**Why each flag:**

| Flag | Purpose |
|------|---------|
| `astonish` | Release name. Drives resource naming (`astonish-api`, `astonish-worker`, etc.). |
| `deploy/helm/astonish` | Path to the chart on disk. |
| `-n astonish` | Tells Helm which namespace owns the release. **Must match** `namespaces.controlPlane` (or the derived value from `namespaces.prefix`). The chart validates this and fails with a clear error if they diverge. |
| `--create-namespace` | Creates the `astonish` namespace if it doesn't exist. Safe if it already exists. The sandbox namespace (`astonish-sandbox`) is created by a Namespace resource inside the chart — no extra flag needed. |
| `-f values-myenv.yaml` | Your overrides. Chart defaults fill everything else. |

**What happens immediately:**
1. Helm stores the release manifest as `secret/sh.helm.release.v1.astonish.v1` in the `astonish` namespace.
2. All non-hook resources are applied: namespaces, RBAC, PVCs, configmap, secrets, deployments, service.
3. The `post-install` hook fires: the seed Job is created in `astonish-sandbox`.

### 4.3 Wait for the seed Job

```bash
kubectl -n astonish-sandbox wait job/astonish-sandbox-seed \
  --for=condition=complete --timeout=300s
```

**Why:** The seed Job populates `astonish-layers/@base/rootfs` by tar-copying the sandbox base image's filesystem onto the shared PVC. Every sandbox pod mounts this path as its lower layer. If you skip this wait and immediately try to create a sandbox session, the pod will fail with a mount error because `@base/rootfs` doesn't exist yet.

**What the Job does internally:**
1. Checks if `@base/rootfs` already has content (idempotent — safe to re-run).
2. If empty, runs `tar -cf - / | tar -xf - -C /mnt/astonish-layers/@base/rootfs` (excluding `/proc`, `/sys`, `/dev`, `/mnt`, `/sandbox`).
3. Exits 0.

**How long it takes:** 30-90 seconds on fast NFS; up to 3 minutes on slow CephFS or cold image pulls.

**Monitor progress while waiting:**

```bash
kubectl -n astonish-sandbox logs job/astonish-sandbox-seed -f
```

Expected output on success:
```
astonish-seed: seeding base layer at /mnt/astonish-layers/@base/rootfs
astonish-seed: done
```

**If it fails:** Common causes are `ImagePullBackOff` (image not published or tag mismatch), PVC not Bound (StorageClass misconfigured), or NFS export not reachable. Check `kubectl -n astonish-sandbox describe job/astonish-sandbox-seed` and pod events.

### 4.4 Verify control-plane pods

```bash
kubectl -n astonish rollout status deploy/astonish-api deploy/astonish-worker
```

**Why:** Confirms api and worker pods are Running and Ready. If `secrets.platformDSN` points at an unreachable Postgres or `secrets.masterKey` is missing/malformed, pods will CrashLoopBackOff. This command blocks until healthy or times out.

**If pods crash:**

```bash
kubectl -n astonish logs deploy/astonish-api --tail=30
kubectl -n astonish logs deploy/astonish-worker --tail=30
```

The logs will show the exact error (connection refused, invalid DSN format, missing key, etc.). If you see "relation does not exist" or "database does not exist", you likely skipped Step 3 (`astonish platform init`).

### 4.5 Inventory the sandbox namespace

```bash
kubectl get all,pvc,sa,role,rolebinding -n astonish-sandbox
```

**Why:** Confirms the sandbox subsystem is fully provisioned. After a successful install you should see:

| Resource | Expected state |
|----------|----------------|
| `pvc/astonish-layers` | Bound |
| `pvc/astonish-uppers` | Bound |
| `role/astonish-sandbox-manager` | Present |
| `rolebinding/astonish-sandbox-manager` | Present |

The seed Job itself will be gone (deleted by `hook-succeeded` policy after completion).

### 4.6 (Path 1 only) Verify the FUSE device plugin

```bash
# Label the nodes that should host sandbox pods:
kubectl label node <node-name> smarter-device-manager=enabled

# Verify the resource is advertised:
kubectl get node <node-name> -o jsonpath='{.status.allocatable}' | grep -o 'smarter-devices/fuse.[^,]*'
# Expected: smarter-devices/fuse: "20"
```

**Why:** Sandbox pods on Path 1 request `smarter-devices/fuse` as a resource limit. If no node advertises it, pods stay Pending forever. The DaemonSet only runs on nodes with the `smarter-device-manager=enabled` label (configurable via `fuseDevicePlugin.nodeSelector`).

The chart ships a minimal reference DaemonSet. For production, consider the upstream [smarter-device-manager Helm chart](https://gitlab.com/arm-research/smarter/smarter-device-manager) and set `fuseDevicePlugin.enabled: false` in your values.

## Step 5: smoke test

```bash
astonish sandbox k8s-smoke --overlay-mode fuse --privileged
```

**Why:** Validates the end-to-end sandbox lifecycle: create pod, wait for overlay composition, exec a command inside the sandbox, verify output, destroy pod.

**Important:** This command talks directly to the Kubernetes API using your local kubeconfig. It does **not** go through the Astonish API service — no port-forward or ingress required. It exercises the same code path the worker uses internally when creating sandbox sessions.

**Expected output:**
```
✓ backend kind=k8s overlay=fuse privileged=true
✓ created pod astonish-sandbox/smoke-xxxxx
✓ overlay composed (fuse-overlayfs)
✓ exec echo hello → "hello\n"
✓ destroyed pod
```

Adjust `--overlay-mode` and `--privileged` flags to match your chosen path (e.g., `--overlay-mode kernel` for Path 2/4, drop `--privileged` for Path 1).

### Optional: verify Studio access

If you configured ingress and secrets correctly:

```bash
# Or use port-forward if no ingress:
kubectl -n astonish port-forward svc/astonish-api 9393:9393 &

# Open Studio:
open http://localhost:9393
```

## Day-2: upgrades

### What `helm upgrade` does

```bash
helm upgrade astonish deploy/helm/astonish \
  -n astonish \
  -f deploy/helm/astonish/values-myenv.yaml
```

Helm computes a diff between the stored release manifest and the newly rendered templates, then applies only the changes. Existing user-supplied values from previous installs are preserved automatically (unless you pass `--reset-values`).

### What triggers what

| Change | Effect |
|--------|--------|
| `sandbox.image.tag` bumped | Seed Job re-runs (`post-upgrade` hook) to refresh `@base/rootfs`. Sandbox pods created after the Job completes use the new base. |
| `config.*` or `secrets.*` changed | ConfigMap/Secret updated. Api and worker pods restart (rolling update) to pick up new config. |
| `image.tag` bumped | Api and worker deployments roll out new pods with the updated image. |
| `sandbox.overlay.*` changed | ConfigMap updated (overlay settings flow into `config.yaml`). Worker restarts. New sandbox pods use the new overlay settings; existing running pods are unaffected until stopped. |
| Chart version bumped | No inherent effect beyond template changes. |

### After upgrade

```bash
# Re-run platform init to apply any new schema migrations:
astonish platform init --host pg-host --user postgres --password 'yourpass' --suffix a1b2c3
# Or from inside the cluster (Option B from Step 3).

# Wait for seed Job if sandbox image changed:
kubectl -n astonish-sandbox wait job/astonish-sandbox-seed \
  --for=condition=complete --timeout=300s

# Wait for control-plane rollout:
kubectl -n astonish rollout status deploy/astonish-api deploy/astonish-worker
```

**Why re-run `platform init`?** New Astonish versions may include database schema migrations. Running `platform init` with `--suffix` (your existing suffix) after upgrading applies any pending migrations before the new pods start serving requests. Already-applied migrations are no-ops.

## Day-2: changing values

When you edit your values file and re-run `helm upgrade`:

- **Rolling restart** occurs for any deployment whose pod spec changed (image, env vars, mounted configmap hash). The api deployment has 2 replicas by default, so one pod stays up during rollout — no downtime for HTTP requests. SSE connections on the restarting pod are dropped; clients reconnect automatically.
- **PVC changes** (size increase) depend on your StorageClass supporting volume expansion. The chart does not set `allowVolumeExpansion` — that's a StorageClass property.
- **Namespace/PSA label changes** are applied immediately (namespace resource is updated in place).

## Reinstall vs. upgrade

**Use `helm upgrade`** (the default) when:
- You're bumping image tags, changing config, or scaling replicas.
- You want to preserve the release history (`helm history astonish -n astonish`).
- The existing data on PVCs (layers, uppers) should persist.

**Use `helm uninstall` + `helm install`** when:
- You don't care about existing state and want a clean slate.
- You're validating the chart works from scratch (first deployment to a new cluster).
- Something is deeply broken and you want to eliminate accumulated state.

```bash
# Clean slate:
helm uninstall astonish -n astonish
# Verify nothing lingers:
kubectl get all,pvc,cm,secret -n astonish-sandbox
# Re-initialize the platform database (Step 3):
astonish platform init --host pg-host --user postgres --password 'yourpass'
# Re-install:
helm install astonish deploy/helm/astonish \
  -n astonish --create-namespace \
  -f deploy/helm/astonish/values-myenv.yaml
```

**What `helm uninstall` deletes:**
- All chart-managed resources (deployments, services, configmap, secrets, RBAC, PVCs, namespace `astonish-sandbox`).
- The release secret (`sh.helm.release.v1.astonish.*`).

**What it does NOT delete:**
- The `astonish` namespace itself (created by `--create-namespace`, not owned by the chart).
- Any resources you created manually outside the chart.

## Removing the deployment

```bash
helm uninstall astonish -n astonish
```

If you also want to remove the control-plane namespace:

```bash
kubectl delete namespace astonish
```

This is a permanent, destructive operation. All data on the sandbox PVCs (layer content, session uppers) is lost when the PVCs are deleted.

## Troubleshooting

### Pod stuck in `ContainerCreating` with "MountVolume.SetUp failed"

**Cause:** The RWX StorageClass isn't provisioning PVs, or a PVC isn't Bound.

```bash
kubectl -n astonish-sandbox describe pvc astonish-layers
kubectl -n astonish-sandbox describe pod <sandbox-pod>
```

Common culprits: nfs-subdir-external-provisioner not running, CephFS CSI node plugin missing on the target node, `sandbox.storage.storageClassName` typo in values.

### Pod stuck in `Pending` with "forbidden: violates PodSecurity"

**Cause:** Overlay path requires privileged features but the sandbox namespace PSA is set to `baseline` (or vice versa).

```bash
kubectl get ns astonish-sandbox -o jsonpath='{.metadata.labels}' | jq
```

The label `pod-security.kubernetes.io/enforce` must match `sandbox.podSecurity` in your values. Fix the values file and run `helm upgrade`.

### Entrypoint fails: "fuse-overlayfs: /dev/fuse: No such file or directory"

**Cause:** Path 1 selected but no FUSE device plugin running on the node hosting the pod.

```bash
kubectl describe node <node> | grep -A3 Allocatable | grep fuse
```

Either install the device plugin (Step 4.6), switch to Path 3 (`overlay.privileged: true`, `podSecurity: privileged`), or ensure the node has the correct label.

### Entrypoint fails: "mount: overlay: wrong fs type, bad option"

**Cause:** Path 2 (`overlay.mode: kernel`) on a host that blocks kernel overlayfs. Common on nested LXC setups (Proxmox dev clusters).

Switch to Path 1 or Path 3 — both use fuse-overlayfs and bypass the host kernel overlay restriction.

### Pod fails: "forbidden: pods may not specify hostUsers"

**Cause:** `overlay.hostUsers: false` on a cluster older than K8s 1.30, or the UserNamespacesSupport feature gate is not enabled.

Either upgrade the cluster, enable the feature gate, or switch to Path 1 or Path 3.

### `helm install` fails: "Release.Namespace does not match computed control-plane namespace"

**Cause:** The `-n` flag passed to Helm doesn't match `namespaces.controlPlane` (or the value derived from `namespaces.prefix`).

Fix: either change the `-n` flag to match, or set `namespaces.prefix` / `namespaces.controlPlane` in your values so the computed name equals the namespace you're installing into.

### `helm install` fails: "sandbox.storage.storageClassName is required"

**Cause:** The chart requires an explicit RWX StorageClass for sandbox PVCs.

Set `sandbox.storage.storageClassName` in your values file to a valid RWX-capable StorageClass (e.g., `nfs-client`, `cephfs`, `efs-sc`, `azurefile-csi`, `manila-csi`).

### API/worker pods CrashLoopBackOff: "relation does not exist" or "database does not exist"

**Cause:** The platform database was not initialized before installing the chart. The api and worker pods expect the platform database and its schema to already exist.

**Fix:** Run `astonish platform init` (Step 3). Once the database is initialized, the pods will recover on their next restart cycle (within ~30 seconds due to CrashLoopBackOff backoff).

```bash
# Option A (from workstation):
astonish platform init --host pg-host --user postgres --password 'yourpass'

# Option B (from inside the cluster):
kubectl run astonish-platform-init --rm -it --restart=Never \
  -n astonish --image=schardosin/astonish:dev \
  --command -- astonish platform init \
  --host pg-host --user postgres --password 'yourpass'
```

## Gardener (SAP open-source Kubernetes)

Notes for deploying on [Gardener](https://gardener.cloud/)-managed shoots:

- **Garden Linux shoots** (kernel 6.5+) provide native overlayfs and user namespaces. Both Path 1 and Path 2 work cleanly.
- **No default RWX StorageClass** on most Gardener infrastructures — provision a shared filesystem (e.g., Manila on OpenStack, EFS on AWS, Azure Files on Azure) and set `sandbox.storage.storageClassName` accordingly.
- **PSA defaults** — Gardener sets `privileged` on the `default` namespace. The sandbox namespace the chart creates pins its own PSA label explicitly, so Gardener's defaults don't affect sandbox pod admission.
- When migrating between Gardener shoots or changing infrastructure providers: validate the StorageClass name exists on the target shoot before migrating, as PVC provisioning silently stalls if the class is absent.

## Reference

- **Architecture:** `docs/architecture/sandbox-backends.md` — full design (Sections 4, 5, 10 cover the K8s backend).
- **Chart source:** `deploy/helm/astonish/` — templates, helpers, validations.
- **Values reference:** `deploy/helm/astonish/values.yaml` — exhaustive list of every tunable with inline documentation.
- **Backend implementation:** `pkg/sandbox/k8s/` — Go source for pod builders, overlay dispatcher, security helpers.
- **Config struct:** `pkg/config/app_config.go` → `SandboxKubernetesConfig`.
- **Sandbox base image:** `docker/sandbox-base/Dockerfile` — multi-stage build producing the sandbox pod rootfs.
