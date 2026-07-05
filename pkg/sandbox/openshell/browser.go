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
	"strings"
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
// (tunneled through the socat bridge). KasmVNC provides visual handoff
// via websocket on port 6901.
//
// This is the OpenShell equivalent of incus.StartChromiumInContainer.
//
// Important: all exec commands inside the sandbox run as user "sandbox"
// (the OpenShell supervisor demotes from root). The script uses
// HOME=/home/browser to access KasmVNC config and CloakBrowser binary
// without needing su/runuser (which require root privileges).
//
// Prerequisites (baked into the sandbox image):
//   - KasmVNC installed, "browser" user exists, ~/.vnc/ pre-configured
//   - CloakBrowser installed via `python3 -m cloakbrowser install`
//   - socat installed
//   - /home/browser/.cloakbrowser/ is chmod 755 (accessible by sandbox user)
//   - /home/browser/.vnc/ is chmod 755 (accessible by sandbox user)
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
		Command: []string{"sh"},
		Stdin:   strings.NewReader(script),
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

// portToHex returns the uppercase hex representation of a port number,
// matching the format used in /proc/net/tcp (4 hex digits, uppercase).
func portToHex(port int) string {
	return fmt.Sprintf("%04X", port)
}

// buildBrowserLaunchScript generates the shell script that starts KasmVNC,
// CloakBrowser, and the socat CDP bridge inside the sandbox.
//
// All commands run as the sandbox user (no privilege escalation available).
// KasmVNC config and CloakBrowser binary are accessed via HOME=/home/browser
// override — both directories must be chmod 755 in the image.
//
// Dependencies: KasmVNC (vncserver), socat, python3, grep, /proc filesystem.
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

# Helper: check if a process matching a pattern is running.
# Uses /proc (always available) instead of pgrep (needs procps).
proc_running() {
  for p in /proc/[0-9]*/cmdline; do
    [ -f "$p" ] || continue
    if tr '\0' ' ' < "$p" 2>/dev/null | grep -q "$1"; then
      return 0
    fi
  done
  return 1
}

echo "STEP 1: KasmVNC" >&2

# --- 1. Start KasmVNC (X display server + web VNC for visual handoff) ---
# Run as sandbox user with HOME=/home/browser so vncserver finds its config.
# XAUTHORITY must point to a writable location (sandbox user can't write to /home/browser/).
# Use -nolisten local to skip /tmp/.X11-unix (requires root ownership). Xvnc listens on TCP only.
# Skip if already running (idempotent).
export XAUTHORITY=/tmp/.Xauthority
if ! proc_running 'Xvnc.*:%s'; then
  VNC_LOG=/tmp/kasmvnc_start.log
  HOME=/home/browser vncserver :%s -geometry %dx%d -depth 24 -websocketPort %d -DisableBasicAuth -- -nolisten local >"$VNC_LOG" 2>&1 || true
  sleep 1
  if ! proc_running 'Xvnc.*:%s'; then
    echo "KasmVNC failed to start. Log:" >&2
    cat "$VNC_LOG" >&2 2>/dev/null
    exit 1
  fi
fi
export DISPLAY=:%s

echo "STEP 2: Resolve CloakBrowser" >&2

# --- 2. Resolve CloakBrowser binary ---
# CloakBrowser was installed at build time with HOME=/home/browser.
# Force HOME=/home/browser so get_binary_path() resolves correctly.
BROWSER_BIN=$(HOME=/home/browser /usr/bin/python3 -c 'from cloakbrowser.config import get_binary_path; print(get_binary_path())' 2>/dev/null) || true
if [ -z "$BROWSER_BIN" ] || [ ! -f "$BROWSER_BIN" ]; then
  echo "CloakBrowser not at python path (got: '$BROWSER_BIN'), searching..." >&2
  # Fallback: find the chrome binary under known install locations.
  for base in /home/browser/.cloakbrowser /sandbox/.cloakbrowser; do
    candidate=$(find "$base" -name chrome -type f 2>/dev/null | head -1)
    if [ -n "$candidate" ] && [ -f "$candidate" ]; then
      BROWSER_BIN="$candidate"
      echo "Found CloakBrowser at: $BROWSER_BIN" >&2
      break
    fi
  done
  if [ -z "$BROWSER_BIN" ] || [ ! -f "$BROWSER_BIN" ]; then
    echo "No CloakBrowser binary found anywhere" >&2
    ls -la /home/browser/.cloakbrowser/ >&2 2>/dev/null || true
    exit 1
  fi
fi

echo "STEP 3: Launch CloakBrowser (bin=$BROWSER_BIN)" >&2

# --- 3. Launch CloakBrowser ---
# Runs as sandbox user directly. Uses /tmp/chromium as user-data-dir
# (writable by sandbox user; /home/browser/.config may not be).
# Skip if already running (idempotent for reconnection after CDP timeout).
if ! proc_running 'remote-debugging-port'; then
  BROWSER_LOG=/tmp/cloakbrowser.log
  "$BROWSER_BIN" \
    --no-sandbox \
    --test-type \
    --disable-gpu \
    --disable-dev-shm-usage \
    --remote-debugging-port=%d \
    --window-size=%d,%d \
    --user-data-dir=/tmp/chromium \
    --disable-background-timer-throttling \
    --disable-backgrounding-occluded-windows \
    --disable-renderer-backgrounding \
    --disable-blink-features=AutomationControlled \
    --no-first-run \
    --no-default-browser-check \
    --noerrdialogs \
    --disable-features=TranslateUI%s%s \
    about:blank >"$BROWSER_LOG" 2>&1 &
  sleep 2

  echo "STEP 3b: Verify browser running" >&2

  # Verify browser started.
  if ! proc_running 'remote-debugging-port'; then
    echo "CloakBrowser died on startup. Log:" >&2
    cat /tmp/cloakbrowser.log >&2 2>/dev/null
    exit 1
  fi
fi

echo "STEP 4: socat CDP bridge" >&2

# --- 4. Start socat CDP bridge ---
# Skip if already running (idempotent).
if ! proc_running 'socat.*TCP-LISTEN:%d'; then
  socat TCP-LISTEN:%d,fork,bind=0.0.0.0,reuseaddr TCP:127.0.0.1:%d &
fi

echo "STEP 5: Verify CDP port" >&2

# --- 5. Verify CDP port is listening ---
# Use /proc/net/tcp instead of ss (avoids iproute2 dependency).
CDP_READY=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  if grep -qi ':%s ' /proc/net/tcp 2>/dev/null || grep -qi ':%s ' /proc/net/tcp6 2>/dev/null; then
    CDP_READY=1
    break
  fi
  sleep 0.5
done
if [ "$CDP_READY" = "0" ]; then
  echo "CDP port %d not listening after 5s" >&2
  cat /proc/net/tcp >&2 2>/dev/null
  exit 1
fi

echo "DONE: browser ready" >&2
`,
		// KasmVNC proc_running pattern + start + verify
		kasmVNCDisplay,
		kasmVNCDisplay, width, height, kasmPort,
		kasmVNCDisplay,
		// DISPLAY export
		kasmVNCDisplay,
		// CloakBrowser launch
		cdpInternalPort, width, height,
		fingerprintFlags, proxyFlag,
		// socat proc_running + launch
		cdpPort, cdpPort, cdpInternalPort,
		// Verify CDP — port in hex for /proc/net/tcp
		portToHex(cdpPort), portToHex(cdpPort),
		// final error message
		cdpPort,
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
