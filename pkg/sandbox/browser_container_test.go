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
