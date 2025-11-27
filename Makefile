# Variables
PACKAGE_NAME = astonish
DIST_DIR = dist
BINARY_NAME = astonish_bin

# Default target
all: help

# Help
help:
	@echo "Usage:"
	@echo "  Python targets:"
	@echo "    make build       - Build the package as a wheel"
	@echo "    make install     - Install the built wheel"
	@echo "    make installdev  - Install in editable mode"
	@echo "    make uninstall   - Uninstall the package"
	@echo "  Go targets:"
	@echo "    make go-build    - Build the Go binary"
	@echo "    make go-run      - Run the Go application"
	@echo "    make go-test     - Run Go tests"
	@echo "  General:"
	@echo "    make clean       - Clean up build artifacts"

# Build the wheel
build: clean
	@echo "Building the wheel..."
	python3 -m build --wheel
	@echo "Wheel built successfully!"

installdev: uninstall
	@echo "Installing in editable mode..."
	pip3 install -e .
	@echo "Package installed successfully!"

# Install the package
install: build uninstall
	@echo "Installing the wheel..."
	pip3 install $(DIST_DIR)/$(PACKAGE_NAME)-*.whl
	@echo "Package installed successfully!"

# Uninstall the package
uninstall:
	@echo "Uninstalling the package..."
	pip3 uninstall -y $(PACKAGE_NAME)
	@echo "Package uninstalled successfully!"

# Go targets
go-build:
	@echo "Building Go binary..."
	go build -o $(BINARY_NAME) .
	@echo "Go binary built successfully: $(BINARY_NAME)"

go-run:
	@echo "Running Go application..."
	go run .

go-test:
	@echo "Running Go tests..."
	go test ./...

# Clean up build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	rm -rf $(DIST_DIR) build *.egg-info $(BINARY_NAME)
	@echo "Cleanup complete!"
