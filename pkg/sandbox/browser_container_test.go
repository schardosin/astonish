package sandbox

import (
	"fmt"
	"strings"
	"testing"
)

func TestDetectBrowserEngine(t *testing.T) {
	tests := []struct {
		name     string
		cfg      BrowserContainerConfig
		expected string
	}{
		{
			name:     "empty path returns default",
			cfg:      BrowserContainerConfig{},
			expected: "default",
		},
		{
			name:     "explicit empty string returns default",
			cfg:      BrowserContainerConfig{ChromePath: ""},
			expected: "default",
		},
		{
			name:     "cloakbrowser in path",
			cfg:      BrowserContainerConfig{ChromePath: "/usr/bin/cloakbrowser"},
			expected: "cloakbrowser",
		},
		{
			name:     "cloakbrowser in subdirectory",
			cfg:      BrowserContainerConfig{ChromePath: "/home/user/.cloakbrowser/chrome"},
			expected: "cloakbrowser",
		},
		{
			name:     "custom chrome path",
			cfg:      BrowserContainerConfig{ChromePath: "/opt/google/chrome/chrome"},
			expected: "custom",
		},
		{
			name:     "custom chromium path",
			cfg:      BrowserContainerConfig{ChromePath: "/snap/bin/chromium"},
			expected: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectBrowserEngine(tt.cfg)
			if got != tt.expected {
				t.Errorf("DetectBrowserEngine(%+v) = %q, want %q", tt.cfg, got, tt.expected)
			}
		})
	}
}

func TestIsContainerCompatibleEngine(t *testing.T) {
	tests := []struct {
		engine   string
		expected bool
	}{
		{"default", true},
		{"cloakbrowser", true},
		{"custom", false},
		{"remote", false},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			got := IsContainerCompatibleEngine(tt.engine)
			if got != tt.expected {
				t.Errorf("IsContainerCompatibleEngine(%q) = %v, want %v", tt.engine, got, tt.expected)
			}
		})
	}
}

// TestBrowserContainerInstallCommands_SharedCommands verifies that commands
// shared across all engines are present: browser user creation, KasmVNC
// config, SSL cert with correct permissions, socat, and runuser usage.
func TestBrowserContainerInstallCommands_SharedCommands(t *testing.T) {
	for _, engine := range []string{"default", "cloakbrowser"} {
		t.Run(engine, func(t *testing.T) {
			cmds := BrowserContainerInstallCommands(engine, "x86_64")
			flat := flattenCommands(cmds)

			// Browser user creation (useradd, NOT adduser)
			assertContainsCmd(t, cmds, "useradd", "browser user creation")

			// KasmVNC config file
			assertContainsStr(t, flat, "kasmvnc.yaml", "KasmVNC config file creation")
			assertContainsStr(t, flat, "require_ssl: false", "SSL disabled in KasmVNC config")
			assertContainsStr(t, flat, "use_ipv6: false", "IPv6 disabled in KasmVNC config")

			// SSL cert: must be chmod 644 root:root (NOT 640 root:ssl-cert)
			assertContainsCmd(t, cmds, "chmod", "SSL cert chmod")
			assertCmdSequence(t, cmds, []string{"chmod", "644"}, "SSL key must be world-readable (644)")
			assertCmdSequence(t, cmds, []string{"chown", "root:root"}, "SSL key must be owned by root:root")

			// SSL cert generation
			assertContainsStr(t, flat, "make-ssl-cert", "SSL cert generation via make-ssl-cert")
			assertContainsStr(t, flat, "openssl req", "SSL cert fallback via openssl")

			// socat installed (for CDP port forwarding)
			assertContainsStr(t, flat, "socat", "socat package for CDP bridge")

			// Uses runuser (NOT su) for unprivileged container compatibility
			assertContainsStr(t, flat, "runuser", "must use runuser, not su")

			// KasmVNC password setup
			assertContainsStr(t, flat, "kasmvncpasswd", "KasmVNC password pre-creation")

			// No ssl-cert group dependency for browser user (the file is 644 root:root)
			for _, cmd := range cmds {
				joined := strings.Join(cmd, " ")
				if strings.Contains(joined, "usermod") && strings.Contains(joined, "ssl-cert") {
					t.Error("browser user should NOT be added to ssl-cert group (file is 644 root:root)")
				}
			}

			// Cleanup
			assertContainsCmd(t, cmds, "apt-get", "apt-get clean")
		})
	}
}

