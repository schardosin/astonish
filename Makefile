# Variables
BINARY_NAME = astonish
WEB_DIR = web
VERSION ?= dev
DOCKER_REGISTRY ?= schardosin
DEV_TAG ?= dev

# Default target
all: build-all

# Help
help:
	@echo "Usage:"
	@echo "  make build           - Build the Go binary only"
	@echo "  make build-ui        - Build the React UI (web/dist)"
	@echo "  make build-all       - Build UI first, then Go binary"
	@echo "  make run             - Run the Go application"
	@echo "  make studio          - Run Astonish Studio (dev mode)"
	@echo "  make studio-dev      - Run Studio with live UI reload"
	@echo "  make test            - Run Go tests"
	@echo "  make install         - Install the binary to ~/bin"
	@echo "  make clean           - Clean up build artifacts"
	@echo "  make update-mcp-stars - Update MCP store GitHub star counts"
	@echo ""
	@echo "E2E Testing (Docker):"
	@echo "  make e2e-up          - Start isolated test environment"
	@echo "  make e2e-down        - Stop test environment"
	@echo "  make e2e-rebuild     - Rebuild and restart test environment"
	@echo ""
	@echo "Kubernetes Platform Init:"
	@echo "  make platform-init PLATFORM_HOST=<host> PLATFORM_USER=<user> PLATFORM_PASSWORD=<pass>"
	@echo "                       - Run 'astonish platform init' inside the cluster"
	@echo "  make create-secrets K8S_NAMESPACE=<ns> PLATFORM_DSN=<dsn>"
	@echo "                       - Create astonish-secrets (master-key, jwt-secret, platform-dsn)"
	@echo ""
	@echo "Docker Production (Persistent Data):"
	@echo "  make docker-up       - Start persistent container (maps ./.astonish-data)"
	@echo "  make docker-down     - Stop persistent container"
	@echo "  make docker-rebuild  - Rebuild and restart persistent container"
	@echo ""
	@echo "Sandbox (Docker+Incus for macOS/Windows):"
	@echo "  make build-linux       - Cross-compile Linux amd64 binary"
	@echo "  make build-linux-arm64 - Cross-compile Linux arm64 binary"
	@echo "  make docker-incus      - Build the Incus Docker image (for CI release)"
	@echo ""
	@echo "Registry Push (multi-arch, requires docker login + buildx):"
	@echo "  make push-dev              - Build+push astonish:dev (multi-arch)"
	@echo "  make push-incus-dev        - Build+push astonish-incus:dev (multi-arch)"
	@echo "  make push-sandbox-base-dev - Build+push astonish-sandbox-base:dev (multi-arch)"
	@echo "  make push-all-dev          - Push all dev images"
	@echo ""
	@echo "Registry Push (fast single-arch, dev iteration):"
	@echo "  make push-dev-fast              - Build+push astonish:dev (native arch only)"
	@echo "  make push-sandbox-base-dev-fast - Build+push sandbox-base:dev (native arch only)"
	@echo "  make push-incus-dev-fast        - Build+push incus:dev (native arch only)"
	@echo "  make push-all-dev-fast          - Push all dev images (native arch only)"

# Build the Go binary only
build:
	@echo "Building Go binary..."
	go build -o $(BINARY_NAME) .
	@echo "Go binary built successfully: $(BINARY_NAME)"

# Build the React UI
build-ui:
	@echo "Building React UI..."
	@rm -rf $(WEB_DIR)/dist
	cd $(WEB_DIR) && npm install && npm run build
	@touch $(WEB_DIR)/embed.go
	@echo "React UI built successfully: $(WEB_DIR)/dist"

# Build everything: UI first, then Go binary
build-all: setup-hooks build-ui build
	@echo "Full build complete!"

# Setup git hooks (runs automatically on first build)
setup-hooks:
	@if [ ! -f .git/hooks/pre-commit ]; then \
		echo "Installing git hooks..."; \
		cp .githooks/pre-commit .git/hooks/pre-commit; \
		chmod +x .git/hooks/pre-commit; \
		echo "✓ Git hooks installed"; \
	fi

# Run the application
run:
	@echo "Running Go application..."
	go run .

# Run Astonish Studio (production mode - serves built UI)
studio: build-ui
	@echo "Starting Astonish Studio..."
	go run . studio

# Run Studio in dev mode (Go backend + Vite dev server)
studio-dev:
	@echo "Starting Astonish Studio (dev mode)..."
	@echo "  Backend: http://localhost:9393"
	@echo "  Frontend: http://localhost:5173"
	@echo ""
	@echo "Run 'cd web && npm run dev' in another terminal for live UI reload"
	go run . studio

# Run tests
test:
	@echo "Running Go tests..."
	go test ./...

