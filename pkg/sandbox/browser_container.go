package sandbox

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Browser container naming constants.
const (
	// BrowserTemplateID is the template name for browser containers.
	BrowserTemplateID = "browser"
	// BrowserSharedContainer is the container name for the shared browser instance.
	BrowserSharedContainer = "astn-browser-shared"
	// BrowserDedicatedPrefix is the container name prefix for per-session browser containers.
	BrowserDedicatedPrefix = "astn-browser-"
	// BrowserProfileVolume is the Incus storage volume name for the persistent browser profile.
	BrowserProfileVolume = "astonish-browser-profile"
	// BrowserProfileMountPath is where the profile volume is mounted inside the container.
	BrowserProfileMountPath = "/home/browser/.config/chromium"
	// DefaultKasmVNCPort is the default port KasmVNC listens on inside the container.
	DefaultKasmVNCPort = 6901
	// kasmVNCDisplay is the X11 display number used by KasmVNC. The websocket
	// port is set independently via -websocketPort, so the display number is
	// just a low, arbitrary value.
	kasmVNCDisplay = 1
	// DefaultCDPPort is the external port for Chromium's CDP endpoint, exposed on
	// 0.0.0.0 via socat. This is the port go-rod connects to from the host.
	DefaultCDPPort = 9222
	// internalCDPPort is the loopback-only port Chromium actually binds to.
	// Chromium ignores --remote-debugging-address (it only works in content_shell,
	// not the full browser) and always binds DevTools to 127.0.0.1. We use socat
	// to forward from 0.0.0.0:DefaultCDPPort to 127.0.0.1:internalCDPPort.
	internalCDPPort = 9223
)

// BrowserContainerConfig controls browser container creation and runtime.
type BrowserContainerConfig struct {
	// ViewportWidth is the browser viewport width in pixels. Default: 1280.
	ViewportWidth int
	// ViewportHeight is the browser viewport height in pixels. Default: 720.
	ViewportHeight int
	// KasmVNCPort is the port KasmVNC listens on. Default: 6901.
	KasmVNCPort int
	// KasmVNCPassword is the VNC password. Empty = auto-generated.
	KasmVNCPassword string
	// Proxy is the HTTP/SOCKS proxy for browser traffic. Empty = direct.
	Proxy string

	// ChromePath is the host-side path to the browser binary. Used to detect
	// the browser engine for template creation. Empty = default (apt Chromium).
	ChromePath string
	// FingerprintSeed is a deterministic seed for CloakBrowser fingerprint
	// generation (only effective with CloakBrowser binary).
	FingerprintSeed string
	// FingerprintPlatform overrides the OS platform reported by CloakBrowser.
	// Valid values: "windows", "macos", "linux".
	FingerprintPlatform string
}

// BrowserContainerInfo holds runtime information about a browser container.
type BrowserContainerInfo struct {
	// ContainerName is the Incus container name.
	ContainerName string
	// ContainerIP is the container's bridge IPv4 address.
	ContainerIP string
	// CDPURL is the WebSocket URL for go-rod to connect via CDP.
	CDPURL string
	// KasmVNCURL is the URL for the KasmVNC web client (when running).
	KasmVNCURL string
}

// DetectBrowserEngine determines which browser engine is configured based on
// the BrowserContainerConfig fields. Returns "default", "cloakbrowser",
// "custom", or "remote". Only "default" and "cloakbrowser" are supported in
// containers; "custom" and "remote" must fall back to host mode.
func DetectBrowserEngine(cfg BrowserContainerConfig) string {
	if cfg.ChromePath == "" {
		return "default"
	}
	if strings.Contains(cfg.ChromePath, "cloakbrowser") {
		return "cloakbrowser"
	}
	return "custom"
}

// IsContainerCompatibleEngine returns true if the detected engine can run
// inside a container. Custom and remote engines are not supported because
// the host binary may be macOS/Windows and cannot run in a Linux container.
func IsContainerCompatibleEngine(engine string) bool {
	return engine == "default" || engine == "cloakbrowser"
}

