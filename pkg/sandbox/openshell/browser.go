// Package openshell — browser.go implements browser-in-sandbox support for
// the OpenShell backend. CloakBrowser + KasmVNC are pre-installed in the
// sandbox image (docker/sandbox-openshell/Dockerfile); this file provides
// the runtime wiring to launch them and tunnel CDP connections through the
// gateway's gRPC exec API.
//
// Architecture mirrors pkg/sandbox/incus/browser_container.go +
// pkg/sandbox/incus/tunnel.go but uses the OpenShell GatewayClient instead
// of the Incus exec API.

package openshell

import (
	"context"
	"fmt"
	"net"
	"time"
)

// ---------------------------------------------------------------------------
// Constants (same values as pkg/sandbox/incus/browser_container.go)
// ---------------------------------------------------------------------------

const (
	// cdpPort is the externally-reachable CDP port inside the sandbox.
	// A socat bridge forwards from 0.0.0.0:cdpPort to 127.0.0.1:cdpInternalPort.
	cdpPort = 9222

	// cdpInternalPort is the port Chromium's --remote-debugging-port binds to
	// (localhost only). Socat bridges this to cdpPort on all interfaces.
	cdpInternalPort = 9223

	// kasmVNCDisplay is the X11 display number used by KasmVNC.
	kasmVNCDisplay = "0"

	// defaultKasmVNCPort is the KasmVNC websocket port (6901 for display :0).
	defaultKasmVNCPort = 6901
)

// ---------------------------------------------------------------------------
// BrowserLaunchConfig
// ---------------------------------------------------------------------------

// BrowserLaunchConfig holds the parameters for launching the browser inside
// an OpenShell sandbox. Passed from the browser Manager's config.
type BrowserLaunchConfig struct {
	ViewportWidth       int
	ViewportHeight      int
	FingerprintSeed     string
	FingerprintPlatform string
	Proxy               string
	KasmVNCPort         int
}

// ---------------------------------------------------------------------------
// StartBrowserInSandbox
// ---------------------------------------------------------------------------

// StartBrowserInSandbox launches CloakBrowser + KasmVNC + socat inside an
// OpenShell sandbox pod. The browser becomes reachable via CDP on port 9222
// (tunneled through the socat bridge).
//
// This is the OpenShell equivalent of incus.StartChromiumInContainer.
//
// Prerequisites (baked into the sandbox image):
//   - KasmVNC installed, "browser" user exists, ~/.vnc/ pre-configured
//   - CloakBrowser installed via `python3 -m cloakbrowser install`
//   - socat installed
func StartBrowserInSandbox(ctx context.Context, gateway GatewayClient, podName string, cfg BrowserLaunchConfig) error {
	width := cfg.ViewportWidth
	if width == 0 {
		width = 1280
	}
	height := cfg.ViewportHeight
	if height == 0 {
		height = 720
	}
	kasmPort := cfg.KasmVNCPort
	if kasmPort == 0 {
		kasmPort = defaultKasmVNCPort
	}

	script := buildBrowserLaunchScript(cfg, width, height, kasmPort)

	resp, err := gateway.ExecCommand(ctx, podName, ExecRequest{
		Command: []string{"sh", "-c", script},
		TTY:     false,
	})
	if err != nil {
		return fmt.Errorf("openshell: start browser in %s: %w", podName, err)
	}
	if resp.ExitCode != 0 {
		return fmt.Errorf("openshell: browser launch in %s exited %d: %s",
			podName, resp.ExitCode, string(resp.Stderr))
	}

	// Allow socat + Chromium time to start listening.
	time.Sleep(2 * time.Second)

	return nil
}

