---
title: Deployment
description: Deploy Astonish beyond a single machine
---

Astonish supports three deployment models depending on your scale, isolation, and security requirements.

## Single-binary daemon (default)

The simplest path. Install Astonish on any machine, run `astonish daemon install && astonish daemon start`, and you have a fully functional assistant with Studio, chat, flows, scheduling, and all 74+ tools. Sessions run in local sandboxes (Docker or Incus) on the same host.

This is covered in [Running as a Service](/astonish/getting-started/running-as-a-service/).

## Kubernetes cluster

For teams, multi-tenant platforms, or environments where sandbox workloads must be isolated from the control plane, Astonish deploys on Kubernetes via a single Helm chart. Two sandbox backends are available:

### Standard K8s backend (`k8s`)

Direct pod creation via the Kubernetes API. The chart provisions:

- **Control plane** — stateless API servers + a background worker, horizontally scalable.
- **Sandbox subsystem** — a dedicated namespace with RBAC, RWX PVCs, and a one-shot seed Job that prepares the base layer all sandbox pods mount.
- **Optional FUSE device plugin** — for production clusters that need `/dev/fuse` without granting privileged pods.

The Helm chart is self-contained: one install creates both namespaces, all RBAC, storage, and the seed pipeline. No manual `kubectl apply` steps outside the chart.

### OpenShell backend (`openshell`) — enhanced security

Uses [NVIDIA OpenShell](https://github.com/NVIDIA/OpenShell) to manage sandboxes with full 4-layer in-pod security:

- **Landlock** — filesystem access policies per process (agent can only read/write within `/sandbox`).
- **seccomp** — syscall filtering tailored to AI agent workloads (blocks dangerous syscalls like `ptrace`, `mount`, `reboot`).
- **Network namespace** — L7-aware network enforcement with policy proxy (blocks unauthorized egress, logs all connections).
- **OCSF audit** — structured security event logging for every tool call, file access, and network connection.

The OpenShell backend deploys an NVIDIA gateway as a Helm subchart (same namespace, managed automatically) that handles sandbox lifecycle, supervisor injection, and workspace provisioning. Istio service mesh provides inter-service encryption and identity. Astonish communicates with the gateway via gRPC through the mesh to create/manage sandboxes. The OpenShell supervisor runs as PID 1 inside each sandbox pod, enforcing security policies on all spawned processes.

**When to choose OpenShell over standard K8s:**

| Concern | Standard K8s | OpenShell |
|---------|-------------|-----------|
| Setup complexity | Simpler (no gateway) | Requires Istio + gateway subchart + CRD |
| Security model | Container boundary only | 4-layer in-pod enforcement |
| Audit trail | Pod-level events | Per-process OCSF events |
| Network control | K8s NetworkPolicy (L3/L4) | Per-process L7 policy proxy |
| Filesystem isolation | OverlayFS chroot | Landlock per-process paths |
| Maturity | Production-proven | Newer, evolving with NVIDIA |

Both backends share the same session lifecycle API. The OpenShell backend uses custom sandbox images (instead of overlay layers) for workspace customization. The choice is transparent to the AI agent — it sees the same `/sandbox` workspace regardless of backend.

See the full guide: [Kubernetes Deployment](/astonish/deployment/kubernetes/).

## Choosing between them

| Concern | Single-binary | Kubernetes (k8s) | Kubernetes (openshell) |
|---------|---------------|------------------|------------------------|
| Setup time | 2 minutes | 15-30 minutes | 30-45 minutes |
| Sandbox isolation | Container on same host | Dedicated pods on cluster nodes | Pods + in-pod 4-layer security |
| Horizontal scaling | Single process | Multiple API replicas + worker | Same as K8s + gateway scaling |
| Multi-tenant | Single user | Platform-level auth + org isolation | Same + per-process audit |
| Production hardening | Systemd restart, local backups | Pod security, RBAC, RWX storage | All K8s features + Landlock, seccomp, OCSF |
| Network control | Host iptables | NetworkPolicy | L7 policy proxy per-process |

Most individual users should start with the single-binary daemon. Move to Kubernetes (`k8s` backend) when you need multi-user access, stronger sandbox isolation, or integration with existing cluster infrastructure. Upgrade to `openshell` when you need defense-in-depth inside the sandbox (regulated environments, untrusted agent workloads, audit requirements).
