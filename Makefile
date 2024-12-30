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
	python -m build --wheel
	@echo "Wheel built successfully!"

# Install the package
install: build
	@echo "Installing the wheel..."
	pip install $(DIST_DIR)/$(PACKAGE_NAME)-*.whl
	@echo "Package installed successfully!"

# Uninstall the package
uninstall:
	@echo "Uninstalling the package..."
	pip uninstall -y $(PACKAGE_NAME)
	@echo "Package uninstalled successfully!"

# Clean up build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	rm -rf $(DIST_DIR) build *.egg-info
	@echo "Cleanup complete!"

