//go:build !linux

package sandbox

// setupUnprivilegedOverlay is a no-op on non-Linux platforms.
// UID shifting and idmap pre-seeding are Linux-specific features.
func setupUnprivilegedOverlay(_ *IncusClient, _, _, _ string) error {
	return nil
}

// mountPlainOverlay is a no-op stub on non-Linux platforms.
func mountPlainOverlay(_, _, _ string) error {
	return nil
}

// reshiftOverlayUIDs is a no-op on non-Linux platforms.
func reshiftOverlayUIDs(_ *IncusClient, _, _ string) error {
	return nil
}

// ShiftSnapshotUIDs is a no-op on non-Linux platforms.
func ShiftSnapshotUIDs(_ *IncusClient, _ string) error {
	return nil
}
