//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	launchdLabel    = "com.astonish.daemon"
	launchdPlistDir = "Library/LaunchAgents"
)

type launchdService struct {
	plistPath string
	logDir    string
}

func newPlatformService() (Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	plistPath := filepath.Join(home, launchdPlistDir, launchdLabel+".plist")

	logDir, err := DefaultLogDir()
	if err != nil {
		return nil, err
	}

	return &launchdService{
		plistPath: plistPath,
		logDir:    logDir,
	}, nil
}

func (s *launchdService) Label() string {
	return launchdLabel
}

func (s *launchdService) LogPath() string {
	return filepath.Join(s.logDir, "daemon.log")
}

func (s *launchdService) Install(cfg InstallConfig) error {
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

	stdoutLog := filepath.Join(logDir, "daemon.log")
	stderrLog := filepath.Join(logDir, "daemon.err")

	// Build environment variables section
	var envSection string
	if len(cfg.EnvVars) > 0 {
		envSection = "\t\t<key>EnvironmentVariables</key>\n\t\t<dict>\n"
		for k, v := range cfg.EnvVars {
			envSection += fmt.Sprintf("\t\t\t<key>%s</key>\n\t\t\t<string>%s</string>\n", k, v)
		}
		envSection += "\t\t</dict>\n"
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
		<string>run</string>
		<string>--port</string>
		<string>%d</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
	<key>ProcessType</key>
	<string>Background</string>
%s</dict>
</plist>
`, launchdLabel, cfg.BinaryPath, port, stdoutLog, stderrLog, envSection)

	if err := EnsureDir(filepath.Dir(s.plistPath)); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	if err := os.WriteFile(s.plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	return nil
}

func (s *launchdService) Uninstall() error {
	// Stop first if running
	if running, _ := s.IsRunning(); running {
		_ = s.Stop()
	}

	if err := os.Remove(s.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	return nil
}

func (s *launchdService) Start() error {
	// Check if plist exists
	if _, err := os.Stat(s.plistPath); os.IsNotExist(err) {
		return fmt.Errorf("service not installed (plist not found at %s)", s.plistPath)
	}

	uid, err := currentUID()
	if err != nil {
		return err
	}

	// Bootstrap the service into the user domain
	domain := fmt.Sprintf("gui/%s", uid)
	cmd := exec.Command("launchctl", "bootstrap", domain, s.plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		out := strings.TrimSpace(string(output))
		// Error 37 means "already loaded" -- not a real error
		if !strings.Contains(out, "37:") && !strings.Contains(out, "already loaded") {
			return fmt.Errorf("launchctl bootstrap failed: %s (%w)", out, err)
		}
	}

	// Kickstart to ensure it's running now
	target := fmt.Sprintf("%s/%s", domain, launchdLabel)
	cmd = exec.Command("launchctl", "kickstart", "-k", target)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart failed: %s (%w)", strings.TrimSpace(string(output)), err)
	}

	return nil
}

func (s *launchdService) Stop() error {
	uid, err := currentUID()
	if err != nil {
		return err
	}

	// Bootout (unloads and sends SIGTERM)
	domain := fmt.Sprintf("gui/%s", uid)
	cmd := exec.Command("launchctl", "bootout", domain, s.plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		out := strings.TrimSpace(string(output))
		// Error 113 means "not loaded" / "not running" -- not a real error for stop
		if !strings.Contains(out, "113:") && !strings.Contains(out, "not find") {
			return fmt.Errorf("launchctl bootout failed: %s (%w)", out, err)
		}
	}
	return nil
}

func (s *launchdService) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start()
}

func (s *launchdService) IsRunning() (bool, error) {
	cmd := exec.Command("launchctl", "list", launchdLabel)
	if err := cmd.Run(); err != nil {
		return false, nil
	}
	return true, nil
}

func (s *launchdService) Status() (*ServiceStatus, error) {
	status := &ServiceStatus{
		LogPath: s.LogPath(),
	}

	// Check via launchctl list
	cmd := exec.Command("launchctl", "list", launchdLabel)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Not loaded
		status.Running = false
		return status, nil
	}

	// Parse the output for PID
	// launchctl list <label> outputs a key-value format:
	// "PID" = <number>;
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "\"PID\"") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				pidStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), ";"))
				if pid, err := strconv.Atoi(pidStr); err == nil {
					status.PID = pid
					status.Running = IsProcessRunning(pid)
				}
			}
		}
	}

	// If we couldn't parse PID but launchctl found it, it's running
	if status.PID == 0 {
		status.Running = true
	}

	return status, nil
}

func currentUID() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	return u.Uid, nil
}