// TestBrowserContainerInstallCommands_ArchAwareKasmVNC verifies that the
// KasmVNC .deb URL uses the correct architecture suffix.
func TestBrowserContainerInstallCommands_ArchAwareKasmVNC(t *testing.T) {
	tests := []struct {
		arch    string
		debArch string
	}{
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			cmds := BrowserContainerInstallCommands("default", tt.arch)
			flat := flattenCommands(cmds)

			expectedURL := fmt.Sprintf("kasmvncserver_noble_%s_%s.deb", kasmVNCVersion, tt.debArch)
			assertContainsStr(t, flat, expectedURL, "KasmVNC .deb URL for "+tt.arch)
		})
	}
}

// TestBrowserContainerInstallCommands_DefaultEngine verifies engine-specific
// commands for the default (Chromium from xtradeb PPA) engine.
func TestBrowserContainerInstallCommands_DefaultEngine(t *testing.T) {
	cmds := BrowserContainerInstallCommands("default", "x86_64")
	flat := flattenCommands(cmds)

	// Must add xtradeb PPA
	assertContainsStr(t, flat, "ppa:xtradeb/apps", "xtradeb PPA for native Chromium .deb")

	// Must install chromium package
	assertCmdSequence(t, cmds, []string{"apt-get", "install", "-y", "chromium"}, "chromium package install")

	// Must tolerate apt-get update failures (|| true) for AppStream metadata errors
	assertContainsStr(t, flat, "apt-get update || true", "tolerant apt-get update after PPA")
}

// TestBrowserContainerInstallCommands_CloakBrowserEngine verifies engine-specific
// commands for the CloakBrowser engine.
func TestBrowserContainerInstallCommands_CloakBrowserEngine(t *testing.T) {
	cmds := BrowserContainerInstallCommands("cloakbrowser", "x86_64")
	flat := flattenCommands(cmds)

	// Must install python3 and pip3
	assertContainsStr(t, flat, "python3", "python3 for CloakBrowser")
	assertContainsStr(t, flat, "python3-pip", "pip3 for CloakBrowser")

	// Must install cloakbrowser pip package
	assertCmdSequence(t, cmds, []string{"pip3", "install"}, "pip install cloakbrowser")
	assertContainsStr(t, flat, "cloakbrowser", "cloakbrowser pip package")

	// Must NOT add xtradeb PPA (that's for the default engine)
	if strings.Contains(flat, "ppa:xtradeb") {
		t.Error("CloakBrowser engine should not add xtradeb PPA")
	}
}

// TestCloakBrowserInstallCommands_RequiredSharedLibs verifies that the
// CloakBrowser install command list includes every shared library that the
// standalone Chromium binary needs. These cannot be auto-resolved by apt
// because CloakBrowser is installed via pip, not as a .deb package.
func TestCloakBrowserInstallCommands_RequiredSharedLibs(t *testing.T) {
	cmds := BrowserContainerInstallCommands("cloakbrowser", "x86_64")
	flat := flattenCommands(cmds)

	// Each of these packages was confirmed required during live testing.
	// Missing any one causes the CloakBrowser binary to fail with a
	// "cannot open shared object file" error on startup.
	requiredLibs := []struct {
		pkg  string
		desc string
	}{
		{"libcups2", "CUPS printing (Chromium print subsystem)"},
		{"libpango-1.0-0", "text layout and rendering"},
		{"libcairo2", "2D graphics"},
		{"libdbus-1-3", "D-Bus IPC"},
		{"libdrm2", "Direct Rendering Manager"},
		{"libexpat1", "XML parsing"},
		{"libxdamage1", "X11 damage extension"},
		{"libxext6", "X11 extensions"},
		{"libxfixes3", "X11 fixes extension"},
		{"libxkbcommon0", "keyboard handling"},
		{"libatspi2.0-0", "accessibility"},
		{"libvulkan1", "Vulkan graphics"},
		{"libxcb-dri3-0", "XCB DRI3 extension (GPU buffer sharing)"},
		{"libatk1.0-0", "ATK accessibility toolkit"},
		{"libgtk-3-0", "GTK3 (Chromium UI toolkit)"},
		{"libgdk-pixbuf-2.0-0", "GDK-Pixbuf image loading"},
		{"libnspr4", "Netscape Portable Runtime (NSS dependency)"},
		{"libnss3", "Network Security Services"},
		{"libatk-bridge2.0-0", "ATK-Bridge accessibility"},
		{"libx11-xcb1", "X11-XCB bridge"},
		{"libxcomposite1", "X11 composite extension"},
		{"libxrandr2", "X11 RandR extension"},
		{"libgbm1", "Mesa GBM (buffer management)"},
		{"libasound2t64", "ALSA sound"},
	}

	for _, lib := range requiredLibs {
		if !strings.Contains(flat, lib.pkg) {
			t.Errorf("missing required shared library package %q (%s)", lib.pkg, lib.desc)
		}
	}
}

