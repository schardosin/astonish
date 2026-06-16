# Dockerfile — astonish-sandbox-openshell image.
#
# This is the container image for sandbox pods managed by the OpenShell
# backend (SandboxConfig.Backend = "openshell"). It extends the existing
# sandbox-base image with:
#   - The OpenShell supervisor binary (openshell-sandbox)
#   - The Astonish agent binary
#   - The astonish-boot binary (ENTRYPOINT: overlay composition + pivot_root)
#   - The 'sandbox' user (for OpenShell privilege-drop)
#
# The ENTRYPOINT is astonish-boot, which:
#   1. Composes the overlay filesystem from the layer chain
#   2. Copies infrastructure binaries into the overlay upper
#   3. Performs pivot_root into the composed overlay
#   4. exec's openshell-sandbox as the new PID 1
#
# Build (from repo root):
#   docker build -f docker/sandbox-openshell/Dockerfile \
#     -t schardosin/astonish-sandbox-openshell:latest .
#
# Reference: docs/architecture/openshell-sandbox-backend.md §4.

# ---------------------------------------------------------------------------
# Stage 1 — Build Go binaries (astonish + astonish-boot)
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the main astonish binary (agent/node for tool execution).
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X github.com/schardosin/astonish/cmd/astonish.Version=sandbox-openshell" \
      -o /tmp/astonish \
      .

# Build astonish-boot (ENTRYPOINT: overlay + pivot_root + exec supervisor).
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w" \
      -o /tmp/astonish-boot \
      ./cmd/astonish-boot

# ---------------------------------------------------------------------------
# Stage 2 — Final runtime image
# ---------------------------------------------------------------------------
FROM schardosin/astonish-sandbox-base:latest

LABEL org.opencontainers.image.title="astonish-sandbox-openshell" \
      org.opencontainers.image.description="OpenShell-managed sandbox image for Astonish K8s pods" \
      org.opencontainers.image.source="https://github.com/schardosin/astonish" \
      org.opencontainers.image.licenses="Apache-2.0"

# --- OpenShell supervisor binary ---
# Pulled from the official NVIDIA OpenShell community release image.
# Pin the version tag for reproducible builds; bump when upgrading.
COPY --from=ghcr.io/nvidia/openshell-community/openshell-sandbox:latest \
     /usr/local/bin/openshell-sandbox /opt/openshell/bin/openshell-sandbox

# --- Astonish binary ---
# Installed at /opt/astonish/bin/ (NOT in the overlay path). astonish-boot
# copies it into the overlay upper at /usr/local/bin/astonish on every boot.
COPY --from=builder /tmp/astonish /opt/astonish/bin/astonish

# --- astonish-boot binary ---
COPY --from=builder /tmp/astonish-boot /opt/astonish/bin/astonish-boot

# --- Directory skeleton ---
# /overlay — emptyDir mount for upper/work/merged
# /var/run/openshell — supervisor runtime socket directory
RUN mkdir -p \
      /opt/astonish/bin \
      /opt/openshell/bin \
      /overlay \
      /var/run/openshell && \
    chmod 755 \
      /opt/astonish/bin/astonish \
      /opt/astonish/bin/astonish-boot \
      /opt/openshell/bin/openshell-sandbox

# --- sandbox user ---
# OpenShell's supervisor drops privileges to this user before spawning
# child processes. After pivot_root, /etc/passwd comes from the overlay
# (ultimately from @base). This user is seeded into @base by the seed Job.
RUN useradd -m -d /sandbox -s /bin/bash sandbox

# The ENTRYPOINT replaces the existing sandbox-base entrypoint. Instead
# of the shell-based overlay + sleep, astonish-boot performs overlay
# composition in Go, pivot_roots, and execs the OpenShell supervisor.
ENTRYPOINT ["/opt/astonish/bin/astonish-boot"]