// BrowserContainerInstallCommands returns the commands to install the browser
// engine and KasmVNC inside a browser container template. The commands are
// engine-aware: "default" installs Chromium from the xtradeb PPA (Ubuntu's
// own chromium-browser package is snap-only and hangs in LXC containers),
// "cloakbrowser" installs python3 + pip3 + xvfb + the CloakBrowser package.
//
// Common packages (X11 deps, fonts, KasmVNC, browser user) are shared across
// all engines.
func BrowserContainerInstallCommands(engine string) [][]string {
	// Common: apt update
	cmds := [][]string{
		{"apt-get", "update"},
	}

	// Engine-specific packages
	switch engine {
	case "cloakbrowser":
		// CloakBrowser needs python3, pip3, xvfb (for headed stealth mode),
		// plus all the shared X11/font deps that Chromium requires.
		cmds = append(cmds, []string{"apt-get", "install", "-y",
			// CloakBrowser runtime deps
			"python3", "python3-pip", "xvfb",
			// Chromium shared deps (CloakBrowser is a patched Chromium)
			"fonts-liberation", "fonts-noto-color-emoji",
			"xdg-utils", "libnss3", "libatk-bridge2.0-0",
			"libx11-xcb1", "libxcomposite1", "libxrandr2",
			"libgbm1", "libasound2t64",
			// KasmVNC dependencies
			"libjpeg-turbo8", "libwebp-dev", "libssl3",
			"xfonts-base", "xfonts-75dpi", "xfonts-100dpi",
			"x11-xserver-utils",
			// SSL cert for KasmVNC (provides ssl-cert group + snakeoil certs)
			"ssl-cert",
			// CDP port forwarding (Chromium binds DevTools to loopback only)
			"socat",
			// Utilities
			"wget", "ca-certificates",
		})
	default: // "default" — Chromium from xtradeb PPA
		// Ubuntu 24.04's chromium-browser package is a snap transitional shim
		// that triggers `snap install chromium`. Snap does not work inside
		// unprivileged LXC containers (requires squashfs mounts and AppArmor
		// confinement), causing the install to hang indefinitely.
		//
		// The xtradeb/apps PPA provides a native .deb build of Chromium for
		// Ubuntu noble. Binary installs to /usr/bin/chromium.
		cmds = append(cmds,
			// Install add-apt-repository tool
			[]string{"apt-get", "install", "-y", "software-properties-common"},
			// Add PPA with native Chromium .deb packages
			[]string{"add-apt-repository", "-y", "ppa:xtradeb/apps"},
			// Refresh package lists after adding PPA
			[]string{"apt-get", "update"},
			// Install Chromium + shared deps
			[]string{"apt-get", "install", "-y",
				"chromium",
				// Chromium shared deps
				"fonts-liberation", "fonts-noto-color-emoji",
				"xdg-utils", "libnss3", "libatk-bridge2.0-0",
				"libx11-xcb1", "libxcomposite1", "libxrandr2",
				"libgbm1", "libasound2t64",
				// KasmVNC dependencies
				"libjpeg-turbo8", "libwebp-dev", "libssl3",
				"xfonts-base", "xfonts-75dpi", "xfonts-100dpi",
				"x11-xserver-utils",
				// SSL cert for KasmVNC (provides ssl-cert group + snakeoil certs)
				"ssl-cert",
				// CDP port forwarding (Chromium binds DevTools to loopback only)
				"socat",
				// Utilities
				"wget", "ca-certificates",
			},
		)
	}

	// Common: create browser user (KasmVNC cannot run as root)
	cmds = append(cmds,
		[]string{"useradd", "-m", "-s", "/bin/bash", "browser"},
		// KasmVNC needs to read the SSL snakeoil key (/etc/ssl/private/ssl-cert-snakeoil.key)
		// even when require_ssl is false — the Xvnc binary validates the cert path at startup.
		// The key file is owned by root:ssl-cert with mode 640, so the browser user must be
		// in the ssl-cert group to avoid a startup failure.
		[]string{"usermod", "-aG", "ssl-cert", "browser"},
	)

	// Common: install KasmVNC from release deb (Ubuntu 24.04 noble amd64)
	cmds = append(cmds, []string{"sh", "-c",
		"wget -q https://github.com/kasmtech/KasmVNC/releases/download/v1.3.3/kasmvncserver_noble_1.3.3_amd64.deb -O /tmp/kasmvnc.deb && " +
			"dpkg -i /tmp/kasmvnc.deb || apt-get install -f -y && " +
			"rm -f /tmp/kasmvnc.deb",
	})

	// Common: configure KasmVNC for headless/non-interactive operation.
	// KasmVNC has several interactive prompts that hang ExecSimple (which has
	// no stdin): (1) user creation prompt, (2) desktop environment selection.
	// We pre-create all required files to skip these prompts entirely.
	cmds = append(cmds,
		// Create the .vnc directory
		[]string{"su", "-", "browser", "-c", "mkdir -p ~/.vnc"},
		// Write kasmvnc.yaml: disable SSL (internal proxy handles TLS),
		// disable the interactive prompt, and bind to all interfaces.
		[]string{"sh", "-c", `cat > /home/browser/.vnc/kasmvnc.yaml << 'KASMCFG'
network:
  protocol: http
  interface: 0.0.0.0
  use_ipv4: true
  ssl:
    require_ssl: false
user_session:
  concurrent_connections_prompt: false
logging:
  log_writer_name: all
  log_dest: logfile
  level: 30
command_line:
  prompt: false
KASMCFG
chown browser:browser /home/browser/.vnc/kasmvnc.yaml`},
		// Create a minimal xstartup that just keeps the X session alive.
		// We don't need a desktop environment — KasmVNC's X server provides
		// the display, and the browser is already running headless via CDP.
		// For handoff we just need a window manager so the user can interact.
		[]string{"sh", "-c", `cat > /home/browser/.vnc/xstartup << 'XSTARTUP'
#!/bin/bash
# Minimal session: just keep the X server alive.
# The browser is controlled via CDP; KasmVNC provides visual access only.
exec sleep infinity
XSTARTUP
chmod +x /home/browser/.vnc/xstartup
chown browser:browser /home/browser/.vnc/xstartup`},
		// Mark desktop environment as already selected (skips select-de.sh prompt)
		[]string{"su", "-", "browser", "-c", "touch ~/.vnc/.de-was-selected"},
		// Create a default KasmVNC user "user" with write permission.
		// The actual password is set at handoff time; this just ensures the
		// user entry exists so vncserver doesn't prompt for user creation.
		[]string{"sh", "-c",
			`printf "kasmvnc\nkasmvnc\n" | su - browser -c "kasmvncpasswd -u user -w"`,
		},
	)

	// CloakBrowser-specific: install the Python package and download the binary
	if engine == "cloakbrowser" {
		cmds = append(cmds,
			[]string{"pip3", "install", "--break-system-packages", "cloakbrowser"},
			// Download the CloakBrowser Chromium binary into ~browser/.cloakbrowser/
			// Run as the browser user so the binary lands in the right home directory.
			[]string{"su", "-", "browser", "-c",
				"python3 -c \"import cloakbrowser; print(cloakbrowser.ensure_binary())\"",
			},
		)
	}

	// Common: clean up apt cache to reduce image size
	cmds = append(cmds,
		[]string{"apt-get", "clean"},
		[]string{"rm", "-rf", "/var/lib/apt/lists/*"},
	)

	return cmds
}