// TestLaunchScript_NoExtraTabFlags verifies that both the default and
// CloakBrowser launch scripts include Chromium flags that prevent extra
// tabs from opening on startup (first-run page, default browser check, etc.).
func TestLaunchScript_NoExtraTabFlags(t *testing.T) {
	requiredFlags := []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--noerrdialogs",
		"--disable-features=TranslateUI",
	}

	for _, engine := range []string{"default", "cloakbrowser"} {
		t.Run(engine, func(t *testing.T) {
			cfg := BrowserContainerConfig{}
			if engine == "cloakbrowser" {
				cfg.ChromePath = "cloakbrowser"
			}
			script := buildLaunchScript(engine, cfg, 1280, 720)

			for _, flag := range requiredFlags {
				if !strings.Contains(script, flag) {
					t.Errorf("launch script missing flag %q", flag)
				}
			}
		})
	}
}

// TestLaunchScript_CloakBrowser_HasDiagnostics verifies that the CloakBrowser
// launch script includes startup diagnostics: stderr log capture, process
// liveness check via pgrep, and CDP port binding verification via ss.
// These diagnostics surface the real error when CloakBrowser crashes on
// startup instead of a generic "CDP timeout after 15s" message.
func TestLaunchScript_CloakBrowser_HasDiagnostics(t *testing.T) {
	script := buildLaunchScript("cloakbrowser", BrowserContainerConfig{
		ChromePath: "cloakbrowser",
	}, 1280, 720)

	checks := []struct {
		substr string
		desc   string
	}{
		{"BROWSER_LOG=/tmp/cloakbrowser.log", "stderr log file path"},
		{">$BROWSER_LOG 2>&1", "stdout+stderr redirect to log file"},
		{"pgrep -u browser", "process liveness check via pgrep"},
		{"CloakBrowser process died on startup", "crash error message"},
		{"cat $BROWSER_LOG", "log dump on failure"},
		{"ss -tln", "CDP port binding check via ss"},
		{"CDP_READY=", "CDP readiness loop variable"},
		{"CloakBrowser started but DevTools port", "CDP port failure message"},
	}

	for _, c := range checks {
		if !strings.Contains(script, c.substr) {
			t.Errorf("CloakBrowser launch script missing diagnostic: %s (expected %q)", c.desc, c.substr)
		}
	}
}

// TestLaunchScript_Default_NoDiagnostics verifies that the default Chromium
// launch script does NOT include CloakBrowser-specific diagnostics (pgrep,
// BROWSER_LOG, etc.) — the default engine is installed via apt and is more
// reliable, so the extra diagnostics would be noise.
func TestLaunchScript_Default_NoDiagnostics(t *testing.T) {
	script := buildLaunchScript("default", BrowserContainerConfig{}, 1280, 720)

	// These are CloakBrowser-specific and should NOT appear in the default script.
	unwanted := []string{
		"BROWSER_LOG=",
		"pgrep",
		"CDP_READY=",
		"CloakBrowser",
	}

	for _, s := range unwanted {
		if strings.Contains(script, s) {
			t.Errorf("default launch script should not contain CloakBrowser diagnostic %q", s)
		}
	}
}

// TestLaunchScript_SharedFlags verifies that both engines include core
// Chromium flags required for container operation (sandbox disabled, CDP port,
// user data dir, anti-detection, etc.).
func TestLaunchScript_SharedFlags(t *testing.T) {
	sharedFlags := []string{
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		fmt.Sprintf("--remote-debugging-port=%d", internalCDPPort),
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-blink-features=AutomationControlled",
		BrowserProfileMountPath,
		"about:blank",
	}

	for _, engine := range []string{"default", "cloakbrowser"} {
		t.Run(engine, func(t *testing.T) {
			cfg := BrowserContainerConfig{}
			if engine == "cloakbrowser" {
				cfg.ChromePath = "cloakbrowser"
			}
			script := buildLaunchScript(engine, cfg, 1280, 720)

			for _, flag := range sharedFlags {
				if !strings.Contains(script, flag) {
					t.Errorf("launch script missing shared flag/value %q", flag)
				}
			}
		})
	}
}

