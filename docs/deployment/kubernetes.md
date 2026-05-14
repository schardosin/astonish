# Kubernetes Deployment Guide

This guide walks through deploying Astonish (control plane + Kubernetes sandbox backend) on a concrete cluster using the Helm chart at `deploy/helm/astonish`. It is the companion to `docs/architecture/sandbox-backends.md` (design).

**Scope (Phase F).** The backend uses a portable overlay strategy that works on any recent Kubernetes cluster — no Sysbox dependency, no custom kernel modules, no CNI assumptions beyond standard NetworkPolicy. Sysbox remains optional for operators who already run it.

## Prerequisites

- Kubernetes cluster. Supported: self-managed (Gardener, Kubernikus, Rancher, kubeadm, K3s, k0s), plus EKS/AKS/GKE with a node pool you control.
  - **Not supported:** GKE Autopilot, EKS Fargate, AKS virtual nodes (forbid the DaemonSet / hostPath access several paths need).
- A ReadWriteMany StorageClass. Examples: CephFS, NFS, EFS, Azure Files, Manila (SAP Converged Cloud), nfs-subdir-external-provisioner.
- `kubectl` 1.30+ and `helm` 3.8+ with access to apply cluster-scoped resources (namespaces, potentially a DaemonSet).
- Access to the Astonish control-plane image (`schardosin/astonish:<tag>`) and sandbox base image (`schardosin/astonish-sandbox-base:<tag>`).

## Step 1: pick an overlay strategy

Pick the leftmost column that matches your cluster. You can change later via values; this decision only affects which Kubernetes features the backend relies on.

| Path | Best for | `sandbox.podSecurity` | `sandbox.overlay.*` | Extras |
|------|----------|-----------------------|---------------------|--------|
| **1. Device plugin** | Production clusters with standard runtimes (containerd + runc) | `baseline` | `mode: fuse`, `fuseDeviceResource: smarter-devices/fuse` | FUSE device plugin (chart ships one, or install upstream) |
| **2. User namespace** | K8s ≥ 1.33 with UserNamespacesSupport enabled | `baseline` | `mode: kernel`, `hostUsers: false` | Nothing — kernel overlayfs works under userns |
| **3. Privileged** | Dev / lab, nested-LXC nodes, "I want it working in 5 minutes" | `privileged` | `mode: fuse`, `privileged: true` | Nothing |
| **4. Sysbox** | Clusters already running Sysbox | `baseline` | `mode: kernel`, `runtimeClassName: sysbox-runc` | Sysbox (`sysbox-deploy-k8s`) |

Most readers should use **Path 1** (production) or **Path 3** (dev).

## Step 2: create a values file

Copy `values-dev-proxmox.yaml` as a starting point and adjust for your environment:

```bash
cp deploy/helm/astonish/values-dev-proxmox.yaml deploy/helm/astonish/values-myenv.yaml
```

Minimum required overrides per path:

```yaml
# Path 1 — device plugin (production)
sandbox:
  podSecurity: baseline
  overlay:
    mode: fuse
    privileged: false
    fuseDeviceResource: smarter-devices/fuse
  storage:
    storageClassName: ceph-filesystem    # or efs-sc, azurefile-csi, manila-csi, ...
fuseDevicePlugin:
  enabled: true                           # install the reference plugin

# Path 2 — user namespace
sandbox:
  podSecurity: baseline
  overlay:
    mode: kernel
    hostUsers: false
  storage:
    storageClassName: <rwx-class>

# Path 3 — privileged (dev)
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
    storageClassName: <rwx-class>
```

Size guidance for the two PVCs:
- `sandbox.storage.layers.size`: 100 GiB is fine for ~50 templates. Plan for 1–5 GiB per distinct template.
- `sandbox.storage.uppers.size`: 50 GiB is a starting point; roughly proportional to concurrently-suspended sessions × upper-layer size.

Secrets (`secrets.masterKey`, `secrets.jwtSecret`, `secrets.platformDSN`) MUST be set in the values file (not defaults).

## Step 3: install the chart

```bash
helm upgrade --install astonish deploy/helm/astonish \
  -n astonish --create-namespace \
  -f deploy/helm/astonish/values-myenv.yaml
```

The chart creates:
- Control-plane namespace (`Release.Namespace`, must equal `namespaces.controlPlane`; default `astonish`).
- Sandbox namespace (`namespaces.sandbox`; default `astonish-sandbox`).
- Control-plane `ServiceAccount` in the control-plane ns + a `Role`/`RoleBinding` in the sandbox ns.
- Two RWX PVCs in the sandbox ns.
- A `post-install`/`post-upgrade` Helm hook `Job` that seeds `@base`.
- Optional FUSE device plugin DaemonSet (when `fuseDevicePlugin.enabled=true`).

Wait for the seed Job to complete:

```bash
kubectl -n astonish-sandbox wait job/astonish-sandbox-seed \
  --for=condition=complete --timeout=300s
```