// InitBrowserTemplate creates a browser container template with the configured
// browser engine and KasmVNC pre-installed. The template is named
// "astn-tpl-browser" and can be used to launch both shared and dedicated
// browser containers.
//
// The engine is detected from cfg: "default" installs chromium-browser from
// apt, "cloakbrowser" installs the CloakBrowser Python package and binary.
// Custom/remote engines are not supported and return an error.
//
// This is separate from the @base sandbox template — browser containers don't
// need the full development toolchain (Docker, Node.js, etc.) and session
// containers don't need Chromium/KasmVNC.
func InitBrowserTemplate(client *IncusClient, registry *TemplateRegistry, cfg BrowserContainerConfig, progressFunc func(string)) error {
	engine := DetectBrowserEngine(cfg)
	if !IsContainerCompatibleEngine(engine) {
		return fmt.Errorf(
			"browser engine %q is not compatible with container mode; "+
				"only 'default' and 'cloakbrowser' engines are supported in containers",
			engine,
		)
	}

	containerName := TemplateName(BrowserTemplateID)

	progress := func(msg string, args ...any) {
		s := fmt.Sprintf(msg, args...)
		if progressFunc != nil {
			progressFunc(s)
		} else {
			fmt.Print(s)
		}
	}

	// Check if already exists with a valid snapshot
	if client.InstanceExists(containerName) {
		if client.HasSnapshot(containerName, SnapshotName) {
			progress("Browser template already initialized.\n")
			return nil
		}
		// Incomplete — clean up and re-create
		progress("Found incomplete browser template, cleaning up...\n")
		if err := client.StopAndDeleteInstance(containerName); err != nil {
			return fmt.Errorf("failed to clean up incomplete browser template: %w", err)
		}
	}

	progress("Creating browser template from %s (engine: %s)...\n", DefaultBaseImage, engine)

	if err := client.LaunchFromImage(containerName, DefaultBaseImage, nil); err != nil {
		return fmt.Errorf("failed to create browser template: %w", err)
	}

	progress("Starting browser template...\n")
	if err := client.StartInstance(containerName); err != nil {
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to start browser template: %w", err)
	}

	// Wait for network
	progress("Waiting for container to be ready...\n")
	if err := waitForReady(client, containerName, 60*time.Second); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("browser container did not become ready: %w", err)
	}

	// Install browser engine + KasmVNC
	engineLabel := "Chromium"
	if engine == "cloakbrowser" {
		engineLabel = "CloakBrowser"
	}
	progress("Installing %s and KasmVNC...\n", engineLabel)
	for _, cmd := range BrowserContainerInstallCommands(engine) {
		progress("  Running: %v\n", cmd)
		exitCode, err := client.ExecSimple(containerName, cmd)
		if err != nil {
			client.StopAndDeleteInstance(containerName)
			return fmt.Errorf("failed to run %v: %w", cmd, err)
		}
		if exitCode != 0 {
			client.StopAndDeleteInstance(containerName)
			return fmt.Errorf("command %v exited with code %d", cmd, exitCode)
		}
	}

	// Stop for snapshot
	progress("Stopping template for snapshot...\n")
	if err := client.StopInstance(containerName, false); err != nil {
		client.StopAndDeleteInstance(containerName)
		return fmt.Errorf("failed to stop browser template: %w", err)
	}

	// Shift UIDs for unprivileged containers
	progress("Preparing template for unprivileged containers...\n")
	if err := ShiftTemplateRootfs(client, BrowserTemplateID); err != nil {
		slog.Warn("failed to shift browser template rootfs UIDs", "component", "sandbox", "error", err)
	}

	// Create snapshot
	progress("Creating snapshot...\n")
	if err := client.CreateSnapshot(containerName, SnapshotName); err != nil {
		client.DeleteInstance(containerName)
		return fmt.Errorf("failed to snapshot browser template: %w", err)
	}

	// Register in metadata
	now := time.Now()
	desc := fmt.Sprintf("Browser container template (%s + KasmVNC)", engineLabel)
	meta := &TemplateMeta{
		Name:        BrowserTemplateID,
		Description: desc,
		CreatedAt:   now,
		SnapshotAt:  now,
	}
	if err := registry.Add(meta); err != nil {
		return fmt.Errorf("failed to register browser template: %w", err)
	}

	progress("Browser template initialized successfully.\n")
	return nil
}

