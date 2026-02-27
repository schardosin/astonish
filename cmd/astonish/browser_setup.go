package astonish

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
)

// handleBrowserSetup runs the interactive browser engine configuration flow.
// It lets the user select between the default Chromium (auto-downloaded by rod),
// CloakBrowser (anti-detect Chromium with C++ stealth patches), or a custom
// Chrome/Chromium binary path. Dependencies are validated and installed
// automatically where possible.
func handleBrowserSetup() error {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		cfg = &config.AppConfig{
			Providers: make(map[string]config.ProviderConfig),
		}
	}

	// Show current configuration
	currentEngine := detectCurrentEngine(cfg)
	printBrowserStatus(currentEngine, cfg)

	// Step 1: Select browser engine
	var engine string
	engineOptions := []huh.Option[string]{
		huh.NewOption("Default (Chromium, auto-downloaded by Astonish)", "default"),
		huh.NewOption("CloakBrowser (anti-detect Chromium with stealth patches)", "cloakbrowser"),
		huh.NewOption("Custom Chrome/Chromium path", "custom"),
		huh.NewOption("Connect to your browser (Chrome on your machine)", "remote"),
	}

	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select browser engine").
				Description("Choose which browser to use for web automation").
				Options(engineOptions...).
				Value(&engine),
		),
	).Run()
	if err != nil {
		return err
	}

	switch engine {
	case "default":
		return configureBrowserDefault(cfg)
	case "cloakbrowser":
		return configureBrowserCloak(cfg)
	case "custom":
		return configureBrowserCustom(cfg)
	case "remote":
		return configureBrowserRemote(cfg)
	default:
		return fmt.Errorf("unknown engine: %s", engine)
	}
}

// detectCurrentEngine determines which browser engine is currently configured.
func detectCurrentEngine(cfg *config.AppConfig) string {
	if cfg.Browser.RemoteCDPURL != "" {
		return "remote"
	}
	if cfg.Browser.ChromePath == "" {
		return "default"
	}
	if strings.Contains(cfg.Browser.ChromePath, "cloakbrowser") {
		return "cloakbrowser"
	}
	return "custom"
}

// printBrowserStatus displays the current browser configuration.
func printBrowserStatus(engine string, cfg *config.AppConfig) {
	style := lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("252"))

	var status string
	switch engine {
	case "default":
		status = "Currently using: Default Chromium (auto-downloaded)"
	case "cloakbrowser":
		status = fmt.Sprintf("Currently using: CloakBrowser at %s", cfg.Browser.ChromePath)
		if cfg.Browser.FingerprintPlatform != "" {
			status += fmt.Sprintf(" (platform: %s)", cfg.Browser.FingerprintPlatform)
		}
	case "remote":
		status = fmt.Sprintf("Currently using: Remote browser at %s", cfg.Browser.RemoteCDPURL)
	case "custom":
		status = fmt.Sprintf("Currently using: Custom Chrome at %s", cfg.Browser.ChromePath)
	}

	fmt.Println(style.Render(status))
	fmt.Println()
}

// configureBrowserDefault resets browser config to use the auto-downloaded Chromium.
func configureBrowserDefault(cfg *config.AppConfig) error {
	cfg.Browser.ChromePath = ""
	cfg.Browser.FingerprintSeed = ""
	cfg.Browser.FingerprintPlatform = ""
	cfg.Browser.RemoteCDPURL = ""

	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	printBrowserSuccess("Browser set to Default Chromium. It will be auto-downloaded on first use.")
	return nil
}

