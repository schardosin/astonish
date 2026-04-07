package sandbox

import (
	"fmt"
	"strings"
	"time"
)

// Browser container constants.
const (
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
	// BrowserProfileMountPath is the Chromium profile dir inside containers.
	BrowserProfileMountPath = "/home/browser/.config/chromium"
)

// BrowserContainerConfig controls browser runtime configuration inside a container.
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
	// the browser engine. Empty = default (apt Chromium).
	ChromePath string
	// FingerprintSeed is a deterministic seed for CloakBrowser fingerprint
	// generation (only effective with CloakBrowser binary).
	FingerprintSeed string
	// FingerprintPlatform overrides the OS platform reported by CloakBrowser.
	// Valid values: "windows", "macos", "linux".
	FingerprintPlatform string
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
// engine and KasmVNC inside a container template. The commands are engine-aware:
// "default" installs Chromium from the xtradeb PPA (Ubuntu's own
// chromium-browser package is snap-only and hangs in LXC containers),
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

// StartKasmVNC starts KasmVNC inside a container for human visual access.
// It runs as the "browser" user on the specified port.
//
// Authentication is disabled via -DisableBasicAuth because the Studio reverse
// proxy already provides access control.
//
// Prerequisites (handled by template install commands):
//   - KasmVNC installed via .deb
//   - "browser" user exists
//   - ~/.vnc/xstartup, ~/.vnc/.de-was-selected, ~/.vnc/kasmvnc.yaml pre-created
//   - Default "user" KasmVNC account pre-created with kasmvncpasswd
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
	if exitCode != 0 && exitCode != 29 {
		return fmt.Errorf("KasmVNC start exited with code %d", exitCode)
	}

	return nil
}

// StopKasmVNC is a no-op. Xvnc serves as the X display server for headed
// Chromium and must remain running for the container's lifetime. VNC proxy
// access is controlled by the handoff token registry in the auth middleware.
func StopKasmVNC(_ *IncusClient, _ string, _ int) error {
	return nil
}

// StartChromiumInContainer launches the browser inside the container with
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
// container's lifetime.
func StartChromiumInContainer(client *IncusClient, containerName string, cfg BrowserContainerConfig) error {
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
	xhostCmd := []string{"su", "-", "browser", "-c",
		fmt.Sprintf("DISPLAY=:%d xhost +local:", kasmVNCDisplay),
	}
	_, _ = client.ExecSimple(containerName, xhostCmd)

	engine := DetectBrowserEngine(cfg)

	var launchScript string

	// socat bridge: expose CDP on all interfaces.
	socatBridge := fmt.Sprintf(
		"socat TCP-LISTEN:%d,fork,bind=0.0.0.0,reuseaddr TCP:127.0.0.1:%d &\n",
		DefaultCDPPort, internalCDPPort,
	)

	display := fmt.Sprintf(":%d", kasmVNCDisplay)

	switch engine {
	case "cloakbrowser":
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
