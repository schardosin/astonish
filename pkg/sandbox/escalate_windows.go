//go:build windows

package sandbox

// NeedsEscalation reports whether the current process needs privilege
// escalation for sandbox operations. On Windows, sandbox uses Docker+Incus
// which handles privileges internally, so escalation is never needed.
func NeedsEscalation() bool {
	return false
}

// Escalate is a no-op on Windows. Sandbox operations use Docker+Incus
// which handles privileges inside the Docker container.
func Escalate() error {
	return nil
}