// LaunchBrowserContainer creates and starts a browser container from the browser
// template. If shared is true, uses BrowserSharedContainer as the name and
// attaches the persistent profile volume. Otherwise creates a dedicated container
// named with the given sessionID.
//
// Returns info needed to connect go-rod and (later) KasmVNC.
func LaunchBrowserContainer(client *IncusClient, sessionID string, shared bool, cfg BrowserContainerConfig) (*BrowserContainerInfo, error) {
	var containerName string
	if shared {
		containerName = BrowserSharedContainer
	} else {
		suffix := sessionID
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		containerName = BrowserDedicatedPrefix + suffix
	}

	// Check if already running (for shared container reuse)
	if client.InstanceExists(containerName) {
		if client.IsRunning(containerName) {
			return buildBrowserContainerInfo(client, containerName, cfg)
		}
		// Exists but stopped — start it
		if err := client.StartInstance(containerName); err != nil {
			return nil, fmt.Errorf("failed to start existing browser container %q: %w", containerName, err)
		}
		return buildBrowserContainerInfo(client, containerName, cfg)
	}

	// Create from browser template snapshot
	if err := client.CreateContainerFromSnapshot(containerName, BrowserTemplateID, nil); err != nil {
		return nil, fmt.Errorf("failed to create browser container %q: %w", containerName, err)
	}

	// Attach persistent profile volume for shared containers
	if shared {
		if err := client.EnsureStorageVolume(BrowserProfileVolume); err != nil {
			client.DeleteInstance(containerName)
			return nil, fmt.Errorf("failed to ensure browser profile volume: %w", err)
		}
		if err := client.AttachVolume(containerName, BrowserProfileVolume, BrowserProfileMountPath); err != nil {
			client.DeleteInstance(containerName)
			return nil, fmt.Errorf("failed to attach profile volume to %q: %w", containerName, err)
		}
	}

	// Start the container
	if err := client.StartInstance(containerName); err != nil {
		client.DeleteInstance(containerName)
		return nil, fmt.Errorf("failed to start browser container %q: %w", containerName, err)
	}

	// Wait for network
	if _, err := client.GetContainerIPv4(containerName); err != nil {
		client.StopAndDeleteInstance(containerName)
		return nil, fmt.Errorf("browser container %q network not ready: %w", containerName, err)
	}

	// Ensure the profile directory exists and is owned by the browser user
	if shared {
		client.ExecSimple(containerName, []string{"mkdir", "-p", BrowserProfileMountPath})
		client.ExecSimple(containerName, []string{"chown", "-R", "browser:browser", "/home/browser"})
	}

	// Start Chromium with remote debugging enabled (headless, listening on all interfaces)
	if err := startChromiumInContainer(client, containerName, cfg); err != nil {
		client.StopAndDeleteInstance(containerName)
		return nil, fmt.Errorf("failed to start Chromium in %q: %w", containerName, err)
	}

	return buildBrowserContainerInfo(client, containerName, cfg)
}

