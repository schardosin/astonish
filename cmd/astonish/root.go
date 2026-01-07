package astonish

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Execute is the main entry point for the CLI
func Execute() error {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		printUsage()
		if len(os.Args) < 2 {
			return fmt.Errorf("no command provided")
		}
		return nil
	}

	// Handle --version flag
	if os.Args[1] == "--version" || os.Args[1] == "-v" {
		printVersion()
		return nil
	}

	// Check for updates (skip for version command)
	if os.Args[1] != "version" {
		checkForUpdates()
	}

	command := os.Args[1]
	switch command {
	case "flows", "agents": // "agents" is a hidden alias for backwards compatibility
		return handleFlowsCommand(os.Args[2:])
	case "tap":
		return handleTapCommand(os.Args[2:])
	case "studio":
		return handleStudioCommand(os.Args[2:])
	case "setup":
		return handleSetupCommand()
	case "config":
		return handleConfigCommand(os.Args[2:])
	case "tools":
		return handleToolsCommand(os.Args[2:])
	case "demo":
		return handleDemoCommand(os.Args[2:])
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printUsage() {
	fmt.Println("usage: astonish [-h] [-v] {flows,tap,studio,config,setup,tools} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {flows,tap,studio,config,setup,tools}")
	fmt.Println("                        Astonish CLI commands")
	fmt.Println("    flows               Design and run AI flows")
	fmt.Println("    tap                 Manage extension repositories")
	fmt.Println("    studio              Launch the visual editor")
	fmt.Println("    config              Manage configuration")
	fmt.Println("    setup               Run interactive setup")
	fmt.Println("    tools               Manage MCP tools")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
	fmt.Println("  -v, --version         show version information and exit")
}

type updateCheckData struct {
	LastCheck time.Time `json:"lastCheck"`
}

func checkForUpdates() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	astonishDir := filepath.Join(configDir, "astonish")
	updateFile := filepath.Join(astonishDir, "update_check.json")

	var lastCheck updateCheckData

	// Read last check time
	if data, err := os.ReadFile(updateFile); err == nil {
		json.Unmarshal(data, &lastCheck)
	}

	// Only check once per day
	if time.Since(lastCheck.LastCheck) < 24*time.Hour {
		return
	}

	// Perform update check
	resp, err := http.Get("https://api.github.com/repos/schardosin/astonish/releases/latest")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return
	}

	// Update last check time
	os.MkdirAll(astonishDir, 0755)
	lastCheck.LastCheck = time.Now()
	if data, err := json.Marshal(lastCheck); err == nil {
		os.WriteFile(updateFile, data, 0644)
	}

	// Compare versions (remove 'v' prefix if present)
	current := Version
	if len(current) > 0 && current[0] == 'v' {
		current = current[1:]
	}
	latest := result.TagName
	if len(latest) > 0 && latest[0] == 'v' {
		latest = latest[1:]
	}

	if current != latest {
		fmt.Println()
		fmt.Printf("\033[93mA new version of Astonish is available: %s\033[0m\n", result.TagName)
		fmt.Printf("\033[93mRun \033[1mbrew upgrade schardosin/astonish/astonish\033[0m\033[93m to update.\033[0m\n")
		fmt.Println()
	}
}
