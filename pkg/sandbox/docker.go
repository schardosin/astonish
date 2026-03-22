package sandbox

// Docker management for the Incus runtime container on macOS/Windows.
// Phase 5 implementation — these are stubs for Phase 1.

// IsIncusDockerContainerRunning checks if the astonish-incus Docker container
// is running. Phase 5 will implement this properly.
func IsIncusDockerContainerRunning() bool {
	// Phase 5: check `docker inspect astonish-incus` for running state
	return false
}

// EnsureIncusDockerContainer pulls the Incus image and creates/starts
// the Docker container that hosts Incus on macOS/Windows.
// Phase 5 will implement this properly.
func EnsureIncusDockerContainer() error {
	return nil
}

// StopIncusDockerContainer stops the astonish-incus Docker container.
// Phase 5 will implement this properly.
func StopIncusDockerContainer() error {
	return nil
}

// UpgradeIncusDockerContainer pulls the latest image and recreates the container
// with the same persistent volume.
// Phase 5 will implement this properly.
func UpgradeIncusDockerContainer() error {
	return nil
}
