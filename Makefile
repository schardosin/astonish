# Variables
BINARY_NAME = astonish
WEB_DIR = web

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

# Build the Go binary only
build:
	@echo "Building Go binary..."
	go build -o $(BINARY_NAME) .
	@echo "Go binary built successfully: $(BINARY_NAME)"

# Build the React UI
build-ui:
	@echo "Building React UI..."
	cd $(WEB_DIR) && npm install && npm run build
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

.PHONY: all help build build-ui build-all run studio studio-dev test install clean update-mcp-stars setup-hooks