// DestroyBrowserContainer stops and deletes a browser container. For shared
// containers this is a destructive operation — the container is gone but the
// profile volume persists.
func DestroyBrowserContainer(client *IncusClient, containerName string) error {
	if !client.InstanceExists(containerName) {
		return nil
	}
	return client.StopAndDeleteInstance(containerName)
}

// BrowserDedicatedContainerName returns the container name for a dedicated
// browser container bound to the given session ID.
func BrowserDedicatedContainerName(sessionID string) string {
	suffix := sessionID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return BrowserDedicatedPrefix + suffix
}

// StartKasmVNC starts KasmVNC inside a browser container for human visual access.
// It runs as the "browser" user on the specified port.
//
// Authentication is disabled via -DisableBasicAuth because the Studio reverse
// proxy already provides access control. This avoids the problem where
// KasmVNC's HTTP Basic Auth blocks sub-resource requests (JS, CSS, images,
// WebSocket) that the browser makes without the original query-parameter
// credentials.
//
// Prerequisites (handled by template install commands):
//   - KasmVNC installed via .deb
//   - "browser" user exists
//   - ~/.vnc/xstartup, ~/.vnc/.de-was-selected, ~/.vnc/kasmvnc.yaml pre-created
//   - Default "user" KasmVNC account pre-created with kasmvncpasswd (avoids
//     interactive user-creation prompt even though auth is disabled at runtime)
func StartKasmVNC(client *IncusClient, containerName string, cfg BrowserContainerConfig) error {
	port := cfg.KasmVNCPort
	if port == 0 {
		port = DefaultKasmVNCPort
	}

	width := cfg.ViewportWidth
	if width == 0 {
		width = 1280
	}
	height := cfg.ViewportHeight
	if height == 0 {
		height = 720
	}

	// Start KasmVNC server as the browser user.
	// Display number and websocket port are independent in KasmVNC — we use a
	// fixed low display (:1) and set the websocket port via -websocketPort.
	// -DisableBasicAuth skips HTTP Basic Auth for all requests (HTML, JS, CSS,
	// WebSocket). This is an undocumented Xvnc flag that vncserver passes
	// through to the Xvnc binary.
	geometry := fmt.Sprintf("%dx%d", width, height)
	startCmd := []string{"su", "-", "browser", "-c",
		fmt.Sprintf("vncserver :%d -geometry %s -depth 24 -websocketPort %d -DisableBasicAuth",
			kasmVNCDisplay,
			geometry,
			port,
		),
	}

	exitCode, err := client.ExecSimple(containerName, startCmd)
	if err != nil {
		return fmt.Errorf("failed to start KasmVNC: %w", err)
	}
	// Exit code 29 means "a VNC server is already running" on this display.
	// This is fine — we treat it as success since the VNC server is available.
	if exitCode != 0 && exitCode != 29 {
		return fmt.Errorf("KasmVNC start exited with code %d", exitCode)
	}

	return nil
}

