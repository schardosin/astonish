# deploy/k8s — deprecated

The static manifests that once lived here have been replaced by the Helm
chart at `deploy/helm/astonish`. All cluster-side resources (namespaces,
RBAC, PVCs, base-layer seed Job, optional FUSE device plugin) are now
templates in that chart, driven by a single per-environment values file.

## Quickstart

Dev cluster (K3s on LXC-on-Proxmox):

```bash
helm upgrade --install astonish deploy/helm/astonish \
  -n astonish --create-namespace \
  -f deploy/helm/astonish/values-dev-proxmox.yaml

kubectl -n astonish-sandbox wait job/astonish-sandbox-seed \
  --for=condition=complete --timeout=300s
```

Other environments: copy `values-dev-proxmox.yaml` to
`values-<env>.yaml`, adjust the overrides (StorageClass, overlay mode,
PSA profile, secrets), and run the same command with that file.

## References

- `deploy/helm/astonish/values.yaml` — full list of tunables with inline docs.
- `docs/deployment/kubernetes.md` — deployment guide and troubleshooting.
- `docs/architecture/sandbox-backends.md` §10 — Phase F overlay strategies.
