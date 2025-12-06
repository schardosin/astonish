# Variables
BINARY_NAME = astonish

# Default target
all: build

# Help
help:
	@echo "Usage:"
	@echo "  make build    - Build the Go binary"
	@echo "  make run      - Run the Go application"
	@echo "  make test     - Run Go tests"
	@echo "  make install  - Install the binary to ~/bin"
	@echo "  make clean    - Clean up build artifacts"

# Build the binary
build:
	@echo "Building Go binary..."
	go build -o $(BINARY_NAME) .
	@echo "Go binary built successfully: $(BINARY_NAME)"

# Run the application
run:
	@echo "Running Go application..."
	go run .

# Run tests
test:
	@echo "Running Go tests..."
	go test ./...

# Install to ~/bin
install: build
	@echo "Installing $(BINARY_NAME) to ~/bin..."
	@mkdir -p ~/bin
	cp $(BINARY_NAME) ~/bin/
	@echo "Installed successfully!"

# Clean up build artifacts
clean:
	@echo "Cleaning up build artifacts..."
	rm -rf $(BINARY_NAME)
	@echo "Cleanup complete!"
