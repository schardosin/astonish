# Dockerfile — astonish-sandbox-openshell image.
#
# Custom sandbox image for Astonish sessions running on the OpenShell backend.
# Based on NVIDIA's official base sandbox image, with Astonish agent tooling
# added on top.
#
# The OpenShell gateway handles:
#   - Supervisor sideloading (via init-container or ImageVolume)
#   - Workspace PVC provisioning (via Agent Sandbox CRD)
#   - mTLS and policy enforcement
#
# This image just needs the tools the agent process uses inside the sandbox.
#
# Build (from repo root):
#   docker build -f docker/sandbox-openshell/Dockerfile \
#     -t schardosin/astonish-sandbox-openshell:latest .
#
# Configure in OpenShell gateway values:
#   server:
#     sandboxImage: schardosin/astonish-sandbox-openshell:latest

# ---------------------------------------------------------------------------
# Stage 1 — Build the Astonish agent binary
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

# ---------------------------------------------------------------------------
# Stage 2 — Final runtime image
# ---------------------------------------------------------------------------
FROM ghcr.io/nvidia/openshell-community/sandboxes/base:latest

LABEL org.opencontainers.image.title="astonish-sandbox-openshell" \
      org.opencontainers.image.description="Astonish agent sandbox image for NVIDIA OpenShell" \
      org.opencontainers.image.source="https://github.com/schardosin/astonish" \
      org.opencontainers.image.licenses="Apache-2.0"

# --- Astonish agent binary ---
COPY --from=builder /tmp/astonish /usr/local/bin/astonish

# The workspace directory. OpenShell mounts a PVC here via the Agent Sandbox
# CRD's volumeClaimTemplates.
WORKDIR /sandbox
