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

### 4. Create Organization and Users

Set environment variables for CLI access:

```bash
export ASTONISH_PLATFORM_DSN="<dsn-from-step-1>"
export ASTONISH_MASTER_KEY="<master-key-from-step-2>"
```

Create your organization and first user:

```bash
./astonish platform org create --name "My Organization" --slug myorg

./astonish platform org invite \
  --org myorg \
  --email admin@company.com \
  --role owner \
  --password
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
  repository: schardosin/astonish
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
    # Gateway address — leave empty for auto-derived:
    #   "{release}-openshell.{namespace}.svc.cluster.local:8080"
    gateway:
      addr: ""
    # Sandbox container image (Astonish agent tooling + OpenShell supervisor)
    image:
      repository: schardosin/astonish-sandbox-openshell
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
    sandboxImage: "schardosin/astonish-sandbox-openshell:latest"
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
    sandboxImage: "schardosin/astonish-sandbox-openshell:latest"
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

Log in with the credentials you created during platform initialization.

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

### Namespace Convention

The `namespaces.prefix` value drives all namespace names:

| Setting | Result |
|---------|--------|
| `prefix: astonish` | Control plane: `astonish`, Sandboxes: `astonish-sandbox` |
| `prefix: astonish-prod` | Control plane: `astonish-prod`, Sandboxes: `astonish-prod-sandbox` |

The `openshell.server.sandboxNamespace` must match the computed sandbox namespace.

## See Also

- [Sandboxes](../security/sandboxes.md) — security model and isolation architecture
- [Kubernetes Deployment](./kubernetes.md) — standard K8s deployment (without OpenShell)
- [Deployment Overview](./index.md) — choosing between deployment models