// StopKasmVNC is a no-op. Xvnc serves as the X display server for headed
// Chromium and must remain running for the container's lifetime. VNC proxy
// access is controlled by the handoff token registry in the auth middleware
// — revoking the token blocks new connections without killing the X server.
//
// The function signature is retained for API compatibility.
func StopKasmVNC(_ *IncusClient, _ string, _ int) error {
	return nil
}

// startChromiumInContainer launches the browser inside the container with
// remote debugging enabled so go-rod can connect via CDP.
//
// Chromium always binds its DevTools server to 127.0.0.1 (the
// --remote-debugging-address flag only works in content_shell, not the full
// browser). We work around this by running Chromium on internalCDPPort
// (loopback) and using socat to forward from 0.0.0.0:DefaultCDPPort to
// 127.0.0.1:internalCDPPort, making CDP accessible from the host.
//
// Both engines run Chromium in headed mode on DISPLAY=:1. The X server is
// provided by KasmVNC (Xvnc), which is started first and persists for the
// container's lifetime. This means the browser is always visible via VNC
// during handoff sessions. CDP works identically in headed mode — go-rod
// doesn't require headless.
func startChromiumInContainer(client *IncusClient, containerName string, cfg BrowserContainerConfig) error {
	width := cfg.ViewportWidth
	if width == 0 {
		width = 1280
	}
	height := cfg.ViewportHeight
	if height == 0 {
		height = 720
	}

	// Start KasmVNC (Xvnc) as the X server for display :1. This provides
	// both the virtual display for headed Chromium and the VNC web client
	// for human handoff sessions. Xvnc stays alive for the container's
	// lifetime — it IS the display backend.
	if err := StartKasmVNC(client, containerName, cfg); err != nil {
		return fmt.Errorf("failed to start Xvnc display server: %w", err)
	}

	// Brief wait for Xvnc to be ready to accept X11 connections.
	time.Sleep(1 * time.Second)

	// Allow any local user (root, browser) to connect to the Xvnc display.
	// Xvnc was started as the "browser" user, so its Xauthority file is
	// user-scoped. Without this, Chromium (running as root) gets
	// "No protocol specified / Missing X server" because it can't auth to
	// the X server. We run xhost as the browser user (who owns the display)
	// to grant access. Safe because all connections are container-local.
	xhostCmd := []string{"su", "-", "browser", "-c",
		fmt.Sprintf("DISPLAY=:%d xhost +local:", kasmVNCDisplay),
	}
	_, _ = client.ExecSimple(containerName, xhostCmd)

	engine := DetectBrowserEngine(cfg)

	var launchScript string

	// socat bridge: expose CDP on all interfaces. Chromium binds to loopback
	// only, so we forward 0.0.0.0:DefaultCDPPort -> 127.0.0.1:internalCDPPort.
	socatBridge := fmt.Sprintf(
		"socat TCP-LISTEN:%d,fork,bind=0.0.0.0,reuseaddr TCP:127.0.0.1:%d &\n",
		DefaultCDPPort, internalCDPPort,
	)

	// X11 display provided by Xvnc (started above)
	display := fmt.Sprintf(":%d", kasmVNCDisplay)

	switch engine {
	case "cloakbrowser":
		// CloakBrowser requires headed mode for its stealth patches.
		// Previously used a separate Xvfb — now uses the same Xvnc display
		// so the browser is visible during KasmVNC handoff sessions.
		fingerprintFlags := ""
		if cfg.FingerprintSeed != "" {
			fingerprintFlags += fmt.Sprintf(" --fingerprint %s", cfg.FingerprintSeed)
		}
		if cfg.FingerprintPlatform != "" {
			fingerprintFlags += fmt.Sprintf(" --fingerprint-platform %s", cfg.FingerprintPlatform)
		}

		proxyFlag := ""
		if cfg.Proxy != "" {
			proxyFlag = fmt.Sprintf(" --proxy-server=%s", cfg.Proxy)
		}

		// Find the CloakBrowser binary path inside the container.
		// It's installed under ~browser/.cloakbrowser/chromium-*/chrome.
		// We use the Python module to resolve the exact path.
		launchScript = fmt.Sprintf(`
BROWSER_BIN=$(su - browser -c 'python3 -c "from cloakbrowser.config import get_binary_path; print(get_binary_path())"')
if [ -z "$BROWSER_BIN" ] || [ ! -f "$BROWSER_BIN" ]; then
  echo "CloakBrowser binary not found" >&2
  exit 1
fi
export DISPLAY=%s
# Launch CloakBrowser as the browser user on the Xvnc display
su - browser -c "DISPLAY=%s $BROWSER_BIN \
  --no-sandbox \
  --disable-gpu \
  --disable-dev-shm-usage \
  --remote-debugging-port=%d \
  --window-size=%d,%d \
  --user-data-dir=%s \
  --disable-background-timer-throttling \
  --disable-backgrounding-occluded-windows \
  --disable-renderer-backgrounding \
  --disable-blink-features=AutomationControlled%s%s \
  about:blank &"
sleep 1
# Bridge CDP port to all interfaces so the host can connect
%s`, display, display, internalCDPPort, width, height, BrowserProfileMountPath, fingerprintFlags, proxyFlag, socatBridge)

	default: // "default" — headed Chromium from xtradeb PPA (/usr/bin/chromium)
		proxyFlag := ""
		if cfg.Proxy != "" {
			proxyFlag = fmt.Sprintf(" --proxy-server=%s", cfg.Proxy)
		}

		launchScript = fmt.Sprintf(
			"export DISPLAY=%s\n"+
				"chromium "+
				"--no-sandbox "+
				"--disable-gpu "+
				"--disable-dev-shm-usage "+
				"--remote-debugging-port=%d "+
				"--window-size=%d,%d "+
				"--user-data-dir=%s "+
				"--disable-background-timer-throttling "+
				"--disable-backgrounding-occluded-windows "+
				"--disable-renderer-backgrounding "+
				"--disable-blink-features=AutomationControlled%s "+
				"about:blank &\nsleep 1\n"+
				"# Bridge CDP port to all interfaces so the host can connect\n%s",
			display, internalCDPPort, width, height, BrowserProfileMountPath, proxyFlag, socatBridge,
		)
	}

	// Launch in the background
	cmd := []string{"sh", "-c", launchScript}
	exitCode, err := client.ExecSimple(containerName, cmd)
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("browser launch exited with code %d", exitCode)
	}

	// Wait briefly for Chromium + socat to start
	time.Sleep(2 * time.Second)

	return nil
}

// buildBrowserContainerInfo resolves the container IP and constructs the
// connection info struct.
func buildBrowserContainerInfo(client *IncusClient, containerName string, cfg BrowserContainerConfig) (*BrowserContainerInfo, error) {
	ip, err := client.GetContainerIPv4(containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get IP for browser container %q: %w", containerName, err)
	}

	kasmPort := cfg.KasmVNCPort
	if kasmPort == 0 {
		kasmPort = DefaultKasmVNCPort
	}

	return &BrowserContainerInfo{
		ContainerName: containerName,
		ContainerIP:   ip,
		CDPURL:        fmt.Sprintf("ws://%s:%d", ip, DefaultCDPPort),
		KasmVNCURL:    fmt.Sprintf("http://%s:%d", ip, kasmPort),
	}, nil
}