// TestLaunchScript_SocatBridge verifies that both engines include the socat
// bridge command that forwards CDP from 0.0.0.0:DefaultCDPPort to the
// internal loopback port.
func TestLaunchScript_SocatBridge(t *testing.T) {
	expected := fmt.Sprintf("socat TCP-LISTEN:%d,fork,bind=0.0.0.0,reuseaddr TCP:127.0.0.1:%d",
		DefaultCDPPort, internalCDPPort)

	for _, engine := range []string{"default", "cloakbrowser"} {
		t.Run(engine, func(t *testing.T) {
			cfg := BrowserContainerConfig{}
			if engine == "cloakbrowser" {
				cfg.ChromePath = "cloakbrowser"
			}
			script := buildLaunchScript(engine, cfg, 1280, 720)

			if !strings.Contains(script, expected) {
				t.Errorf("launch script missing socat bridge command")
			}
		})
	}
}

// TestLaunchScript_CloakBrowser_FingerprintFlags verifies that CloakBrowser
// fingerprint flags are included when configured.
func TestLaunchScript_CloakBrowser_FingerprintFlags(t *testing.T) {
	script := buildLaunchScript("cloakbrowser", BrowserContainerConfig{
		ChromePath:          "cloakbrowser",
		FingerprintSeed:     "42",
		FingerprintPlatform: "windows",
	}, 1280, 720)

	if !strings.Contains(script, "--fingerprint 42") {
		t.Error("launch script missing --fingerprint seed")
	}
	if !strings.Contains(script, "--fingerprint-platform windows") {
		t.Error("launch script missing --fingerprint-platform")
	}
}

// TestLaunchScript_ProxyFlag verifies that the proxy flag is included when
// configured, for both engines.
func TestLaunchScript_ProxyFlag(t *testing.T) {
	for _, engine := range []string{"default", "cloakbrowser"} {
		t.Run(engine, func(t *testing.T) {
			cfg := BrowserContainerConfig{Proxy: "socks5://127.0.0.1:1080"}
			if engine == "cloakbrowser" {
				cfg.ChromePath = "cloakbrowser"
			}
			script := buildLaunchScript(engine, cfg, 1280, 720)

			if !strings.Contains(script, "--proxy-server=socks5://127.0.0.1:1080") {
				t.Error("launch script missing proxy flag")
			}
		})
	}
}

// TestLaunchScript_ViewportSize verifies that the viewport dimensions are
// passed to the --window-size flag.
func TestLaunchScript_ViewportSize(t *testing.T) {
	for _, engine := range []string{"default", "cloakbrowser"} {
		t.Run(engine, func(t *testing.T) {
			cfg := BrowserContainerConfig{}
			if engine == "cloakbrowser" {
				cfg.ChromePath = "cloakbrowser"
			}
			script := buildLaunchScript(engine, cfg, 1920, 1080)

			if !strings.Contains(script, "--window-size=1920,1080") {
				t.Error("launch script missing correct --window-size")
			}
		})
	}
}

// flattenCommands joins all command slices into a single string for substring matching.
func flattenCommands(cmds [][]string) string {
	var parts []string
	for _, cmd := range cmds {
		parts = append(parts, strings.Join(cmd, " "))
	}
	return strings.Join(parts, "\n")
}

// assertContainsStr fails if s does not contain substr.
func assertContainsStr(t *testing.T, s, substr, desc string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: expected to find %q in commands", desc, substr)
	}
}

// assertContainsCmd fails if no command starts with the given binary.
func assertContainsCmd(t *testing.T, cmds [][]string, binary, desc string) {
	t.Helper()
	for _, cmd := range cmds {
		if len(cmd) > 0 && cmd[0] == binary {
			return
		}
		// Also check inside sh -c commands
		if len(cmd) >= 3 && cmd[0] == "sh" && cmd[1] == "-c" && strings.Contains(cmd[2], binary) {
			return
		}
	}
	t.Errorf("%s: expected a command starting with %q", desc, binary)
}

// assertCmdSequence fails if no command contains all the given tokens in order.
func assertCmdSequence(t *testing.T, cmds [][]string, tokens []string, desc string) {
	t.Helper()
	for _, cmd := range cmds {
		joined := strings.Join(cmd, " ")
		allFound := true
		pos := 0
		for _, tok := range tokens {
			idx := strings.Index(joined[pos:], tok)
			if idx < 0 {
				allFound = false
				break
			}
			pos += idx + len(tok)
		}
		if allFound {
			return
		}
	}
	t.Errorf("%s: expected command containing %v in sequence", desc, tokens)
}
