# Dockerfile — astonish-sandbox-webhook
#
# MutatingAdmissionWebhook server that injects Astonish PVC volumes,
# FUSE device resources, and CAP_SYS_ADMIN into OpenShell sandbox pods.
#
# Build (from repo root):
#   docker build -f deploy/webhook/Dockerfile -t schardosin/astonish-sandbox-webhook:latest .
#
# Reference: docs/architecture/openshell-sandbox-backend.md §10.

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w" \
      -o /tmp/webhook \
      ./deploy/webhook

# ---------------------------------------------------------------------------
# Runtime image — distroless for minimal attack surface.
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /tmp/webhook /webhook

EXPOSE 8443
ENTRYPOINT ["/webhook"]
