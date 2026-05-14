# Dockerfile — astonish-sandbox-base image.
#
# This is the container image every Astonish sandbox pod runs in when
# the K8s+Sysbox backend is active (SandboxConfig.Backend = "k8s"). It
# is NOT the application image (see top-level Dockerfile for that);
# it's a minimal userland that serves as PID 1 inside the user
# namespace Sysbox creates for each sandbox.
#
# The image contains, and is RESTRICTED to:
#
#   1. /usr/local/bin/astonish-sandbox-entrypoint
#        The POSIX-shell script whose source of truth is
#        pkg/sandbox/k8s.EntrypointScript. Generated at build time via
#        cmd/astonish-sandbox-entrypoint-script so there is no risk of
#        drift between the backend's expectations (env vars, mount
#        paths) and the runtime.
#
#   2. Base userland: busybox coreutils, tar, zstd (for the resume path
#        in §5.14 that streams upper layers back from the uppers PVC),
#        and kernel overlayfs helpers. Sysbox provides the user-namespace
#        isolation and CAP_SYS_ADMIN inside that namespace, so mount(2)
#        works from userland without hostPath tricks.
#
#   3. A tiny /sandbox skeleton where the overlay will be composed:
#        /sandbox/rootfs  — overlay mount point (created by the entrypoint)
#        /var/astonish/   — emptyDir mount roots (upper, work)
#        /mnt/astonish-layers, /mnt/astonish-uppers — PVC mount roots
#
# What this image INTENTIONALLY does NOT contain:
#
#   - The Astonish daemon binary. The daemon connects to the pod from
#     outside via the exec subresource; the entrypoint merely sleeps
#     after composing the overlay and awaits exec sessions
#     (§5.3 step 3). We keep this minimal so base-image updates don't
#     require rebuilding with every Astonish release.
#
#   - Any language runtimes, shells beyond /bin/sh, or user tooling.
#     Those belong in layers composed ON TOP of this base (the @base
#     layer plus org-wide and template-specific layers on CephFS).
#
# Build:
#   docker build -f docker/sandbox-base/Dockerfile -t schardosin/astonish-sandbox-base:latest .
#
# Publish:
#   The image tag MUST match SandboxKubernetesConfig.SandboxImage's
#   default (pkg/config/app_config.go). If that default changes, bump
#   the tag here and in deploy/k8s/ manifests in lockstep.
#
# Reference: docs/architecture/sandbox-backends.md §§5.3, 5.14.

# ---------------------------------------------------------------------------
# Stage 1 — generate the entrypoint script with the Go helper.
#
# We use the same Go toolchain version as the main Dockerfile so the
# generated script is bit-identical across builds (EntrypointScript is
# deterministic, but keeping toolchains aligned avoids future footguns
# like go fmt / string-builder behavioural drift).
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS entrypoint-builder

WORKDIR /src

# Copy just the module manifests first so the dependency cache is
# reused across source edits.
COPY go.mod go.sum ./
RUN go mod download

# Only the files the entrypoint-script command transitively needs.
# Copying the whole tree would be correct but wasteful; limiting to
# the relevant packages keeps the build cache tight.
COPY cmd/astonish-sandbox-entrypoint-script ./cmd/astonish-sandbox-entrypoint-script
COPY pkg/sandbox/k8s ./pkg/sandbox/k8s
COPY pkg/sandbox ./pkg/sandbox
COPY pkg ./pkg

RUN CGO_ENABLED=0 go run ./cmd/astonish-sandbox-entrypoint-script \
      > /tmp/astonish-sandbox-entrypoint && \
    chmod +x /tmp/astonish-sandbox-entrypoint && \
    # Sanity check — the file is non-empty and starts with the
    # POSIX shebang; a broken generator is a build-time failure, not a
    # runtime surprise in the cluster.
    head -1 /tmp/astonish-sandbox-entrypoint | grep -q '^#!/bin/sh'

# ---------------------------------------------------------------------------
# Stage 2 — final runtime image.
#
# Debian slim is chosen over Alpine because Sysbox + kernel overlayfs
# works more predictably against glibc in the wild; the resume path
# also uses zstd which is packaged trivially on Debian. The image stays
# under ~40 MiB after apt-get clean.
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim

# Labels make kubectl describe / docker inspect self-explanatory and
# give image-signing tools something stable to chew on.
LABEL org.opencontainers.image.title="astonish-sandbox-base" \
      org.opencontainers.image.description="Minimal base image for Astonish K8s+Sysbox sandbox pods" \
      org.opencontainers.image.source="https://github.com/schardosin/astonish" \
      org.opencontainers.image.licenses="Apache-2.0"

ENV DEBIAN_FRONTEND=noninteractive

# Userland required by the entrypoint script:
#   - ca-certificates: workloads often fetch HTTPS resources
#   - tar + zstd: resume-tar extraction in §5.14
#   - coreutils: mkdir, echo, tee, etc.
#   - busybox-static: fallback tooling when a layer ships a minimal
#       rootfs without GNU utilities
#   - mount (util-linux): the entrypoint calls mount(2) via /bin/mount
# We remove apt lists after install to keep the image small; users who
# need additional packages should compose them into higher layers.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        tar \
        zstd \
        coreutils \
        busybox-static \
        mount \
        util-linux \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Directory skeleton the entrypoint and pod manifest expect. Creating
# them here (instead of relying on the kubelet to mkdir emptyDirs)
# makes the contract explicit and surfaces misconfigurations at image-
# build time rather than at first pod start.
RUN mkdir -p \
      /sandbox/rootfs \
      /var/astonish/upper \
      /var/astonish/work \
      /mnt/astonish-layers \
      /mnt/astonish-uppers \
    && chmod 0755 /sandbox /sandbox/rootfs /var/astonish

# Pull the generated entrypoint from the builder stage. It is the ONLY
# artefact we carry forward — everything else lives in the base apt
# footprint. Keeping the COPY at the end of the Dockerfile maximises
# cache hits when iterating on the Go source.
COPY --from=entrypoint-builder \
     /tmp/astonish-sandbox-entrypoint \
     /usr/local/bin/astonish-sandbox-entrypoint

# Default to the entrypoint; the pod manifest can override via command/
# args if a particular test needs to skip overlay composition. In
# normal operation, Sysbox starts PID 1 here which sets up the overlay
# then execs into the workload via chroot (§5.3 step 3).
ENTRYPOINT ["/usr/local/bin/astonish-sandbox-entrypoint"]
