# deploy/k8s — cluster manifests for the K8s+Sysbox sandbox backend.
#
# These manifests provision the cluster-side dependencies the backend
# needs:
#
#   00-namespaces.yaml     Two namespaces (control-plane + sandboxes)
#   10-rbac.yaml           ServiceAccount + Role + RoleBinding
#   20-runtimeclass.yaml   Sysbox RuntimeClass for user-namespace isolation
#   30-storage.yaml        PVCs for layers (RWX RO fan-out) and uppers (RWX RW)
#   40-seed-base-layer.yaml Job that seeds the @base layer into the layers PVC
#
# Apply order is encoded in filename prefixes; `kubectl apply -f deploy/k8s`
# applies lexicographically so the order is respected.
#
# Prerequisites:
#   - A CephFS (or any RWX) StorageClass named ceph-filesystem. Edit
#     30-storage.yaml if your cluster uses a different RWX class.
#   - Sysbox installed on nodes that should host sandbox pods
#     (see https://github.com/nestybox/sysbox).
#   - The astonish-sandbox-base image published where the cluster can
#     pull it (default: docker.io/schardosin/astonish-sandbox-base:latest;
#     override via SandboxKubernetesConfig.SandboxImage).
#
# These manifests are intentionally plain YAML (not Helm) so the Phase
# D smoke test can `kubectl apply -f` without installing extra tooling.
# A Helm chart may be added later alongside deploy/helm/astonish.
#
# Reference: docs/architecture/sandbox-backends.md §§4, 5, 11.
