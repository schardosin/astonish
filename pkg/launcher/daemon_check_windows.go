package launcher

// isProcessRunning on Windows always returns false.
// The daemon is only supported on Unix platforms.
func isProcessRunning(pid int) bool {
	return false
}