// configureBrowserCloak sets up CloakBrowser with dependency validation,
// automatic installation, and fingerprint configuration.
func configureBrowserCloak(cfg *config.AppConfig) error {
	fmt.Println()
	fmt.Println("  Checking dependencies...")
	fmt.Println()

	// 1. Check OS (CloakBrowser only supports Linux and macOS)
	if runtime.GOOS == "windows" {
		printBrowserError("CloakBrowser does not support Windows. Use the Default engine or a Custom path.")
		return fmt.Errorf("unsupported platform: windows")
	}

	// 2. Check Python 3
	python3, err := exec.LookPath("python3")
	if err != nil {
		printBrowserCheck(false, "Python 3", "")
		fmt.Println()
		printBrowserError("Python 3 is required to install CloakBrowser.\n  Install it with: apt install python3")
		return fmt.Errorf("python3 not found")
	}
	printBrowserCheck(true, "Python 3", "")

	// 3. Check pip3
	_, err = exec.LookPath("pip3")
	if err != nil {
		printBrowserCheck(false, "pip3", "")
		fmt.Println()

		var installPip bool
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("pip3 is required but not found").
					Description("Install python3-pip automatically?").
					Affirmative("Yes, install").
					Negative("No, abort").
					Value(&installPip),
			),
		).Run()
		if err != nil {
			return err
		}
		if !installPip {
			printBrowserError("pip3 is required. Install it with: apt install python3-pip")
			return fmt.Errorf("pip3 not found")
		}

		fmt.Println("  Installing pip3...")
		cmd := exec.Command("apt-get", "install", "-y", "-qq", "python3-pip")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			printBrowserError(fmt.Sprintf("Failed to install pip3: %v\n  Install manually with: apt install python3-pip", err))
			return fmt.Errorf("failed to install pip3: %w", err)
		}
		printBrowserCheck(true, "pip3", "installed")
	} else {
		printBrowserCheck(true, "pip3", "")
	}

	// 4. Check Xvfb
	_, err = exec.LookPath("Xvfb")
	if err != nil {
		// Also try lowercase
		_, err = exec.LookPath("xvfb")
	}
	if err != nil {
		printBrowserCheck(false, "Xvfb", "")
		fmt.Println()

		var installXvfb bool
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Xvfb is required for headed stealth mode").
					Description("Without Xvfb, the browser falls back to headless mode which is easier to detect.\nInstall Xvfb automatically?").
					Affirmative("Yes, install").
					Negative("No, abort").
					Value(&installXvfb),
			),
		).Run()
		if err != nil {
			return err
		}
		if !installXvfb {
			printBrowserError("Xvfb is required for CloakBrowser stealth mode.\n  Install it with: apt install xvfb")
			return fmt.Errorf("xvfb not found")
		}

		fmt.Println("  Installing Xvfb...")
		cmd := exec.Command("apt-get", "install", "-y", "-qq", "xvfb")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			printBrowserError(fmt.Sprintf("Failed to install Xvfb: %v\n  Install manually with: apt install xvfb", err))
			return fmt.Errorf("failed to install xvfb: %w", err)
		}
		printBrowserCheck(true, "Xvfb", "installed")
	} else {
		printBrowserCheck(true, "Xvfb", "")
	}

	// 5. Check/install CloakBrowser
	binaryPath := findCloakBrowserBinary()
	if binaryPath == "" {
		printBrowserCheck(false, "CloakBrowser", "")
		fmt.Println()
		fmt.Println("  Installing CloakBrowser (this may take a moment)...")

		// Install the Python package
		cmd := exec.Command("pip3", "install", "--break-system-packages", "cloakbrowser")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			printBrowserError(fmt.Sprintf("Failed to install CloakBrowser package: %v", err))
			return fmt.Errorf("failed to install cloakbrowser: %w", err)
		}

		// Download the binary
		fmt.Println("  Downloading CloakBrowser Chromium binary...")
		out, err := exec.Command(python3, "-c",
			"import cloakbrowser; print(cloakbrowser.ensure_binary())").CombinedOutput()
		if err != nil {
			printBrowserError(fmt.Sprintf("Failed to download CloakBrowser binary: %v\n  Output: %s", err, strings.TrimSpace(string(out))))
			return fmt.Errorf("failed to download cloakbrowser binary: %w", err)
		}

		binaryPath = strings.TrimSpace(string(out))
		if binaryPath == "" || !fileExists(binaryPath) {
			printBrowserError("CloakBrowser binary not found after download")
			return fmt.Errorf("cloakbrowser binary not found")
		}
	}

	// Verify the binary works
	verOut, err := exec.Command(binaryPath, "--version").CombinedOutput()
	if err != nil {
		printBrowserError(fmt.Sprintf("CloakBrowser binary failed to run: %v", err))
		return fmt.Errorf("cloakbrowser binary failed: %w", err)
	}
	version := strings.TrimSpace(string(verOut))
	printBrowserCheck(true, "CloakBrowser", version)

	fmt.Printf("      Binary: %s\n", binaryPath)
	fmt.Println()

	// 6. Fingerprint platform selection
	var platform string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Fingerprint platform").
				Description("Which OS should the browser pretend to be running on?").
				Options(
					huh.NewOption("Windows (most common, recommended)", "windows"),
					huh.NewOption("macOS", "macos"),
					huh.NewOption("Linux", "linux"),
				).
				Value(&platform),
		),
	).Run()
	if err != nil {
		return err
	}

	// 7. Fingerprint seed
	var seedChoice string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Fingerprint seed").
				Description("The seed determines a unique, consistent browser fingerprint").
				Options(
					huh.NewOption("Auto-generate (random, persisted across restarts)", "auto"),
					huh.NewOption("Enter custom seed", "custom"),
				).
				Value(&seedChoice),
		),
	).Run()
	if err != nil {
		return err
	}

	var seed string
	if seedChoice == "auto" {
		seed = generateFingerprintSeed()
	} else {
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Fingerprint seed").
					Description("Enter a numeric seed (e.g. 42000)").
					Value(&seed),
			),
		).Run()
		if err != nil {
			return err
		}
		if seed == "" {
			seed = generateFingerprintSeed()
		}
	}

	// 8. Save config
	cfg.Browser.ChromePath = binaryPath
	cfg.Browser.FingerprintSeed = seed
	cfg.Browser.FingerprintPlatform = platform

	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	printBrowserSuccess(fmt.Sprintf("Browser configured to use CloakBrowser!\n  Engine: CloakBrowser (%s)\n  Platform: %s\n  Fingerprint seed: %s", version, platform, seed))
	return nil
}

