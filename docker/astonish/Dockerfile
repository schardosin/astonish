# Astonish Docker image
#
# Go binaries are cross-compiled on the host (no QEMU needed) and copied in.
# This avoids Go 1.26 segfaults when running `go mod download` under QEMU
# user-mode emulation on Apple Silicon Macs.
#
# Base must be glibc (Debian), not Alpine/musl: pkg/codeintel uses purego.Dlopen,
# which produces a dynamically linked binary needing /lib64/ld-linux-*.so.2.
#
# Build (multi-arch, from Makefile):
#   make push-dev   # cross-compiles both arch binaries, then buildx pushes
#
# Build (single-arch, local dev):
#   make push-dev-fast

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

# Install certificates, Node.js (includes npx), Python3, uv (includes uvx), and git
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        nodejs \
        npm \
        python3 \
        python3-pip \
        git \
    && pip3 install uv --break-system-packages \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the pre-built Linux astonish binary for the target architecture.
# TARGETARCH is set automatically by docker buildx (amd64 or arm64).
# Do not give TARGETARCH a default — BuildKit overrides are ignored when a
# default is set, which previously shipped an amd64 binary in the arm64 image.
ARG TARGETARCH
COPY astonish-linux-${TARGETARCH} /usr/local/bin/astonish
RUN chmod +x /usr/local/bin/astonish

# Expose default port
EXPOSE 9393

# Set default entrypoint
ENTRYPOINT ["astonish"]
CMD ["daemon", "run"]
