package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Service manages the daemon's lifecycle as a system service.
type Service interface {
	// Install registers the daemon as a system service (launchd/systemd).
	Install(cfg InstallConfig) error
	// Uninstall removes the system service registration.
	Uninstall() error
	// Start starts the installed service.
	Start() error
	// Stop stops the running service.
	Stop() error
	// Restart stops then starts the service.
	Restart() error
	// IsRunning checks whether the daemon process is running.
	IsRunning() (bool, error)
	// Status returns detailed status information.
	Status() (*ServiceStatus, error)
	// Label returns the service identifier (e.g., "com.astonish.daemon").
	Label() string
	// LogPath returns the path to the daemon log file.
	LogPath() string
}

// InstallConfig holds the parameters needed to install the daemon as a service.
type InstallConfig struct {
	BinaryPath string            // Absolute path to the astonish binary
	Port       int               // HTTP port for the Studio server
	LogDir     string            // Directory for log files
	EnvVars    map[string]string // Extra environment variables to set
}

// ServiceStatus contains the current state of the daemon service.
type ServiceStatus struct {
	Running   bool
	PID       int
	StartedAt time.Time
	LogPath   string
}

// NewService returns the platform-appropriate Service implementation.
func NewService() (Service, error) {
	return newPlatformService()
}

// DefaultLogDir returns the default log directory (~/.config/astonish/logs/).
func DefaultLogDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "logs"), nil
}

// DefaultPIDPath returns the default PID file path (~/.config/astonish/daemon.pid).
func DefaultPIDPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "daemon.pid"), nil
}

// EnsureDir creates a directory if it doesn't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// WritePID writes the current process ID to the PID file.
func WritePID(path string) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

// RemovePID removes the PID file.
func RemovePID(path string) {
	os.Remove(path)
}

// ReadPID reads the PID from the PID file. Returns 0 if the file doesn't exist.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %w", err)
	}
	return pid, nil
}

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
