//go:build windows

package daemon

// IsProcessRunning checks if a process with the given PID exists.
// The daemon service is not supported on Windows (use WSL2 instead),
// so this is a stub that always returns false.
func IsProcessRunning(_ int) bool {
	return false
}
