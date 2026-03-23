#!/bin/bash
# entrypoint-incus.sh — Starts the Incus daemon inside the Docker container
# and configures it for remote API access from the host.
#
# On first run (fresh volume), this also runs `incus admin init` to create
# the storage pool, network bridge, and default profile.

set -e

# Zabbly packages install Incus binaries to /opt/incus/bin/ and shared
# libraries to /opt/incus/lib/. Add both to PATH and LD_LIBRARY_PATH.
export PATH="/opt/incus/bin:$PATH"
export LD_LIBRARY_PATH="/opt/incus/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

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
    # - managed bridge network with NAT (static subnet to avoid auto-detect
    #   failures in Docker's limited network namespace)
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
      ipv4.address: "10.99.0.1/24"
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

# Generate a client certificate for the host to authenticate over TCP.
# The cert/key are stored on the persistent volume so they survive container
# recreation. The cert is added to the Incus trust store on first run.
CLIENT_CERT_DIR="/var/lib/incus/astonish-client"
CLIENT_CERT="$CLIENT_CERT_DIR/client.crt"
CLIENT_KEY="$CLIENT_CERT_DIR/client.key"

if [ ! -f "$CLIENT_CERT" ] || [ ! -f "$CLIENT_KEY" ]; then
    echo "[astonish-incus] Generating client certificate for host access..."
    mkdir -p "$CLIENT_CERT_DIR"
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
        -sha256 -nodes -days 3650 \
        -subj "/CN=astonish-host-client" \
        -keyout "$CLIENT_KEY" -out "$CLIENT_CERT" >/dev/null 2>&1
    # Add the cert to Incus trust store
    incus config trust add-certificate "$CLIENT_CERT" --name astonish-host 2>/dev/null || true
    echo "[astonish-incus] Client certificate generated and trusted."
else
    # Ensure the cert is in the trust store (may have been lost if Incus DB was recreated)
    if ! incus config trust list --format csv 2>/dev/null | grep -q "astonish-host"; then
        incus config trust add-certificate "$CLIENT_CERT" --name astonish-host 2>/dev/null || true
        echo "[astonish-incus] Client certificate re-added to trust store."
    fi
fi

# Keep the container running by waiting on incusd
wait $INCUSD_PID