// buildBrowserLaunchScript generates the shell script that starts KasmVNC,
// CloakBrowser, and the socat CDP bridge inside the sandbox.
func buildBrowserLaunchScript(cfg BrowserLaunchConfig, width, height, kasmPort int) string {
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

	return fmt.Sprintf(`#!/bin/sh
set -e

# --- 1. Start KasmVNC (X display server for headed browser) ---
# Skip if already running (idempotent).
if ! pgrep -u browser -f 'Xvnc.*:%s' >/dev/null 2>&1; then
  runuser -l browser -c "vncserver :%s -geometry %dx%d -depth 24 -websocketPort %d -DisableBasicAuth" 2>/dev/null || true
  sleep 1
fi

# Allow any local user to connect to the Xvnc display.
runuser -l browser -c "DISPLAY=:%s xhost +local:" 2>/dev/null || true

# --- 2. Resolve CloakBrowser binary ---
BROWSER_BIN=$(runuser -u browser -- python3 -c "from cloakbrowser.config import get_binary_path; print(get_binary_path())" 2>/dev/null)
if [ -z "$BROWSER_BIN" ] || [ ! -f "$BROWSER_BIN" ]; then
  echo "CloakBrowser binary not found" >&2
  exit 1
fi

# --- 3. Launch CloakBrowser ---
# Skip if already running (idempotent for reconnection after CDP timeout).
if ! pgrep -u browser -f 'chrome.*--remote-debugging-port' >/dev/null 2>&1; then
  BROWSER_LOG=/tmp/cloakbrowser.log
  runuser -l browser -c "DISPLAY=:%s $BROWSER_BIN \
    --no-sandbox \
    --test-type \
    --disable-gpu \
    --disable-dev-shm-usage \
    --remote-debugging-port=%d \
    --window-size=%d,%d \
    --user-data-dir=/home/browser/.config/chromium \
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

  # Verify browser started.
  if ! pgrep -u browser -f 'chrome|chromium' >/dev/null 2>&1; then
    echo "CloakBrowser died on startup. Log:" >&2
    cat /tmp/cloakbrowser.log >&2 2>/dev/null
    exit 1
  fi
fi

# --- 4. Start socat CDP bridge ---
# Skip if already running (idempotent).
if ! pgrep -f 'socat.*TCP-LISTEN:%d' >/dev/null 2>&1; then
  socat TCP-LISTEN:%d,fork,bind=0.0.0.0,reuseaddr TCP:127.0.0.1:%d &
fi

# --- 5. Verify CDP port is listening ---
CDP_READY=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  if ss -tln 2>/dev/null | grep -q ':%d '; then
    CDP_READY=1
    break
  fi
  sleep 0.5
done
if [ "$CDP_READY" = "0" ]; then
  echo "CDP port %d not listening after 5s" >&2
  exit 1
fi
`,
		// KasmVNC pgrep pattern
		kasmVNCDisplay,
		// KasmVNC start
		kasmVNCDisplay, width, height, kasmPort,
		// xhost
		kasmVNCDisplay,
		// CloakBrowser launch
		kasmVNCDisplay, cdpInternalPort, width, height,
		fingerprintFlags, proxyFlag,
		// socat pgrep + launch
		cdpPort, cdpPort, cdpInternalPort,
		// Verify CDP
		cdpPort, cdpPort,
	)
}

// ---------------------------------------------------------------------------
// DialSandboxPort — exec-based TCP tunnel
// ---------------------------------------------------------------------------

// DialSandboxPort creates a net.Conn to host:port inside an OpenShell sandbox
// by running `socat STDIO TCP:127.0.0.1:<port>` through the gateway's
// bidirectional exec stream (gRPC ExecSandboxInteractive).
//
// This is the OpenShell equivalent of incus.DialViaExec. The tunnel works
// regardless of pod-to-pod network connectivity because it routes through
// the gateway's gRPC API.
func DialSandboxPort(ctx context.Context, gateway GatewayClient, podName string, port int) (net.Conn, error) {
	conn, err := gateway.ExecStream(ctx, podName, ExecRequest{
		Command: []string{"socat", "STDIO", fmt.Sprintf("TCP:127.0.0.1:%d", port)},
		TTY:     false,
	})
	if err != nil {
		return nil, fmt.Errorf("openshell: dial %s:%d: %w", podName, port, err)
	}

	return &ExecConn{
		conn:   conn,
		local:  execAddr{pod: podName, port: 0},
		remote: execAddr{pod: podName, port: port},
	}, nil
}

// ---------------------------------------------------------------------------
// ExecConn — net.Conn adapter over ExecStreamConn
// ---------------------------------------------------------------------------

// ExecConn wraps an OpenShell ExecStreamConn as a net.Conn. It tunnels a
// single TCP connection through the gateway's gRPC exec stream (socat STDIO).
//
// Same pattern as pkg/sandbox/incus/tunnel.go:ExecConn.
type ExecConn struct {
	conn   ExecStreamConn
	local  execAddr
	remote execAddr
}

// execAddr is a minimal net.Addr for informational purposes.
type execAddr struct {
	pod  string
	port int
}

func (a execAddr) Network() string { return "openshell-exec" }
func (a execAddr) String() string  { return fmt.Sprintf("%s:%d", a.pod, a.port) }

// Read reads data from the tunneled TCP connection (via socat stdout).
func (c *ExecConn) Read(b []byte) (int, error) {
	return c.conn.Read(b)
}

// Write sends data to the tunneled TCP connection (via socat stdin).
func (c *ExecConn) Write(b []byte) (int, error) {
	return c.conn.Write(b)
}

// Close terminates the tunnel by closing the exec stream.
func (c *ExecConn) Close() error {
	return c.conn.Close()
}

// LocalAddr returns a stub address identifying the tunnel.
func (c *ExecConn) LocalAddr() net.Addr { return c.local }

// RemoteAddr returns a stub address identifying the tunnel target.
func (c *ExecConn) RemoteAddr() net.Addr { return c.remote }

// SetDeadline is a no-op. The underlying gRPC stream does not support deadlines.
func (c *ExecConn) SetDeadline(_ time.Time) error { return nil }

// SetReadDeadline is a no-op.
func (c *ExecConn) SetReadDeadline(_ time.Time) error { return nil }

// SetWriteDeadline is a no-op.
func (c *ExecConn) SetWriteDeadline(_ time.Time) error { return nil }
