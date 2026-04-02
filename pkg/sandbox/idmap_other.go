//go:build !linux

package sandbox

// setupIdmappedOverlay is a no-op on non-Linux platforms.
// Idmapped mounts are a Linux-specific feature.
func setupIdmappedOverlay(_ *IncusClient, _, _, _ string) error {
	return nil
}

// teardownIdmappedLayers is a no-op on non-Linux platforms.
func teardownIdmappedLayers(_ string) {}

// idmapMountsDir returns the idmap mounts directory path for a container.
// On non-Linux platforms this is never used for actual mounts but is needed
// for compilation of shared code in overlay.go.
func idmapMountsDir(containerName string) string {
	return ""
}

// mountIdmappedOverlay is a no-op on non-Linux platforms.
func mountIdmappedOverlay(_ *IncusClient, _, _, _ string) error {
	return nil
}
