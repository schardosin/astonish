package sandbox

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Browser container constants.
const (
	// DefaultKasmVNCPort is the default port KasmVNC listens on inside the container.
	DefaultKasmVNCPort = 6901
	// kasmVNCDisplay is the X11 display number used by KasmVNC. The websocket
	// port is kasmVNCPort + (display * 100) = 6901 for display :0.
	kasmVNCDisplay = "0"
	// DefaultCDPPort is the port exposed by the socat bridge inside the container.
	// Chromium's --remote-debugging-port binds to 127.0.0.1 only (ignores
	// --remote-debugging-address=0.0.0.0 since M91+). We use socat to forward
	// from 0.0.0.0:DefaultCDPPort to 127.0.0.1:internalCDPPort, which makes
	// the DevTools endpoint accessible from outside the container.
	DefaultCDPPort = 9222
	// internalCDPPort is the Chromium DevTools port inside the container. Set to
	// 9223 to avoid clashing with the socat bridge on 9222. Chromium listens on
	// this port (localhost only — see DefaultCDPPort comment above for why that's
	// not the full browser) and always binds DevTools to 127.0.0.1. We use socat
	// to forward from 0.0.0.0:DefaultCDPPort to 127.0.0.1:internalCDPPort.
	internalCDPPort = 9223
	// BrowserProfileMountPath is the Chromium profile dir inside containers.
	BrowserProfileMountPath = "/home/browser/.config/chromium"

	// kasmVNCVersion is the KasmVNC release version to install.
	kasmVNCVersion = "1.3.3"
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
// The arch parameter is the Incus server architecture ("x86_64" or "aarch64")
// and is used to select the correct KasmVNC .deb for the platform.
//
// Common packages (X11 deps, fonts, KasmVNC, browser user) are shared across
// all engines.
func BrowserContainerInstallCommands(engine, arch string) [][]string {
	// Common: apt update
	cmds := [][]string{
		{"apt-get", "update"},
	}

	// Engine-specific packages
	switch engine {
	case "cloakbrowser":
		// CloakBrowser needs python3, pip3, xvfb (for headed stealth mode),
		// plus all the shared X11/font deps that Chromium requires.
		// Unlike the "default" engine where `apt install chromium` pulls in
		// transitive deps automatically, CloakBrowser is a standalone binary
		// download — we must install every shared library it links against.
		cmds = append(cmds, []string{"apt-get", "install", "-y",
			// CloakBrowser runtime deps
			"python3", "python3-pip", "xvfb",
			// Chromium shared deps (CloakBrowser is a patched Chromium).
			// These must be listed explicitly because the binary is not
			// installed via apt and has no automatic dependency resolution.
			"fonts-liberation", "fonts-noto-color-emoji",
			"xdg-utils", "libnss3", "libatk-bridge2.0-0",
			"libx11-xcb1", "libxcomposite1", "libxrandr2",
			"libgbm1", "libasound2t64",
			"libcups2",            // CUPS printing (required by Chromium's print subsystem)
			"libpango-1.0-0",      // text layout and rendering
			"libcairo2",           // 2D graphics
			"libdbus-1-3",         // D-Bus IPC
			"libdrm2",             // Direct Rendering Manager
			"libexpat1",           // XML parsing
			"libxdamage1",         // X11 damage extension
			"libxext6",            // X11 extensions
			"libxfixes3",          // X11 fixes extension
			"libxkbcommon0",       // keyboard handling
			"libatspi2.0-0",       // accessibility
			"libvulkan1",          // Vulkan graphics (optional but avoids warnings)
			"libxcb-dri3-0",       // XCB DRI3 extension (GPU buffer sharing)
			"libatk1.0-0",         // ATK accessibility toolkit (base, needed alongside bridge)
			"libgtk-3-0",          // GTK3 (Chromium UI toolkit dependency)
			"libgdk-pixbuf-2.0-0", // GDK-Pixbuf image loading
			"libnspr4",            // Netscape Portable Runtime (NSS dependency)
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
		// The xtradeb/apps PPA provides native Chromium .deb packages for both
		// amd64 and arm64. We use the PPA so apt auto-selects the correct
		// architecture — critical for Apple Silicon Macs where Docker+Incus
		// runs arm64 containers.
		//
		// After adding the PPA, apt-get update may fail with exit code 100
		// due to AppStream metadata (dep11/Components) download errors during
		// Ubuntu mirror syncs. These errors are non-fatal for package
		// installation, so we tolerate them with "|| true".
		cmds = append(cmds,
			// Install add-apt-repository tool + shared deps
			[]string{"apt-get", "install", "-y",
				"software-properties-common",
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
			// Add PPA with native Chromium .deb packages (amd64 + arm64)
			[]string{"add-apt-repository", "-y", "ppa:xtradeb/apps"},
			// Refresh package lists. Tolerate AppStream metadata errors (dep11
			// Components download failures during mirror syncs) — these don't
			// affect package installation. The "|| true" prevents exit code 100
			// from aborting the template creation.
			[]string{"sh", "-c", "apt-get update || true"},
			// Install Chromium (apt auto-selects the correct architecture)
			[]string{"apt-get", "install", "-y", "chromium"},
		)
	}

	// Common: create browser user (KasmVNC cannot run as root)
	cmds = append(cmds,
		[]string{"useradd", "-m", "-s", "/bin/bash", "browser"},
	)

	// Map Incus server architecture to Debian package architecture.
	// Incus returns "x86_64" or "aarch64"; .deb filenames use "amd64" or "arm64".
	debArch := "amd64"
	if arch == "aarch64" {
		debArch = "arm64"
	}

	// Common: install KasmVNC from release deb (Ubuntu 24.04 noble).
	// Use apt-get install with the .deb path (not dpkg) — this resolves and
	// installs transitive dependencies in a single step. The dpkg + apt-get -f
	// pattern silently removes the package on Docker+Incus when deps fail.
	kasmURL := fmt.Sprintf(
		"https://github.com/kasmtech/KasmVNC/releases/download/v%s/kasmvncserver_noble_%s_%s.deb",
		kasmVNCVersion, kasmVNCVersion, debArch,
	)
	cmds = append(cmds,
		// Download the KasmVNC .deb (architecture-aware)
		[]string{"wget", "-q", "-O", "/tmp/kasmvnc.deb", kasmURL},
		// Install with apt-get which resolves deps properly (requires apt 1.1+, Ubuntu 24.04 has 2.7+)
		[]string{"apt-get", "install", "-y", "/tmp/kasmvnc.deb"},
		// Clean up the .deb
		[]string{"rm", "-f", "/tmp/kasmvnc.deb"},
		// Ensure the SSL snakeoil certificate exists. KasmVNC validates the
		// cert path at startup even when require_ssl is false. The ssl-cert
		// package's postinst may fail silently in unprivileged containers, so
		// regenerate explicitly. Fall back to raw openssl if make-ssl-cert fails.
		// This MUST run at template creation time — session containers on
		// overlayfs cannot modify /etc/ssl/private/ due to user namespace
		// restrictions on copy-up operations.
		[]string{"sh", "-c",
			`make-ssl-cert generate-default-snakeoil --force-overwrite 2>/dev/null || ` +
				`openssl req -x509 -newkey rsa:2048 ` +
				`-keyout /etc/ssl/private/ssl-cert-snakeoil.key ` +
				`-out /etc/ssl/certs/ssl-cert-snakeoil.pem ` +
				`-days 3650 -nodes -subj '/CN=localhost' 2>/dev/null`,
		},
		// Force the key + directory world-readable, owned by root:root.
		// On Docker+Incus (macOS), the ssl-cert group GID can become
		// unmapped (nobody:nogroup) after UID shifting, leaving the file
		// unreadable by all users — even root inside the unprivileged
		// container can't chmod it back. Using root:root + 644 avoids
		// any group-based UID mapping issues entirely. Safe because SSL
		// is disabled (require_ssl: false) and the container is isolated
		// behind the exec tunnel.
		[]string{"chmod", "755", "/etc/ssl/private"},
		[]string{"chmod", "644", "/etc/ssl/private/ssl-cert-snakeoil.key"},
		[]string{"chown", "root:root", "/etc/ssl/private/ssl-cert-snakeoil.key"},
	)

	// Common: configure KasmVNC for headless/non-interactive operation.
	// KasmVNC has several interactive prompts that hang ExecSimple (which has
	// no stdin): (1) user creation prompt, (2) desktop environment selection.
	// We pre-create all required files to skip these prompts entirely.
	//
	// All config file operations run as root (not via su/runuser) because in
	// unprivileged LXC containers on Docker+Incus, the UID shift applied by
	// ShiftTemplateRootfs makes /home/browser owned by nobody:nogroup from
	// the perspective of non-mapped users. We create files as root and set
	// ownership explicitly with chown.
	cmds = append(cmds,
		// Create the .vnc directory as root with correct ownership
		[]string{"install", "-d", "-o", "browser", "-g", "browser", "-m", "755", "/home/browser/.vnc"},
		// Write kasmvnc.yaml: disable SSL (internal proxy handles TLS),
		// disable IPv6 (KasmVNC can't bind the same port on both IPv4 and
		// IPv6 simultaneously — upstream bug kasmtech/KasmVNC#183),
		// disable the interactive prompt, and bind to all interfaces.
		[]string{"sh", "-c", `cat > /home/browser/.vnc/kasmvnc.yaml << 'KASMCFG'
network:
  protocol: http
  interface: 0.0.0.0
  use_ipv4: true
  use_ipv6: false
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
		[]string{"sh", "-c", "touch /home/browser/.vnc/.de-was-selected && chown browser:browser /home/browser/.vnc/.de-was-selected"},
		// Create a default KasmVNC user "user" with write permission.
		// The actual password is set at handoff time; this just ensures the
		// user entry exists so vncserver doesn't prompt for user creation.
		// Use runuser instead of su — in unprivileged LXC containers on
		// Docker+Incus, su fails with "Authentication failure" because PAM
		// can't read /etc/shadow (UID namespace mapping breaks pam_unix).
		// runuser (part of util-linux, always present) bypasses PAM.
		[]string{"sh", "-c",
			`printf "kasmvnc\nkasmvnc\n" | runuser -u browser -- /usr/bin/kasmvncpasswd -u user -w`,
		},
	)

	// ARM64 (aarch64): build a small LD_PRELOAD shim that masks problematic
	// HWCAP/HWCAP2 CPU feature bits. On Apple Silicon Macs, the Docker Desktop
	// VM advertises advanced ARMv9 features (SVE2, SME, SME2, BF16, etc.) via
	// getauxval(AT_HWCAP/AT_HWCAP2). Libraries like libjpeg-turbo, Skia,
	// BoringSSL, and zlib detect these features at runtime and use optimized
	// code paths — but some of these instructions are not fully functional in
	// the nested virtualization stack (macOS → Docker VM → Incus LXC), causing
	// SIGILL crashes when rendering image-heavy pages.
	//
	// The shim intercepts getauxval() and masks out everything beyond baseline
	// ARMv8.0 + safe extensions (NEON, AES, SHA, CRC32, atomics). This forces
	// all libraries to use their baseline NEON code paths which work correctly.
	if arch == "aarch64" {
		// The C source for the HWCAP masking shim.
		hwcapShimSource := `
#define _GNU_SOURCE
#include <sys/auxv.h>
#include <dlfcn.h>

/* Safe HWCAP bits to keep (ARMv8.0 baseline + common extensions):
 *   FP, ASIMD, EVTSTRM, AES, PMULL, SHA1, SHA2, CRC32, ATOMICS,
 *   FPHP, ASIMDHP, CPUID, ASIMDRDM, JSCVT, FCMA, LRCPC, DCPOP,
 *   SHA3, ASIMDDP, SHA512, ASIMDFHM, DIT, USCAT, ILRCPC, FLAGM, SB
 * Masked out: SVE(22), SSBS(28), PACA(30), PACG(31), and bits 32+
 */
#define HWCAP_SAFE_MASK  0x2FBFFFFFul

/* Safe HWCAP2 bits: DCPODP(0), FLAGM2(7), FRINT(8), I8MM(13)
 * Masked out: SVE2, all SVE* variants, BF16, BTI, MTE, SME, SME2, etc.
 */
#define HWCAP2_SAFE_MASK 0x2181ul

unsigned long getauxval(unsigned long type) {
    unsigned long (*real_getauxval)(unsigned long) =
        (unsigned long (*)(unsigned long))dlsym(RTLD_NEXT, "getauxval");
    unsigned long val = real_getauxval(type);
    if (type == AT_HWCAP)  return val & HWCAP_SAFE_MASK;
    if (type == AT_HWCAP2) return val & HWCAP2_SAFE_MASK;
    return val;
}
`
		cmds = append(cmds,
			// Install gcc (needed to compile the shim)
			[]string{"apt-get", "install", "-y", "gcc"},
			// Write the C source, compile to shared library, clean up
			[]string{"sh", "-c",
				fmt.Sprintf(`cat > /tmp/hwcap_mask.c << 'SHIMEOF'
%s
SHIMEOF
gcc -shared -fPIC -o /usr/lib/hwcap_mask.so /tmp/hwcap_mask.c -ldl
rm -f /tmp/hwcap_mask.c`, hwcapShimSource),
			},
			// Remove gcc to keep the template lean (only needed at build time)
			[]string{"sh", "-c", "apt-get remove -y gcc && apt-get autoremove -y"},
		)
	}

	// CloakBrowser-specific: install the Python package and download the binary
	if engine == "cloakbrowser" {
		cmds = append(cmds,
			[]string{"pip3", "install", "--break-system-packages", "cloakbrowser"},
			// Download the CloakBrowser Chromium binary into ~browser/.cloakbrowser/
			// Run as the browser user so the binary lands in the right home directory.
			// Use runuser (not su) to avoid PAM authentication failures in
			// unprivileged LXC containers on Docker+Incus.
			[]string{"runuser", "-u", "browser", "--",
				"python3", "-c", "import cloakbrowser; print(cloakbrowser.ensure_binary())",
			},
		)
	}

	// Common: fix ownership of browser home directory.
	// In unprivileged LXC containers on Docker+Incus, the UID shift
	// (ShiftTemplateRootfs) makes files owned by nobody:nogroup after
	// the template is snapshotted. Ensure the browser user owns its
	// home directory and all files within it.
	cmds = append(cmds,
		[]string{"chown", "-R", "browser:browser", "/home/browser"},
	)

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

	// Use runuser instead of su — in unprivileged LXC containers on
	// Docker+Incus, su fails with "Authentication failure" because PAM
	// can't read /etc/shadow through the UID namespace mapping.
	// runuser (part of util-linux) bypasses PAM authentication.
	//
	// Note: /home/browser ownership is correct because ShiftTemplateRootfs
	// shifts ALL UIDs (not just root) during template creation. The shifted
	// UIDs are captured in the snapshot and inherited by session containers.
	geometry := fmt.Sprintf("%dx%d", width, height)
	startCmd := []string{"runuser", "-l", "browser", "-c",
		fmt.Sprintf("vncserver :%s -geometry %s -depth 24 -websocketPort %d -DisableBasicAuth",
			kasmVNCDisplay,
			geometry,
			port,
		),
	}

	// Use ExecWithOutput to capture stdout+stderr — previous iterations
	// of this code used ExecSimple which discarded all output, making it
	// impossible to diagnose failures without manual incus exec debugging.
	exitCode, output, err := ExecWithOutput(client, containerName, startCmd)
	if err != nil {
		return fmt.Errorf("failed to start KasmVNC: %w (output: %s)", err, output)
	}
	// Exit code 29 means "a VNC server is already running" on this display.
	if exitCode != 0 && exitCode != 29 {
		return fmt.Errorf("KasmVNC start exited with code %d: %s", exitCode, strings.TrimSpace(output))
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
	xhostCmd := []string{"runuser", "-l", "browser", "-c",
		fmt.Sprintf("DISPLAY=:%s xhost +local:", kasmVNCDisplay),
	}
	exitCode, err := client.ExecSimple(containerName, xhostCmd)
	if err != nil {
		slog.Warn("xhost +local: failed", "container", containerName, "error", err)
	} else if exitCode != 0 {
		slog.Warn("xhost +local: exited with non-zero code", "container", containerName, "exit_code", exitCode)
	}

	engine := DetectBrowserEngine(cfg)

	var launchScript string

	// socat bridge: expose CDP on all interfaces.
	socatBridge := fmt.Sprintf(
		"socat TCP-LISTEN:%d,fork,bind=0.0.0.0,reuseaddr TCP:127.0.0.1:%d &\n",
		DefaultCDPPort, internalCDPPort,
	)

	display := fmt.Sprintf(":%s", kasmVNCDisplay)

	// On Docker+Incus (Apple Silicon Macs), the Docker Desktop VM advertises
	// advanced ARMv9 CPU features (SVE2, SME, SME2, BF16, BTI, etc.) via
	// getauxval(AT_HWCAP/AT_HWCAP2). Libraries bundled in Chromium
	// (libjpeg-turbo, Skia, BoringSSL, zlib) detect these at runtime and use
	// optimized code paths — but some instructions are not fully functional
	// through the nested virtualization stack (macOS → Docker VM → Incus LXC),
	// causing SIGILL crashes on image-heavy pages.
	//
	// The fix is hwcap_mask.so (compiled during template creation for aarch64),
	// which intercepts getauxval() and masks HWCAP/HWCAP2 down to safe ARMv8.0
	// baseline features. This forces all libraries to use their baseline NEON
	// code paths which work correctly.
	ldPreload := ""
	if activePlatform == PlatformDockerIncus {
		ldPreload = "LD_PRELOAD=/usr/lib/hwcap_mask.so "
	}

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
BROWSER_BIN=$(runuser -u browser -- python3 -c "from cloakbrowser.config import get_binary_path; print(get_binary_path())")
if [ -z "$BROWSER_BIN" ] || [ ! -f "$BROWSER_BIN" ]; then
  echo "CloakBrowser binary not found" >&2
  exit 1
fi
export DISPLAY=%s
BROWSER_LOG=/tmp/cloakbrowser.log
# Launch CloakBrowser as the browser user on the Xvnc display.
# Redirect stdout+stderr to a log file so we can diagnose crashes.
runuser -l browser -c "%sDISPLAY=%s $BROWSER_BIN \
  --no-sandbox \
  --test-type \
  --disable-gpu \
  --disable-dev-shm-usage \
  --remote-debugging-port=%d \
  --window-size=%d,%d \
  --user-data-dir=%s \
  --disable-background-timer-throttling \
  --disable-backgrounding-occluded-windows \
  --disable-renderer-backgrounding \
  --disable-blink-features=AutomationControlled \
  --no-first-run \
  --no-default-browser-check \
  --noerrdialogs \
  --disable-features=TranslateUI%s%s \
  about:blank >$BROWSER_LOG 2>&1 &"
sleep 2
# Verify the browser process started by checking if any chrome/chromium
# process is running. If not, the binary crashed on startup — report the
# log so the error propagates to the user instead of a generic CDP timeout.
if ! pgrep -u browser -f 'chrome|chromium' >/dev/null 2>&1; then
  echo "CloakBrowser process died on startup. Binary: $BROWSER_BIN" >&2
  echo "--- browser log ---" >&2
  cat $BROWSER_LOG >&2 2>/dev/null
  echo "--- end log ---" >&2
  exit 1
fi
# Verify the DevTools port is bound. CloakBrowser may start the main process
# but fail to initialize the DevTools server (e.g. if the binary does not
# support --remote-debugging-port). Wait up to 5 seconds for port %d.
CDP_READY=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  if ss -tln 2>/dev/null | grep -q ':%d '; then
    CDP_READY=1
    break
  fi
  sleep 0.5
done
if [ "$CDP_READY" = "0" ]; then
  echo "CloakBrowser started but DevTools port %d is not listening after 5s." >&2
  echo "Binary: $BROWSER_BIN" >&2
  echo "Listening ports:" >&2
  ss -tln >&2 2>/dev/null
  echo "--- browser log ---" >&2
  cat $BROWSER_LOG >&2 2>/dev/null
  echo "--- end log ---" >&2
  exit 1
fi
# Bridge CDP port to all interfaces so the host can connect
%s`, display, ldPreload, display, internalCDPPort, width, height, BrowserProfileMountPath, fingerprintFlags, proxyFlag,
			internalCDPPort, internalCDPPort, internalCDPPort, socatBridge)

	default: // "default" — headed Google Chrome (/usr/bin/chromium via symlink)
		proxyFlag := ""
		if cfg.Proxy != "" {
			proxyFlag = fmt.Sprintf(" --proxy-server=%s", cfg.Proxy)
		}

		launchScript = fmt.Sprintf(
			"export DISPLAY=%s\n"+
				"runuser -l browser -c \"%sDISPLAY=%s chromium "+
				"--no-sandbox "+
				"--test-type "+
				"--disable-gpu "+
				"--disable-dev-shm-usage "+
				"--remote-debugging-port=%d "+
				"--window-size=%d,%d "+
				"--user-data-dir=%s "+
				"--disable-background-timer-throttling "+
				"--disable-backgrounding-occluded-windows "+
				"--disable-renderer-backgrounding "+
				"--disable-blink-features=AutomationControlled "+
				"--no-first-run "+
				"--no-default-browser-check "+
				"--noerrdialogs "+
				"--disable-features=TranslateUI%s "+
				"about:blank &\"\nsleep 1\n"+
				"# Bridge CDP port to all interfaces so the host can connect\n%s",
			display, ldPreload, display, internalCDPPort, width, height, BrowserProfileMountPath, proxyFlag, socatBridge,
		)
	}

	// Use ExecWithOutput to capture stdout+stderr for diagnostic error messages.
	// The launch script runs background processes (chromium &, socat &) so we
	// wrap it in sh -c ourselves rather than using ExecWithOutput's wrapper.
	cmd := []string{"sh", "-c", launchScript + "\n"}
	exitCode, output, err := ExecWithOutput(client, containerName, cmd)
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w (output: %s)", err, output)
	}
	if exitCode != 0 {
		return fmt.Errorf("browser launch exited with code %d: %s", exitCode, strings.TrimSpace(output))
	}

	// Wait briefly for Chromium + socat to start
	time.Sleep(2 * time.Second)

	return nil
}
