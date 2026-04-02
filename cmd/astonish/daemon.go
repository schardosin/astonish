package astonish

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/daemon"
	"github.com/schardosin/astonish/pkg/fleet"
)

func handleDaemonCommand(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printDaemonUsage()
		return nil
	}

	subcommand := args[0]
	switch subcommand {
	case "install":
		return handleDaemonInstall(args[1:])
	case "uninstall":
		return handleDaemonUninstall()
	case "start":
		return handleDaemonStart()
	case "stop":
		return handleDaemonStop()
	case "restart":
		return handleDaemonRestart()
	case "status":
		return handleDaemonStatus()
	case "run":
		return handleDaemonRun(args[1:])
	case "logs":
		return handleDaemonLogs(args[1:])
	default:
		fmt.Printf("Unknown daemon subcommand: %s\n", subcommand)
		printDaemonUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

func handleDaemonInstall(args []string) error {
	installCmd := flag.NewFlagSet("daemon install", flag.ExitOnError)
	port := installCmd.Int("port", 0, "HTTP port (default: from config or 9393)")

	if err := installCmd.Parse(args); err != nil {
		return err
	}

	// Find the binary path
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find binary path: %w", err)
	}

	// Load config for defaults
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
	}
	installPort := *port
	if installPort <= 0 && appCfg != nil {
		installPort = appCfg.Daemon.GetPort()
	}
	if installPort <= 0 {
		installPort = 9393
	}

	logDir := ""
	if appCfg != nil {
		logDir = appCfg.Daemon.GetLogDir()
	}

	// Build env vars from provider config
	envVars := make(map[string]string)

	// Inherit HOME and PATH so the daemon process can find tools and config.
	// When running via sudo, SUDO_USER tells us the real user; we resolve
	// their home directory so the daemon reads the correct config files.
	homeDir := os.Getenv("HOME")
	if os.Getuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if u, err := user.Lookup(sudoUser); err == nil {
				homeDir = u.HomeDir
				slog.Info("system-level install: resolved HOME from SUDO_USER",
					"sudo_user", sudoUser, "home", homeDir)
			}
		}
	}
	if homeDir != "" {
		envVars["HOME"] = homeDir
	}
	if path := os.Getenv("PATH"); path != "" {
		envVars["PATH"] = path
	}

	// Capture delegate env vars from the current shell (e.g. BIFROST_API_KEY).
	// Fleet configs declare which env vars their delegates need. We snapshot
	// the current values so the daemon service has them from the start.
	fleetsDir, flErr := config.GetFleetsDir()
	if flErr == nil {
		// Ensure bundled fleets are on disk before loading
		_, bundleErr := fleet.EnsureBundled(fleetsDir)
		if bundleErr != nil {
			slog.Warn("failed to ensure bundled fleets", "error", bundleErr)
		}
		if fleets, loadErr := fleet.LoadFleets(fleetsDir); loadErr == nil {
			for _, name := range fleet.CollectDelegateEnvVars(fleets) {
				if val := os.Getenv(name); val != "" {
					envVars[name] = val
				}
			}
		}
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	if err := svc.Install(daemon.InstallConfig{
		BinaryPath: binaryPath,
		Port:       installPort,
		LogDir:     logDir,
		EnvVars:    envVars,
	}); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}

	mode := "user"
	if svc.IsSystem() {
		mode = "system"
	}
	fmt.Printf("Service installed (%s-level): %s\n", mode, svc.Label())
	fmt.Printf("  Port: %d\n", installPort)
	fmt.Printf("  Logs: %s\n", svc.LogPath())
	if svc.IsSystem() {
		fmt.Printf("\nRun 'sudo astonish daemon start' to start the service.\n")
	} else {
		fmt.Printf("\nRun 'astonish daemon start' to start the service.\n")
	}
	return nil
}