## Step 4 (Path 1 only): verify the FUSE device plugin

```bash
# Label the nodes that should host sandbox pods (if you kept the default nodeSelector):
kubectl label node <node-N> smarter-device-manager=enabled

# Verify the resource is advertised:
kubectl get node <node-N> -o jsonpath='{.status.allocatable}' | grep -o 'smarter-devices/fuse.[^,]*'
# Expected: smarter-devices/fuse: "20"
```

The chart ships a minimal reference DaemonSet. For production consider the upstream [smarter-device-manager Helm chart](https://gitlab.com/arm-research/smarter/smarter-device-manager) with `fuseDevicePlugin.enabled: false` in your values.

## Step 5: smoke test

```bash
# In-cluster smoke (requires port-forward to the API):
kubectl -n astonish port-forward svc/astonish-api 9393:9393 &

# Run the built-in k8s smoke command against the local control plane:
astonish sandbox k8s-smoke
# Expected: ✓ backend kind=k8s ... ✓ created pod ... ✓ exec echo hello → "hello\n" ... ✓ destroyed pod
```

If the smoke succeeds, run a real flow through the deployed API.

## Troubleshooting

### Pod stuck in `ContainerCreating` with "MountVolume.SetUp failed"

**Cause:** the RWX StorageClass isn't provisioning PVs, or the layers PVC isn't Bound.

```bash
kubectl -n astonish-sandbox describe pvc astonish-layers
kubectl -n astonish-sandbox describe pod <sandbox-pod>
```

Common culprits: nfs-subdir-external-provisioner not running, CephFS CSI node plugin missing on the target node, `sandbox.storage.storageClassName` typo.

### Pod stuck in `Pending` with "forbidden: violates PodSecurity"

**Cause:** Path 3 values deployed to a namespace with `podSecurity: baseline`, OR Path 1/2 with `overlay.privileged: true` accidentally set.

```bash
kubectl get ns astonish-sandbox -o jsonpath='{.metadata.labels}' | jq
```

The label `pod-security.kubernetes.io/enforce` must match `sandbox.podSecurity` in your values. Re-run `helm upgrade --install` after fixing.

### Entrypoint fails with "fuse-overlayfs: no such file or directory: /dev/fuse"

**Cause:** Path 1 selected but no device plugin running on the node hosting the pod.

```bash
kubectl describe node <node> | grep -A3 Allocatable | grep fuse
```

Either install the device plugin (Step 4), switch to Path 3 (`overlay.privileged: true`, `podSecurity: privileged`), or restrict sandbox pod scheduling via node selector.

### Entrypoint fails with "mount: overlay: wrong fs type, bad option"

**Cause:** Path 2 (`overlay.mode: kernel`) on a cluster where the host kernel blocks overlay. Common on nested LXC setups (Proxmox dev clusters).

Switch to Path 1 or Path 3 — both use fuse-overlayfs and don't depend on host kernel overlay support.

### Pod creation fails with "forbidden: pods may not specify hostUsers"

**Cause:** `overlay.hostUsers: false` on a cluster older than K8s 1.30, or UserNamespacesSupport not enabled.

Either upgrade the cluster, enable the feature gate, or switch to Path 1 or Path 3.

### `helm install` fails: "Release.Namespace ... does not match computed control-plane namespace ..."

**Cause:** the `-n` flag doesn't match `namespaces.controlPlane` (or the prefix).

Fix by either changing the `-n` flag or setting `namespaces.prefix` / `namespaces.controlPlane` in your values.

### `helm install` fails: "sandbox.storage.storageClassName is required"

**Cause:** the chart refuses to template PVCs without an explicit RWX StorageClass.

Set `sandbox.storage.storageClassName` in your values file.

## SAP Converged Cloud notes

The platform team's target environment:
- **Gardener shoots** on Garden Linux (kernel ≥ 6.5) provide native overlayfs and userns; Path 2 and Path 1 both work cleanly.
- **No default RWX class** — provision Manila (OpenStack shared file system) and set `sandbox.storage.storageClassName: manila-csi`.
- **PSA defaults to `privileged`** on the `default` namespace; the sandbox namespace we create pins `baseline` (or whatever you set `sandbox.podSecurity` to) explicitly, so this doesn't matter for sandbox pods.
- Kubernikus clusters being migrated to Gardener: validate the storage class name before migration, otherwise PVC provisioning silently stalls.

## Reference

- `docs/architecture/sandbox-backends.md` — full architecture (Sections 4, 5, 10 are the K8s-specific parts).
- `deploy/helm/astonish/` — chart source.
- `deploy/helm/astonish/values.yaml` — exhaustive list of tunables with inline docs.
- `pkg/sandbox/k8s/` — backend implementation.
- `pkg/config/app_config.go` → `SandboxKubernetesConfig` — Go-side config struct.
