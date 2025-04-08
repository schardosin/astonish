# Variables
PACKAGE_NAME = astonish
DIST_DIR = dist

# Default target
all: help

# Help
help:
	@echo "Usage:"
	@echo "  make build       - Build the package as a wheel"
	@echo "  make install     - Install the built wheel"
	@echo "  make uninstall   - Uninstall the package"
	@echo "  make clean       - Clean up build artifacts"

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

# Clean up build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	rm -rf $(DIST_DIR) build *.egg-info
	@echo "Cleanup complete!"

