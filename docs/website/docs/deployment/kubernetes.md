# Kubernetes Deployment

This guide covers deploying Astonish as a multi-tenant platform on Kubernetes using the official Helm chart.

## Prerequisites

- Kubernetes 1.28+ cluster
- Helm 3.12+
- `kubectl` configured for your cluster
- PostgreSQL 15+ with the `pgvector` extension enabled
- A container registry accessible from the cluster
- A 256-bit master encryption key

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  Kubernetes Cluster                                 │
│                                                     │
│  ┌──────────┐  ┌──────────┐  ┌─────────────────┐  │
│  │ Astonish │  │ Astonish │  │ Sandbox Pods     │  │
│  │  API     │  │  Worker  │  │ (per-org netpol) │  │
│  └────┬─────┘  └────┬─────┘  └─────────────────┘  │
│       │              │                              │
│       └──────┬───────┘                              │
│              │                                      │
│       ┌──────▼──────┐                               │
│       │ PostgreSQL  │                               │
│       │ + pgvector  │                               │
│       └─────────────┘                               │
└─────────────────────────────────────────────────────┘
```

## Step 1: Create Namespace

```bash
kubectl create namespace astonish
```

## Step 2: Configure Secrets

Create the required secrets before installing the chart:

```bash
# Generate a master encryption key
export MASTER_KEY=$(openssl rand -base64 32)

# Create the secret
kubectl create secret generic astonish-secrets \
  --namespace astonish \
  --from-literal=master-key="$MASTER_KEY" \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=database-url="postgres://astonish:password@postgres:5432/astonish?sslmode=require"
```

## Step 3: PostgreSQL Setup

Astonish requires a PostgreSQL instance with `pgvector`. You can use a managed service (RDS, Cloud SQL, Azure Database) or deploy in-cluster.

Enable the extension:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

Astonish uses **separate databases per organization** for tenant isolation. The application role needs `CREATEDB` privilege:

```sql
CREATE ROLE astonish WITH LOGIN PASSWORD 'secure-password' CREATEDB;
```

## Step 4: Helm Values

Create a `values.yaml` file:

```yaml
replicaCount: 2

image:
  repository: registry.astonish.dev/astonish
  tag: "latest"

config:
  mode: platform
  log_level: info

database:
  existingSecret: astonish-secrets
  secretKey: database-url

encryption:
  existingSecret: astonish-secrets
  secretKey: master-key

auth:
  jwt:
    existingSecret: astonish-secrets
    secretKey: jwt-secret
  oidc:
    enabled: true
    issuer: https://login.example.com
    clientId: astonish-platform

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: astonish.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: astonish-tls
      hosts:
        - astonish.example.com

sandboxes:
  backend: kubernetes
  resources:
    defaultCpu: "1"
    defaultMemory: 2Gi
    maxCpu: "4"
    maxMemory: 8Gi

resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: "2"
    memory: 2Gi
```

## Step 5: Install the Chart

```bash
helm install astonish oci://registry.astonish.dev/charts/astonish \
  --namespace astonish \
  --values values.yaml
```

## Step 6: Initialize the Platform

After the pods are running, bootstrap the database schema and create the initial admin:

```bash
# Port-forward to the API pod (or use the ingress)
kubectl port-forward -n astonish svc/astonish 8080:8080 &

# Initialize database (creates roles, runs migrations)
astonish platform init \
  --url https://astonish.example.com \
  --admin-email admin@example.com
```

This command:
1. Creates the application database schema
2. Sets up the audit log table with restricted grants
3. Creates the first organization and admin user
4. Generates the initial org DEK (encrypted with the master KEK)

## Step 7: Verify the Deployment

```bash
# Check pod status
kubectl get pods -n astonish

# Check logs
kubectl logs -n astonish -l app=astonish --tail=50

# Health check
curl https://astonish.example.com/health
```

Expected health response:

```json
{
  "status": "healthy",
  "database": "connected",
  "version": "0.x.y"
}
```

## Upgrades

Upgrade to a new version:

```bash
helm upgrade astonish oci://registry.astonish.dev/charts/astonish \
  --namespace astonish \
  --values values.yaml \
  --set image.tag="new-version"
```

Astonish runs database migrations automatically on startup. For major version upgrades, consult the release notes for any required manual migration steps.

## Scaling

The API server is stateless and can be horizontally scaled:

```yaml
replicaCount: 4

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilization: 70
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Pod in CrashLoopBackOff | Missing secret or bad DB URL | Check `kubectl describe pod` and secret values |
| 500 on `/health` | Database unreachable | Verify PostgreSQL connectivity and credentials |
| OIDC login fails | Incorrect issuer URL | Confirm the issuer matches your provider's discovery endpoint |
| Sandbox pods not starting | Missing NetworkPolicy controller | Install a CNI that supports NetworkPolicy (Calico, Cilium) |

## See Also

- [Deployment Overview](./index.md) — comparison of deployment models
- [OpenShell](./openshell.md) — kernel-level agent sandboxing with NVIDIA OpenShell
- [Authentication](../security/authentication.md) — configuring OIDC providers
- [Envelope Encryption](../security/envelope-encryption.md) — master key management
