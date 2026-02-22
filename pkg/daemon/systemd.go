//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	systemdServiceName = "astonish-daemon"
	systemdLabel       = "astonish-daemon.service"
)

type systemdService struct {
	unitPath string
	logDir   string
}

func newPlatformService() (Service, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}
	unitPath := filepath.Join(configDir, "systemd", "user", systemdLabel)

	logDir, err := DefaultLogDir()
	if err != nil {
		return nil, err
	}

	return &systemdService{
		unitPath: unitPath,
		logDir:   logDir,
	}, nil
}

func (s *systemdService) Label() string {
	return systemdServiceName
}

func (s *systemdService) LogPath() string {
	return filepath.Join(s.logDir, "daemon.log")
}

func (s *systemdService) Install(cfg InstallConfig) error {
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = s.logDir
	}
	if err := EnsureDir(logDir); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	port := cfg.Port
	if port <= 0 {
		port = 9393
	}

	// Build environment section
	var envLines string
	if len(cfg.EnvVars) > 0 {
		var pairs []string
		for k, v := range cfg.EnvVars {
			pairs = append(pairs, fmt.Sprintf(`"%s=%s"`, k, v))
		}
		envLines = "Environment=" + strings.Join(pairs, " ") + "\n"
	}

	stdoutLog := filepath.Join(logDir, "daemon.log")

	unit := fmt.Sprintf(`[Unit]
Description=Astonish AI Agent Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon run --port %d
Restart=always
RestartSec=5
StandardOutput=append:%s
StandardError=append:%s
%s
[Install]
WantedBy=default.target
`, cfg.BinaryPath, port, stdoutLog, stdoutLog, envLines)

	if err := EnsureDir(filepath.Dir(s.unitPath)); err != nil {
		return fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	if err := os.WriteFile(s.unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}

	// Enable linger so the service runs without an active login session
	cmd := exec.Command("loginctl", "enable-linger")
	cmd.Run() // Best-effort; may fail without root but that's OK for user-session use

	// Reload systemd
	cmd = exec.Command("systemctl", "--user", "daemon-reload")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}

	// Enable the service (auto-start on login)
	cmd = exec.Command("systemctl", "--user", "enable", systemdLabel)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}

	return nil
}

func (s *systemdService) Uninstall() error {
	// Stop and disable
	if running, _ := s.IsRunning(); running {
		_ = s.Stop()
	}

	cmd := exec.Command("systemctl", "--user", "disable", systemdLabel)
	cmd.Run() // Best-effort

	if err := os.Remove(s.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	// Reload
	cmd = exec.Command("systemctl", "--user", "daemon-reload")
	cmd.Run()

	return nil
}

func (s *systemdService) Start() error {
	if _, err := os.Stat(s.unitPath); os.IsNotExist(err) {
		return fmt.Errorf("service not installed (unit file not found at %s)", s.unitPath)
	}

	cmd := exec.Command("systemctl", "--user", "start", systemdLabel)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl start failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (s *systemdService) Stop() error {
	cmd := exec.Command("systemctl", "--user", "stop", systemdLabel)
	if output, err := cmd.CombinedOutput(); err != nil {
		out := strings.TrimSpace(string(output))
		// Not loaded/not running is fine for stop
		if !strings.Contains(out, "not loaded") && !strings.Contains(out, "not-found") {
			return fmt.Errorf("systemctl stop failed: %s (%w)", out, err)
		}
	}
	return nil
}

func (s *systemdService) Restart() error {
	cmd := exec.Command("systemctl", "--user", "restart", systemdLabel)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (s *systemdService) IsRunning() (bool, error) {
	cmd := exec.Command("systemctl", "--user", "is-active", "--quiet", systemdLabel)
	err := cmd.Run()
	return err == nil, nil
}

func (s *systemdService) Status() (*ServiceStatus, error) {
	status := &ServiceStatus{
		LogPath: s.LogPath(),
	}

	running, _ := s.IsRunning()
	status.Running = running

	if running {
		// Get PID
		cmd := exec.Command("systemctl", "--user", "show", "--property=MainPID", "--value", systemdLabel)
		if output, err := cmd.CombinedOutput(); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil && pid > 0 {
				status.PID = pid
			}
		}
	}

	return status, nil
}
