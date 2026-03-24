package sandbox

import (
	"fmt"
	"log"
)

// SandboxStatus holds information about the sandbox runtime environment.
type SandboxStatus struct {
	Platform           Platform
	IncusConnected     bool
	IncusVersion       string
	StorageBackend     string
	OverlayReady       bool
	TemplateCount      int
	SessionCount       int
	OrphanCount        int    // containers in Incus with no registry entry
	DockerContainerUp  bool   // Docker+Incus: whether the Docker container is running
	DockerImageVersion string // Docker+Incus: version label on the Docker container
	DockerNeedsUpgrade bool   // Docker+Incus: version mismatch detected
}

// SetupSandboxRuntime detects the platform and connects to Incus.
// Returns a connected IncusClient or an error with actionable guidance.
//
// On Docker+Incus (macOS/Windows), this also ensures the Docker container
// is running and handles version-based auto-upgrades. The Docker layer
// is fully transparent — the user never interacts with Docker directly.
func SetupSandboxRuntime() (*IncusClient, error) {
	platform, reason := DetectPlatformReason()

	switch platform {
	case PlatformLinuxNative:
		SetActivePlatform(platform)
		client, err := Connect(platform)
		if err != nil {
			return nil, fmt.Errorf("Incus is installed but not reachable: %w\nMake sure the Incus daemon is running: sudo systemctl start incus", err)
		}
		return client, nil

	case PlatformDockerIncus:
		// Ensure the Docker container is running (pulls image, creates
		// container, or starts existing one). Also handles auto-upgrade
		// when the astonish binary version doesn't match the container's label.
		if err := EnsureIncusDockerContainer(); err != nil {
			return nil, fmt.Errorf("failed to set up Docker+Incus runtime: %w\n\n"+
				"Make sure Docker is running and try again.\n"+
				"If this is a fresh install, run: astonish setup", err)
		}

		SetActivePlatform(platform)
		client, err := Connect(platform)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Incus inside Docker: %w", err)
		}

		log.Printf("[sandbox] Connected to Incus via Docker+Incus (TCP localhost:8443)")
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

	// On Docker+Incus, ensure the container is running before connecting
	if platform == PlatformDockerIncus {
		if !IsIncusDockerContainerRunning() {
			fmt.Println("Docker container: not running")
			fmt.Println("Run 'astonish setup' to initialize the sandbox runtime.")
			return nil
		}
		fmt.Println("Docker container: running")
		containerVersion := GetDockerContainerVersion()
		if containerVersion != "" {
			fmt.Printf("Container version: %s\n", containerVersion)
		}
		if NeedsUpgrade() {
			fmt.Println("Upgrade needed:  yes (version mismatch)")
		}
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

	// Docker+Incus specific status
	if client.platform == PlatformDockerIncus {
		status.DockerContainerUp = IsIncusDockerContainerRunning()
		status.DockerImageVersion = GetDockerContainerVersion()
		status.DockerNeedsUpgrade = NeedsUpgrade()
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

	// Count orphan containers (exist in Incus but not in registry)
	sessionContainers, err := client.ListSessionContainers()
	if err == nil {
		registeredNames := make(map[string]bool)
		for _, entry := range sessRegistry.List() {
			registeredNames[entry.ContainerName] = true
		}
		for _, inst := range sessionContainers {
			if !registeredNames[inst.Name] {
				status.OrphanCount++
			}
		}
	}

	return status, nil
}
