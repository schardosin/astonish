package tmplmeta

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// SandboxDataDir returns the directory for sandbox data files.
// When running under sudo, resolves the real user's home via SUDO_USER
// so that data files are consistent regardless of whether sudo is used.
func SandboxDataDir() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := RealUserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine home directory: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	return filepath.Join(dataHome, "astonish", "sandbox"), nil
}

// RealUserHomeDir returns the home directory of the real (non-root) user.
// When running under sudo, SUDO_USER identifies the original user and we
// resolve their home directory. This ensures sandbox data files (sessions.json,
// templates.json) are stored in the same location whether the command is run
// with or without sudo.
func RealUserHomeDir() (string, error) {
	if os.Getuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if u, err := user.Lookup(sudoUser); err == nil {
				return u.HomeDir, nil
			}
		}
	}
	return os.UserHomeDir()
}
