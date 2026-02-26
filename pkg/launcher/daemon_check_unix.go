//go:build !windows

package launcher

import (
	"os"
	"syscall"
)

// isProcessRunning checks if a process with the given PID exists by sending signal 0.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
