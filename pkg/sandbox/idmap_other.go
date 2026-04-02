//go:build !linux

package sandbox

// setupIdmappedOverlay is a no-op on non-Linux platforms.
// Idmapped mounts are a Linux-specific feature.
func setupIdmappedOverlay(_ *IncusClient, _, _, _ string) error {
	return nil
}

// teardownIdmappedLayers is a no-op on non-Linux platforms.
func teardownIdmappedLayers(_ string) {}