// configureBrowserCustom lets the user specify a custom Chrome/Chromium binary path.
func configureBrowserCustom(cfg *config.AppConfig) error {
	var chromePath string
	currentPath := cfg.Browser.ChromePath

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Chrome/Chromium binary path").
				Description("Enter the full path to the Chrome or Chromium binary").
				Placeholder("/usr/bin/google-chrome").
				Value(&chromePath),
		),
	).Run()
	if err != nil {
		return err
	}

	if chromePath == "" {
		if currentPath != "" {
			fmt.Println("  No path entered. Keeping current configuration.")
			return nil
		}
		fmt.Println("  No path entered. Using default Chromium.")
		return configureBrowserDefault(cfg)
	}

	// Expand ~ if present
	if strings.HasPrefix(chromePath, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			chromePath = filepath.Join(home, chromePath[2:])
		}
	}

	// Verify the binary exists and runs
	if !fileExists(chromePath) {
		printBrowserError(fmt.Sprintf("File not found: %s", chromePath))
		return fmt.Errorf("chrome binary not found: %s", chromePath)
	}

	verOut, err := exec.Command(chromePath, "--version").CombinedOutput()
	if err != nil {
		printBrowserError(fmt.Sprintf("Binary failed to run: %v", err))
		return fmt.Errorf("chrome binary failed: %w", err)
	}
	version := strings.TrimSpace(string(verOut))
	fmt.Printf("  Verified: %s\n", version)
	fmt.Println()

	cfg.Browser.ChromePath = chromePath
	// Clear CloakBrowser-specific fields since this is a custom binary
	cfg.Browser.FingerprintSeed = ""
	cfg.Browser.FingerprintPlatform = ""

	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	printBrowserSuccess(fmt.Sprintf("Browser configured with custom path!\n  Binary: %s\n  Version: %s", chromePath, version))
	return nil
}

// configureBrowserRemote connects Astonish to a Chrome instance running on the
// user's machine (or anywhere on the network). The user launches Chrome with
// --remote-debugging-port and Astonish auto-discovers the CDP WebSocket URL.
func configureBrowserRemote(cfg *config.AppConfig) error {
	fmt.Println()

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("33")).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("33"))

	fmt.Println(infoStyle.Render(
		"How it works:\n" +
			"1. On your machine, launch Chrome with remote debugging enabled\n" +
			"2. Astonish connects over the network and controls that browser\n" +
			"3. You get your real cookies, extensions, and browsing history\n\n" +
			"Launch Chrome on your machine with:\n\n" +
			"  Windows:  chrome.exe --remote-debugging-port=9222\n" +
			"  macOS:    /Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome --remote-debugging-port=9222\n" +
			"  Linux:    google-chrome --remote-debugging-port=9222"))
	fmt.Println()

	// Ask for host
	var host string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Your machine's IP address or hostname").
				Description("The IP/hostname where Chrome is running (must be reachable from this server)").
				Placeholder("192.168.1.100").
				Value(&host),
		),
	).Run()
	if err != nil {
		return err
	}
	if host == "" {
		printBrowserError("IP address or hostname is required")
		return fmt.Errorf("no host provided")
	}
	host = strings.TrimSpace(host)

	// Ask for port
	var portStr string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Remote debugging port").
				Description("The port Chrome is listening on (default: 9222)").
				Placeholder("9222").
				Value(&portStr),
		),
	).Run()
	if err != nil {
		return err
	}
	if portStr == "" {
		portStr = "9222"
	}
	portStr = strings.TrimSpace(portStr)

	endpoint := fmt.Sprintf("%s:%s", host, portStr)
	fmt.Println()
	fmt.Printf("  Connecting to %s...\n", endpoint)

	// Probe the endpoint and auto-discover the WebSocket URL
	wsURL, browserVersion, err := discoverCDPEndpoint(endpoint)
	if err != nil {
		fmt.Println()
		printBrowserError(fmt.Sprintf(
			"Could not connect to Chrome at %s\n\n"+
				"  Make sure:\n"+
				"  1. Chrome is running with --remote-debugging-port=%s\n"+
				"  2. The port is reachable from this server (check firewall)\n"+
				"  3. If on different networks, use an SSH tunnel:\n"+
				"     ssh -L %s:localhost:%s your-server\n\n"+
				"  Error: %v",
			endpoint, portStr, portStr, portStr, err))
		return fmt.Errorf("connection failed: %w", err)
	}

	printBrowserCheck(true, "Connected", browserVersion)
	fmt.Printf("      CDP endpoint: %s\n", wsURL)
	fmt.Println()

	// Save config
	cfg.Browser.RemoteCDPURL = wsURL
	// Clear local browser fields since we're using a remote browser
	cfg.Browser.ChromePath = ""
	cfg.Browser.FingerprintSeed = ""
	cfg.Browser.FingerprintPlatform = ""

	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	printBrowserSuccess(fmt.Sprintf(
		"Browser configured to connect to your Chrome!\n"+
			"  Remote: %s\n"+
			"  Browser: %s\n\n"+
			"  Remember to launch Chrome with --remote-debugging-port=%s\n"+
			"  before starting Astonish.",
		endpoint, browserVersion, portStr))
	return nil
}