# Install to ~/bin
install: build-all
	@echo "Installing $(BINARY_NAME) to ~/bin..."
	@mkdir -p ~/bin
	cp $(BINARY_NAME) ~/bin/
	@echo "Installed successfully!"

# Clean up build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	rm -rf $(BINARY_NAME)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(WEB_DIR)/node_modules
	@echo "Cleanup complete!"

# Update MCP store star counts from GitHub
update-mcp-stars:
	@echo "Updating MCP server star counts..."
	GITHUB_TOKEN=$$(gh auth token) python3 scripts/update-mcp-stars.py
	@echo "Star counts updated!"

.PHONY: all help build build-ui build-all run studio studio-dev test install clean update-mcp-stars setup-hooks platform-init create-secrets e2e-up e2e-down e2e-rebuild docker-up docker-down docker-rebuild build-linux build-linux-arm64 docker-incus ensure-builder push-dev push-incus-dev push-all-dev

# E2E Testing - Docker-based isolated environment
e2e-up:
	@echo "Starting isolated test environment..."
	docker compose -f docker-compose.e2e.yml up -d --build
	@echo "Astonish running at http://localhost:9393"

e2e-down:
	@echo "Stopping test environment..."
	docker compose -f docker-compose.e2e.yml down
	@echo "Test environment stopped."

e2e-rebuild:
	@echo "Rebuilding test environment..."
	docker compose -f docker-compose.e2e.yml down
	docker compose -f docker-compose.e2e.yml up -d --build
	docker compose -f docker-compose.e2e.yml up -d --build
	@echo "Astonish running at http://localhost:9393"

# Docker Production - Persistent environment
docker-up:
	@echo "Starting persistent container..."
	docker compose up -d --build
	@echo "Astonish running at http://localhost:9393"
	@echo "Data stored in ./.astonish-data"

docker-down:
	@echo "Stopping persistent container..."
	docker compose down
	@echo "Container stopped."

docker-rebuild:
	@echo "Rebuilding persistent container..."
	docker compose down
	docker compose up -d --build
	@echo "Astonish running at http://localhost:9393"

# Platform init via Kubernetes (run inside cluster)
platform-init:
ifndef PLATFORM_HOST
	$(error PLATFORM_HOST is required. Usage: make platform-init PLATFORM_HOST=... PLATFORM_USER=... PLATFORM_PASSWORD=...)
endif
ifndef PLATFORM_USER
	$(error PLATFORM_USER is required. Usage: make platform-init PLATFORM_HOST=... PLATFORM_USER=... PLATFORM_PASSWORD=...)
endif
ifndef PLATFORM_PASSWORD
	$(error PLATFORM_PASSWORD is required. Usage: make platform-init PLATFORM_HOST=... PLATFORM_USER=... PLATFORM_PASSWORD=...)
endif
	kubectl run astonish-platform-init --rm -it --restart=Never \
	  -n astonish --image=$(DOCKER_REGISTRY)/astonish:$(DEV_TAG) --image-pull-policy=Always \
	  --command -- astonish platform init \
	  --host $(PLATFORM_HOST) --user $(PLATFORM_USER) --password $(PLATFORM_PASSWORD)

# Create Kubernetes secrets for Astonish
create-secrets:
ifndef K8S_NAMESPACE
	$(error K8S_NAMESPACE is required. Usage: make create-secrets K8S_NAMESPACE=... PLATFORM_DSN=...)
endif
ifndef PLATFORM_DSN
	$(error PLATFORM_DSN is required. Usage: make create-secrets K8S_NAMESPACE=... PLATFORM_DSN=...)
endif
	kubectl -n $(K8S_NAMESPACE) create secret generic astonish-secrets \
	  --from-literal=master-key="$$(openssl rand -hex 32)" \
	  --from-literal=jwt-secret="$$(openssl rand -hex 32)" \
	  --from-literal=platform-dsn="$(PLATFORM_DSN)"

# Cross-compile Linux binary (for macOS/Windows dev pushing into sandbox containers)
build-linux:
	@echo "Cross-compiling Linux amd64 binary..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o astonish-linux-amd64 .
	@echo "Linux binary built: astonish-linux-amd64"

build-linux-arm64:
	@echo "Cross-compiling Linux arm64 binary..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o astonish-linux-arm64 .
	@echo "Linux binary built: astonish-linux-arm64"

# Build the Incus Docker image (for CI release pipeline)
# Requires: astonish-linux-amd64 binary to exist (run build-linux first)
docker-incus: build-linux
	@echo "Building Incus Docker image..."
	docker build -f docker/incus/Dockerfile -t schardosin/astonish-incus:$(VERSION) .
	@echo "Image built: schardosin/astonish-incus:$(VERSION)"

