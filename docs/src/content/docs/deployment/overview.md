---
title: Deployment
description: Deploy Astonish beyond a single machine
---

Astonish supports two deployment models depending on your scale and isolation requirements.

## Single-binary daemon (default)

The simplest path. Install Astonish on any machine, run `astonish daemon install && astonish daemon start`, and you have a fully functional assistant with Studio, chat, flows, scheduling, and all 74+ tools. Sessions run in local sandboxes (Docker or Incus) on the same host.

This is covered in [Running as a Service](/astonish/getting-started/running-as-a-service/).

## Kubernetes cluster

For teams, multi-tenant platforms, or environments where sandbox workloads must be isolated from the control plane, Astonish deploys on Kubernetes via a single Helm chart. The chart provisions:

- **Control plane** — stateless API servers + a background worker, horizontally scalable.
- **Sandbox subsystem** — a dedicated namespace with RBAC, RWX PVCs, and a one-shot seed Job that prepares the base layer all sandbox pods mount.
- **Optional FUSE device plugin** — for production clusters that need `/dev/fuse` without granting privileged pods.

The Helm chart is self-contained: one install creates both namespaces, all RBAC, storage, and the seed pipeline. No manual `kubectl apply` steps outside the chart.

See the full guide: [Kubernetes Deployment](/astonish/deployment/kubernetes/).

## Choosing between them

| Concern | Single-binary | Kubernetes |
|---------|---------------|------------|
| Setup time | 2 minutes | 15-30 minutes |
| Sandbox isolation | Container on same host | Dedicated pods on cluster nodes |
| Horizontal scaling | Single process | Multiple API replicas + worker |
| Multi-tenant | Single user | Platform-level auth + org isolation |
| Production hardening | Systemd restart, local backups | Pod security, RBAC, RWX storage, rolling upgrades |

Most individual users should start with the single-binary daemon. Move to Kubernetes when you need multi-user access, stronger sandbox isolation, or integration with existing cluster infrastructure.
