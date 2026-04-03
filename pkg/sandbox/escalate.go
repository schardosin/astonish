//go:build !windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// NeedsEscalation reports whether the current process needs privilege
// escalation for sandbox operations. Returns true only on Linux when
// the process is not running as root.
//
// On macOS/Windows, all privileged operations are delegated to the
// Docker+Incus container, so no host-level escalation is needed.
func NeedsEscalation() bool {
	return runtime.GOOS == "linux" && os.Getuid() != 0
}

// Escalate re-executes the current binary via sudo, replacing the
// current process. The user sees a standard sudo password prompt.
// On success this function does not return (the process is replaced).
// On failure it returns an error.
//
// sudo sets SUDO_USER automatically, which the codebase already uses
// to resolve the real user's HOME directory for data files.
func Escalate() error {
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sandbox requires root privileges on Linux but 'sudo' is not available.\n" +
			"Install sudo or run as root")
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	// Build argv: sudo <binary> <original args...>
	// os.Args[0] is replaced with the resolved absolute path so sudo
	// can find the binary regardless of PATH or relative invocation.
	argv := make([]string, 0, len(os.Args)+1)
	argv = append(argv, "sudo", exePath)
	argv = append(argv, os.Args[1:]...)

	fmt.Println("Sandbox requires elevated privileges for container management.")

	// Replace the current process with sudo. stdin/stdout/stderr are
	// inherited automatically, so the sudo password prompt works as
	// expected and the escalated process takes over the terminal.
	return syscall.Exec(sudoPath, argv, os.Environ())
}
