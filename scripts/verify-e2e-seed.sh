#!/usr/bin/env bash
# verify-e2e-seed.sh — Positive verification that the e2e sandbox @base layer
# is actually seeded.
#
# After `make e2e-k8s-up`, helm has already waited for the seed Job hook to
# complete and (per hook-delete-policy: hook-succeeded) deleted it. We can no
# longer inspect the Job — but we CAN mount the layers PVC and verify that
# /@base/rootfs is populated.
#
# Spins up a short-lived busybox pod, mounts the layers PVC read-only, checks
# that /@base/rootfs/usr/bin exists and is non-empty. Exits non-zero on
# failure.
#
# Required env:
#   E2E_K8S_SANDBOX_NS  (default: astonishe2e-sandbox)
#   E2E_K8S_LAYERS_PVC  (default: astonish-layers)
#
# Usage:
#   scripts/verify-e2e-seed.sh
set -euo pipefail

NS="${E2E_K8S_SANDBOX_NS:-astonishe2e-sandbox}"
PVC="${E2E_K8S_LAYERS_PVC:-astonish-layers}"
POD_NAME="e2e-seed-verify-$$"
WAIT_TIMEOUT="${E2E_SEED_VERIFY_TIMEOUT:-90s}"

cleanup() {
  kubectl -n "$NS" delete pod "$POD_NAME" --ignore-not-found --force --grace-period=0 >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Pre-check: the PVC must exist (catches the most common failure mode early
# instead of waiting for a pod scheduling timeout).
if ! kubectl -n "$NS" get pvc "$PVC" >/dev/null 2>&1; then
  echo "ERROR: PVC '$PVC' not found in namespace '$NS'" >&2
  echo "  Re-provision with: make e2e-k8s-down && make e2e-k8s-up" >&2
  exit 1
fi

POD_MANIFEST=$(cat <<EOF
{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": { "name": "$POD_NAME", "namespace": "$NS" },
  "spec": {
    "restartPolicy": "Never",
    "containers": [
      {
        "name": "verify",
        "image": "busybox:1.36",
        "command": ["sh", "-c"],
        "args": [
          "if [ -d /mnt/layers/@base/rootfs/usr/bin ] && [ -n \"\$(ls -A /mnt/layers/@base/rootfs/usr/bin 2>/dev/null)\" ]; then echo OK; exit 0; else echo MISSING; exit 1; fi"
        ],
        "volumeMounts": [{ "name": "layers", "mountPath": "/mnt/layers", "readOnly": true }]
      }
    ],
    "volumes": [
      { "name": "layers", "persistentVolumeClaim": { "claimName": "$PVC", "readOnly": true } }
    ]
  }
}
EOF
)

# Create the pod, wait for it to terminate, then inspect logs + status.
echo "$POD_MANIFEST" | kubectl apply -f - >/dev/null

if ! kubectl -n "$NS" wait --for=jsonpath='{.status.phase}'=Succeeded \
       "pod/$POD_NAME" --timeout="$WAIT_TIMEOUT" >/dev/null 2>&1; then
  PHASE=$(kubectl -n "$NS" get "pod/$POD_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
  echo "ERROR: seed-verify pod did not Succeed (phase=$PHASE)" >&2
  echo "  Pod logs:" >&2
  kubectl -n "$NS" logs "pod/$POD_NAME" 2>&1 | sed 's/^/    /' >&2 || true
  echo "  Recent events:" >&2
  kubectl -n "$NS" get events --sort-by=.lastTimestamp 2>&1 | tail -10 | sed 's/^/    /' >&2 || true
  echo "" >&2
  echo "  This usually means the helm post-install seed Job failed silently." >&2
  echo "  Re-provision with: make e2e-k8s-down && make e2e-k8s-up" >&2
  exit 1
fi

LOGS=$(kubectl -n "$NS" logs "pod/$POD_NAME" 2>&1 || true)
if echo "$LOGS" | grep -q "^OK$"; then
  echo "Seed verification: @base/rootfs populated in PVC '$PVC' (ns: $NS)"
  exit 0
fi

echo "ERROR: @base layer not seeded in PVC '$PVC' (ns: $NS)" >&2
echo "  Pod logs:" >&2
echo "$LOGS" | sed 's/^/    /' >&2
echo "" >&2
echo "  Re-provision with: make e2e-k8s-down && make e2e-k8s-up" >&2
exit 1
