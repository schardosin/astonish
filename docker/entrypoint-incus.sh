#!/bin/bash
# entrypoint-incus.sh — Starts the Incus daemon inside the Docker container
# and configures it for remote API access from the host.
#
# On first run (fresh volume), this also runs `incus admin init` to create
# the storage pool, network bridge, and default profile.

set -e

echo "[astonish-incus] Starting Incus daemon..."

# Start incusd in the background
incusd --group incus-admin &
INCUSD_PID=$!

# Wait for the daemon to be ready
echo "[astonish-incus] Waiting for Incus daemon..."
MAX_WAIT=30
WAITED=0
while ! incus info >/dev/null 2>&1; do
    sleep 1
    WAITED=$((WAITED + 1))
    if [ "$WAITED" -ge "$MAX_WAIT" ]; then
        echo "[astonish-incus] ERROR: Incus daemon did not start within ${MAX_WAIT}s"
        exit 1
    fi
done
echo "[astonish-incus] Incus daemon ready (${WAITED}s)"

# First-run initialization: create storage pool, network, and profile.
# Skip if already initialized (pool exists).
if ! incus storage list --format csv 2>/dev/null | grep -q "^default,"; then
    echo "[astonish-incus] First run: initializing Incus..."

    # Preseed Incus with a minimal configuration:
    # - dir storage backend (overlayfs handles CoW for fast clones)
    # - managed bridge network with NAT
    # - default profile with root disk and network device
    cat <<'PRESEED' | incus admin init --preseed
config:
  core.https_address: "[::]:8443"
storage_pools:
  - name: default
    driver: dir
networks:
  - name: incusbr0
    type: bridge
    config:
      ipv4.address: auto
      ipv4.nat: "true"
      ipv6.address: none
profiles:
  - name: default
    devices:
      root:
        type: disk
        path: /
        pool: default
      eth0:
        type: nic
        network: incusbr0
        name: eth0
PRESEED

    echo "[astonish-incus] Incus initialized."
else
    # Already initialized — just ensure the API is listening on all interfaces
    CURRENT_ADDR=$(incus config get core.https_address 2>/dev/null || echo "")
    if [ -z "$CURRENT_ADDR" ]; then
        echo "[astonish-incus] Enabling HTTPS API on :8443..."
        incus config set core.https_address "[::]:8443"
    fi
    echo "[astonish-incus] Incus already initialized, using existing data."
fi

echo "[astonish-incus] Incus API listening on :8443"

# Keep the container running by waiting on incusd
wait $INCUSD_PID