// discoverCDPEndpoint probes a Chrome remote debugging endpoint and returns
// the WebSocket URL and browser version. It queries /json/version which Chrome
// exposes on the debugging port.
func discoverCDPEndpoint(endpoint string) (wsURL string, version string, err error) {
	client := &http.Client{Timeout: 5 * time.Second}

	url := fmt.Sprintf("http://%s/json/version", endpoint)
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	var result struct {
		Browser              string `json:"Browser"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.WebSocketDebuggerURL == "" {
		return "", "", fmt.Errorf("no webSocketDebuggerUrl in response from %s", url)
	}

	// The WebSocket URL from Chrome points to localhost. We need to rewrite
	// the host to the user-provided endpoint so Astonish can reach it.
	wsURL = result.WebSocketDebuggerURL
	wsURL = strings.Replace(wsURL, "localhost:", endpoint+":", 1)
	wsURL = strings.Replace(wsURL, "127.0.0.1:", endpoint+":", 1)
	// Handle case where port is already in the endpoint
	if !strings.Contains(wsURL, endpoint) {
		// Replace just the host portion, keeping the path
		parts := strings.SplitN(wsURL, "/devtools/", 2)
		if len(parts) == 2 {
			wsURL = fmt.Sprintf("ws://%s/devtools/%s", endpoint, parts[1])
		}
	}

	return wsURL, result.Browser, nil
}

// findCloakBrowserBinary looks for an existing CloakBrowser installation.
// First checks via Python (reliable, version-independent), then falls back
// to scanning the known cache directory.
func findCloakBrowserBinary() string {
	// Try Python resolution first (handles version changes gracefully)
	python3, err := exec.LookPath("python3")
	if err == nil {
		out, err := exec.Command(python3, "-c",
			"from cloakbrowser.config import get_binary_path; print(get_binary_path())").CombinedOutput()
		if err == nil {
			path := strings.TrimSpace(string(out))
			if path != "" && fileExists(path) {
				return path
			}
		}
	}

	// Fallback: scan the known cache directory for any chromium-*/chrome
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cacheDir := filepath.Join(home, ".cloakbrowser")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ""
	}

	// Pick the latest version directory (sorted lexicographically)
	var latest string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "chromium-") {
			candidate := filepath.Join(cacheDir, e.Name(), "chrome")
			if fileExists(candidate) {
				latest = candidate
			}
		}
	}
	return latest
}

// generateFingerprintSeed creates a random numeric seed between 10000 and 99999.
func generateFingerprintSeed() string {
	n, err := rand.Int(rand.Reader, big.NewInt(90000))
	if err != nil {
		return "42000" // fallback
	}
	return fmt.Sprintf("%d", n.Int64()+10000)
}

// fileExists returns true if the path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// printBrowserCheck prints a dependency check result line.
func printBrowserCheck(ok bool, name, detail string) {
	checkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true) // green
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // red

	if ok {
		msg := fmt.Sprintf("  %s %s", checkStyle.Render("[ok]"), name)
		if detail != "" {
			msg += fmt.Sprintf(" (%s)", detail)
		}
		fmt.Println(msg)
	} else {
		fmt.Printf("  %s %s\n", failStyle.Render("[missing]"), name)
	}
}

// printBrowserSuccess prints a styled success message for browser configuration.
func printBrowserSuccess(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42"))

	fmt.Println(style.Render(msg))
}

// printBrowserError prints a styled error message for browser configuration.
func printBrowserError(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true).
		Padding(0, 2)

	fmt.Println(style.Render("Error: " + msg))
}