func handleDaemonUninstall() error {
	if err := requireSystemPrivileges("uninstall"); err != nil {
		return err
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	fmt.Printf("Service uninstalled: %s\n", svc.Label())
	return nil
}

func handleDaemonStart() error {
	if err := requireSystemPrivileges("start"); err != nil {
		return err
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	if running, _ := svc.IsRunning(); running {
		fmt.Println("Daemon is already running.")
		return nil
	}

	if err := svc.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	fmt.Println("Daemon started.")
	return nil
}

func handleDaemonStop() error {
	if err := requireSystemPrivileges("stop"); err != nil {
		return err
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	if running, _ := svc.IsRunning(); !running {
		fmt.Println("Daemon is not running.")
		return nil
	}

	if err := svc.Stop(); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	fmt.Println("Daemon stopped.")
	return nil
}

func handleDaemonRestart() error {
	if err := requireSystemPrivileges("restart"); err != nil {
		return err
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	if err := svc.Restart(); err != nil {
		return fmt.Errorf("failed to restart daemon: %w", err)
	}

	fmt.Println("Daemon restarted.")
	return nil
}

func handleDaemonStatus() error {
	if err := requireSystemPrivileges("status"); err != nil {
		return err
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	status, err := svc.Status()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	mode := "user"
	if svc.IsSystem() {
		mode = "system"
	}
	fmt.Printf("Service: %s (%s-level)\n", svc.Label(), mode)
	if status.Running {
		fmt.Printf("Status:  running\n")
		if status.PID > 0 {
			fmt.Printf("PID:     %d\n", status.PID)
		}
	} else {
		fmt.Printf("Status:  stopped\n")
	}
	fmt.Printf("Log:     %s\n", status.LogPath)
	return nil
}

func handleDaemonRun(args []string) error {
	runCmd := flag.NewFlagSet("daemon run", flag.ExitOnError)
	port := runCmd.Int("port", 0, "HTTP port (default: from config or 9393)")

	if err := runCmd.Parse(args); err != nil {
		return err
	}

	return daemon.Run(daemon.RunConfig{
		Port: *port,
	})
}

func handleDaemonLogs(args []string) error {
	logsCmd := flag.NewFlagSet("daemon logs", flag.ExitOnError)
	follow := logsCmd.Bool("f", false, "Follow log output")
	lines := logsCmd.Int("n", 50, "Number of lines to show")

	if err := logsCmd.Parse(args); err != nil {
		return err
	}

	svc, err := daemon.NewService()
	if err != nil {
		return err
	}

	logPath := svc.LogPath()
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Printf("No log file found at %s\n", logPath)
		fmt.Println("The daemon may not have been started yet.")
		return nil
	}

	return daemon.TailLog(logPath, *lines, *follow, os.Stdout)
}

// requireSystemPrivileges checks whether a system-level systemd unit exists
// but the current user is not root. If so, it prints a helpful message and
// returns an error to abort the command. For user-level installs or when
// already running as root, it returns nil.
func requireSystemPrivileges(action string) error {
	if os.Getuid() != 0 && daemon.SystemUnitExists() {
		return fmt.Errorf(
			"system-level service detected at /etc/systemd/system/.\n"+
				"Run with sudo: sudo astonish daemon %s", action)
	}
	return nil
}

func printDaemonUsage() {
	fmt.Println("usage: astonish daemon <command> [options]")
	fmt.Println("")
	fmt.Println("Manage the Astonish background daemon service.")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  install     Install as a system service (launchd on macOS, systemd on Linux)")
	fmt.Println("  uninstall   Remove the system service")
	fmt.Println("  start       Start the daemon service")
	fmt.Println("  stop        Stop the daemon service")
	fmt.Println("  restart     Restart the daemon service")
	fmt.Println("  status      Show daemon status")
	fmt.Println("  run         Run the daemon in the foreground (for debugging)")
	fmt.Println("  logs        Show daemon logs")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  --port      HTTP port (default: from config or 9393)")
	fmt.Println("  -f          Follow log output (for 'logs' command)")
	fmt.Println("  -n          Number of log lines to show (default: 50)")
	fmt.Println("")
	fmt.Println("On Linux, use 'sudo' for system-level install (runs as root with overlay")
	fmt.Println("mount capabilities). Without sudo, installs as a user-level service.")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  sudo astonish daemon install    # System-level (recommended on Linux)")
	fmt.Println("  astonish daemon install          # User-level")
	fmt.Println("  sudo astonish daemon start")
	fmt.Println("  astonish daemon status")
	fmt.Println("  astonish daemon logs -f")
	fmt.Println("  astonish daemon run --port 9394")
}
