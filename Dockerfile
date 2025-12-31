# Multi-stage build for Astonish
# Provides a clean, standalone container

# Stage 1: Build web UI first
FROM node:20-alpine AS web-builder

WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
# Remove any old dist versions and build fresh
RUN rm -rf ./dist && npm run build

# Stage 2: Build Go binary with embedded UI
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go modules first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Remove any local web/dist (may have multiple old versions)
# Then copy the fresh build from web-builder
RUN rm -rf ./web/dist
COPY --from=web-builder /app/web/dist ./web/dist

# Build with embedded UI (CGO disabled for static binary)
RUN CGO_ENABLED=0 go build -o astonish .

# Stage 3: Final minimal image
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
CMD ["studio"]
