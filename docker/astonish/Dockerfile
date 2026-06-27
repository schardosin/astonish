# Astonish Docker image
#
# Go binaries are cross-compiled on the host (no QEMU needed) and copied in.
# This avoids Go 1.26 segfaults when running `go mod download` under QEMU
# user-mode emulation on Apple Silicon Macs.
#
# Build (multi-arch, from Makefile):
#   make push-dev   # cross-compiles both arch binaries, then buildx pushes
#
# Build (single-arch, local dev):
#   make push-dev-fast

FROM alpine:3.19

# Install certificates, Node.js (includes npx), Python3, uv (includes uvx), and git
RUN apk add --no-cache ca-certificates nodejs npm python3 py3-pip git && \
    pip3 install uv --break-system-packages

WORKDIR /app

# Copy the pre-built Linux astonish binary for the target architecture.
# TARGETARCH is set automatically by docker buildx (amd64 or arm64).
ARG TARGETARCH=amd64
COPY astonish-linux-${TARGETARCH} /usr/local/bin/astonish
RUN chmod +x /usr/local/bin/astonish

# Expose default port
EXPOSE 9393

# Set default entrypoint
ENTRYPOINT ["astonish"]
CMD ["daemon", "run"]
