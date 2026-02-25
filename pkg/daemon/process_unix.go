//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// IsProcessRunning checks if a process with the given PID exists.
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check if process is alive.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
