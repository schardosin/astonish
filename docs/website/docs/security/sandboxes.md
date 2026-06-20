# Sandboxes

Astonish executes agent tool calls inside isolated sandbox environments. Each organization gets its own network-isolated sandbox infrastructure, preventing lateral movement between tenants even if code execution is compromised.

## Per-Org Network Isolation

Every organization's sandboxes run on a dedicated network segment. Containers from different orgs cannot communicate at the network layer — isolation is enforced by the infrastructure, not application logic.

## Backends

Astonish supports two sandbox backends depending on your deployment model.

### Incus (LXC Containers)

For non-Kubernetes deployments, Astonish uses [Incus](https://linuxcontainers.org/incus/) to manage LXC system containers with per-org bridge networks.

#### Network Architecture

Each organization gets a dedicated bridge interface:

```
org-a-br0  →  10.100.1.0/24  →  org-a containers only
org-b-br0  →  10.100.2.0/24  →  org-b containers only
org-c-br0  →  10.100.3.0/24  →  org-c containers only
```

Bridge-level isolation means org-a containers have no route to org-b, regardless of firewall rules.

#### Container Naming

Containers follow a deterministic naming convention:

```
astonish-{org}-{team}-{session-short-id}
```

For example: `astonish-acme-backend-a1b2c3`

This makes it straightforward to identify, monitor, and clean up containers per org or team.

#### Lifecycle

1. Agent session starts → Astonish creates a container on the org's bridge.
2. Tool calls execute inside the container with resource limits (CPU, memory, disk).
3. Session ends → container is destroyed and network resources released.

### Kubernetes

For platform deployments on Kubernetes, sandboxes run as pods with NetworkPolicy-based isolation.

#### Pod Labels

Every sandbox pod is labeled with org and team identifiers:

```yaml
metadata:
  labels:
    astonish.dev/org: acme
    astonish.dev/team: backend
    astonish.dev/sandbox: "true"
    astonish.dev/session: a1b2c3d4
```

#### NetworkPolicies

A NetworkPolicy is applied per org to restrict traffic:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: astonish-sandbox-acme
spec:
  podSelector:
    matchLabels:
      astonish.dev/org: acme
      astonish.dev/sandbox: "true"
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              astonish.dev/org: acme
  egress:
    - to:
        - podSelector:
            matchLabels:
              astonish.dev/org: acme
```

Pods from org `acme` can only communicate with other `acme` pods. Cross-org traffic is denied by default.

## Team-Scoped Sandbox Templates

Organizations can define sandbox templates per team, controlling the base image, resource limits, and pre-installed tools:

```yaml
sandboxes:
  templates:
    backend:
      image: astonish-sandbox:python-3.12
      cpu: "2"
      memory: 4Gi
      tools: [git, python, pip]
    frontend:
      image: astonish-sandbox:node-22
      cpu: "1"
      memory: 2Gi
      tools: [git, node, npm]
```

Templates are scoped to teams — the `backend` team gets Python environments while `frontend` gets Node.js.

## See Also

- [Deployment Overview](../deployment/index.md) — choosing between Incus and Kubernetes backends
- [OpenShell](../deployment/openshell.md) — kernel-level agent isolation via NVIDIA OpenShell
- [Credential Security](./credential-security.md) — how secrets are injected into sandbox environments
