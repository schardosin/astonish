# Multi-stage build for Astonish
# Web frontend is built natively on the CI runner (before Docker build)
# to avoid slow/hanging Vite builds under QEMU emulation for arm64.
# The pre-built web/dist is included via the build context.

# Stage 1: Build Go binary with embedded UI
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go modules first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code (includes pre-built web/dist from native runner build)
COPY . .

# Build with embedded UI (CGO disabled for static binary)
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/schardosin/astonish/cmd/astonish.Version=${VERSION}" -o astonish .

# Stage 2: Final minimal image
FROM alpine:3.19

# Install certificates, Node.js (includes npx), Python3, uv (includes uvx), and git
RUN apk add --no-cache ca-certificates nodejs npm python3 py3-pip git && \
    pip3 install uv --break-system-packages

WORKDIR /app

# Copy built binary (UI is embedded)
COPY --from=builder /app/astonish /usr/local/bin/

# Expose default port
EXPOSE 9393

# Set default entrypoint
ENTRYPOINT ["astonish"]
CMD ["daemon", "run"]
