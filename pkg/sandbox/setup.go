package sandbox

import (
	"fmt"
)

// SandboxStatus holds information about the sandbox runtime environment.
type SandboxStatus struct {
	Platform       Platform
	IncusConnected bool
	IncusVersion   string
	StorageBackend string
	OverlayReady   bool
	TemplateCount  int
	SessionCount   int
}

// SetupSandboxRuntime detects the platform and connects to Incus.
// Returns a connected IncusClient or an error with actionable guidance.
func SetupSandboxRuntime() (*IncusClient, error) {
	platform, reason := DetectPlatformReason()

	switch platform {
	case PlatformLinuxNative:
		client, err := Connect(platform)
		if err != nil {
			return nil, fmt.Errorf("Incus is installed but not reachable: %w\nMake sure the Incus daemon is running: sudo systemctl start incus", err)
		}
		return client, nil

	case PlatformDockerIncus:
		// Check if the Incus Docker container is running
		if !IsIncusDockerContainerRunning() {
			return nil, fmt.Errorf("Incus Docker container is not running.\nRun 'astonish sandbox init' to set up the container runtime")
		}

		client, err := Connect(platform)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Incus inside Docker: %w", err)
		}
		return client, nil

	default:
		return nil, fmt.Errorf("sandbox is enabled but the container runtime is not available.\n%s", reason)
	}
}

// ValidateEnvironment checks that all prerequisites for sandbox operation are met.
func ValidateEnvironment() error {
	platform := DetectPlatform()

	if platform == PlatformUnsupported {
		return fmt.Errorf("no container runtime available.\nLinux: install Incus (apt install incus)\nmacOS/Windows: install Docker (any Docker-compatible runtime)")
	}

	// Try connecting
	client, err := Connect(platform)
	if err != nil {
		return fmt.Errorf("container runtime detected but connection failed: %w", err)
	}

	// Check server info
	server, err := client.GetServerInfo()
	if err != nil {
		return fmt.Errorf("failed to get Incus server info: %w", err)
	}

	fmt.Printf("Incus version: %s\n", server.Environment.ServerVersion)

	// Check storage backend
	backend, err := client.GetStorageBackend()
	if err != nil {
		fmt.Printf("Warning: could not determine storage backend: %v\n", err)
	} else {
		fmt.Printf("Storage backend: %s\n", backend)
	}

	// Check overlay image
	_, _, aliasErr := client.Server().GetImageAlias(OverlayImageAlias)
	if aliasErr == nil {
		fmt.Println("Overlay image:  ready (instant session creation)")
	} else {
		fmt.Println("Overlay image:  not found (run 'astonish sandbox init')")
	}

	return nil
}

// Status returns the current sandbox runtime status.
func Status(client *IncusClient, tplRegistry *TemplateRegistry, sessRegistry *SessionRegistry) (*SandboxStatus, error) {
	status := &SandboxStatus{
		Platform:       client.platform,
		IncusConnected: true,
	}

	// Incus version
	server, err := client.GetServerInfo()
	if err == nil {
		status.IncusVersion = server.Environment.ServerVersion
	}

	// Storage backend
	backend, err := client.GetStorageBackend()
	if err == nil {
		status.StorageBackend = backend
	}

	// Overlay image readiness
	_, _, aliasErr := client.Server().GetImageAlias(OverlayImageAlias)
	status.OverlayReady = aliasErr == nil

	// Auto-reap stale registry entries before counting
	sessRegistry.Reap(client)

	// Counts
	status.TemplateCount = len(tplRegistry.List())
	status.SessionCount = len(sessRegistry.List())

	return status, nil
}
