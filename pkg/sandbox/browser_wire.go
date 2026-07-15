package sandbox

import (
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/schardosin/astonish/pkg/browser"
	incus "github.com/schardosin/astonish/pkg/sandbox/incus"
)

// WireIncusBrowserManager configures mgr so Chromium + KasmVNC run inside the
// session container (same path Studio chat uses).
//
// Host-only ChromePath values (macOS/Windows binaries) are treated as
// "default" for the container: the sandbox installs Linux Chromium inside the
// session. Returning false here previously left SandboxEnabled off and drills
// fell through to host Chrome, which cannot reach container localhost services.
func WireIncusBrowserManager(mgr *browser.Manager, client *IncusClient, touchActivity func(sessionID string)) bool {
	if mgr == nil || client == nil {
		return false
	}
	cfg := mgr.Config()
	engine := incus.DetectBrowserEngine(incus.BrowserContainerConfig{
		ChromePath: cfg.ChromePath,
	})
	containerChromePath := cfg.ChromePath
	if !incus.IsContainerCompatibleEngine(engine) {
		slog.Info("browser ChromePath is host-only; using default Chromium in sandbox container",
			"component", "browser-wire",
			"chrome_path", cfg.ChromePath,
			"detected_engine", engine,
		)
		containerChromePath = ""
	}

	bCfg := incus.BrowserContainerConfig{
		ViewportWidth:       cfg.ViewportWidth,
		ViewportHeight:      cfg.ViewportHeight,
		KasmVNCPort:         cfg.KasmVNCPort,
		KasmVNCPassword:     cfg.KasmVNCPassword,
		Proxy:               cfg.Proxy,
		ChromePath:          containerChromePath,
		FingerprintSeed:     cfg.FingerprintSeed,
		FingerprintPlatform: cfg.FingerprintPlatform,
	}
	mgr.SandboxEnabled = true
	mgr.ContainerResolveFunc = func(sessionID string) (string, string, error) {
		containerName := SessionContainerName(sessionID)
		if !client.IsRunning(containerName) {
			return "", "", fmt.Errorf("session container %q is not running", containerName)
		}
		ip, err := client.GetContainerIPv4(containerName)
		if err != nil {
			return "", "", fmt.Errorf("failed to get IP for session container %q: %w", containerName, err)
		}
		return containerName, ip, nil
	}
	mgr.ContainerStartBrowserFunc = func(containerName string) (io.Closer, error) {
		return nil, incus.StartChromiumInContainer(client, containerName, bCfg)
	}
	mgr.ContainerDialFunc = func(containerName string, port int) (net.Conn, error) {
		dialer := &incus.ContainerDialer{Client: client}
		return dialer.Dial(containerName, port)
	}
	mgr.ContainerStartRecordingFunc = func(containerName, display string, width, height int, outPath string) (func() error, error) {
		return incus.StartRecordingInContainer(client, containerName, display, width, height, outPath)
	}
	if touchActivity != nil {
		mgr.ActivityTouchFunc = touchActivity
	}
	return true
}