# --- Registry Push (multi-arch via buildx) ---

# Ensure a buildx builder with multi-platform support exists
ensure-builder:
	@docker buildx inspect astonish-builder >/dev/null 2>&1 || \
		docker buildx create --name astonish-builder --use --driver docker-container --bootstrap
	@docker buildx use astonish-builder

# Build and push the main Astonish image (multi-arch)
push-dev: ensure-builder
	@echo "Building and pushing $(DOCKER_REGISTRY)/astonish:$(DEV_TAG) (linux/amd64,linux/arm64)..."
	docker buildx build --platform linux/amd64,linux/arm64 \
		-f docker/astonish/Dockerfile \
		--build-arg VERSION=$(DEV_TAG) \
		-t $(DOCKER_REGISTRY)/astonish:$(DEV_TAG) \
		--push .
	@echo "Pushed: $(DOCKER_REGISTRY)/astonish:$(DEV_TAG)"

# Build and push the Incus image (multi-arch)
# Requires local cross-compiled binaries for both architectures
push-incus-dev: ensure-builder build-linux build-linux-arm64
	@echo "Building and pushing $(DOCKER_REGISTRY)/astonish-incus:$(DEV_TAG) (linux/amd64,linux/arm64)..."
	docker buildx build --platform linux/amd64,linux/arm64 \
		-f docker/incus/Dockerfile \
		-t $(DOCKER_REGISTRY)/astonish-incus:$(DEV_TAG) \
		--push .
	@echo "Pushed: $(DOCKER_REGISTRY)/astonish-incus:$(DEV_TAG)"

# Build and push the sandbox base image (multi-arch)
# Requires web/dist/ to exist (run `cd web && npm ci && npm run build` first)
push-sandbox-base-dev: ensure-builder
	@echo "Building and pushing $(DOCKER_REGISTRY)/astonish-sandbox-base:$(DEV_TAG) (linux/amd64,linux/arm64)..."
	docker buildx build --platform linux/amd64,linux/arm64 \
		-f docker/sandbox-base/Dockerfile \
		-t $(DOCKER_REGISTRY)/astonish-sandbox-base:$(DEV_TAG) \
		--push .
	@echo "Pushed: $(DOCKER_REGISTRY)/astonish-sandbox-base:$(DEV_TAG)"

# Push all dev images
push-all-dev: push-dev push-incus-dev push-sandbox-base-dev
	@echo "All dev images pushed successfully!"

# --- Fast single-arch dev builds (native architecture only) ---
# These skip arm64 cross-compilation for faster iteration during development.
# They detect the host architecture automatically.

DEV_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Fast: build and push main Astonish API image (single arch)
push-dev-fast: ensure-builder
	@echo "Building and pushing $(DOCKER_REGISTRY)/astonish:$(DEV_TAG) (linux/$(DEV_ARCH) only)..."
	docker buildx build --platform linux/$(DEV_ARCH) \
		-f docker/astonish/Dockerfile \
		--build-arg VERSION=$(DEV_TAG) \
		-t $(DOCKER_REGISTRY)/astonish:$(DEV_TAG) \
		--push .
	@echo "Pushed: $(DOCKER_REGISTRY)/astonish:$(DEV_TAG)"

# Fast: build and push sandbox base image (single arch)
push-sandbox-base-dev-fast: ensure-builder
	@echo "Building and pushing $(DOCKER_REGISTRY)/astonish-sandbox-base:$(DEV_TAG) (linux/$(DEV_ARCH) only)..."
	docker buildx build --platform linux/$(DEV_ARCH) \
		-f docker/sandbox-base/Dockerfile \
		-t $(DOCKER_REGISTRY)/astonish-sandbox-base:$(DEV_TAG) \
		--push .
	@echo "Pushed: $(DOCKER_REGISTRY)/astonish-sandbox-base:$(DEV_TAG)"

# Fast: build and push Incus image (single arch)
push-incus-dev-fast: ensure-builder build-linux
	@echo "Building and pushing $(DOCKER_REGISTRY)/astonish-incus:$(DEV_TAG) (linux/$(DEV_ARCH) only)..."
	docker buildx build --platform linux/$(DEV_ARCH) \
		-f docker/incus/Dockerfile \
		-t $(DOCKER_REGISTRY)/astonish-incus:$(DEV_TAG) \
		--push .
	@echo "Pushed: $(DOCKER_REGISTRY)/astonish-incus:$(DEV_TAG)"

# Fast: push all dev images (single arch)
push-all-dev-fast: push-dev-fast push-sandbox-base-dev-fast push-incus-dev-fast
	@echo "All dev images pushed ($(DEV_ARCH) only)!"


