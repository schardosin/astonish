package browser

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// defaultPlatform reported to websites via navigator.platform / CDP.
const defaultPlatform = "Win32"

// timestampToTime converts a CDP timestamp (seconds since epoch as float64)
// to a Go time.Time.
func timestampToTime(ts float64) time.Time {
	if ts == 0 {
		return time.Now()
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

// BrowserConfig configures the managed browser instance.
type BrowserConfig struct {
	Headless          bool          // Default: false (headed with Xvfb for stealth)
	ChromePath        string        // Empty = auto-download via rod launcher
	ViewportWidth     int           // Default: 1280
	ViewportHeight    int           // Default: 720
	NoSandbox         bool          // For running as root in containers
	UserDataDir       string        // Browser profile dir. Default: ~/.config/astonish/browser/
	NavigationTimeout time.Duration // Max time to wait for page load (default: 30s)
	Proxy             string        // HTTP/SOCKS proxy URL. Empty = direct connection.
	RemoteCDPURL      string        // External CDP endpoint. When set, skip local launch.

	// CloakBrowser fingerprint control (only effective with CloakBrowser binary)
	FingerprintSeed     string // Deterministic fingerprint seed (e.g. "42000")
	FingerprintPlatform string // OS to spoof: "windows", "macos", "linux"

	// Handoff (human-in-the-loop)
	HandoffBindAddress string // Network bind address for CDP handoff. Default: "127.0.0.1"
	HandoffPort        int    // TCP port for CDP handoff proxy. Default: 9222
}

// HandoffCfg holds resolved handoff configuration for external use.
type HandoffCfg struct {
	BindAddress string
	Port        int
}

// DefaultConfig returns a BrowserConfig with sensible defaults.
// Headless defaults to false for better anti-detection (headed mode with Xvfb).
// NoSandbox is enabled automatically when running as root (uid 0),
// because Chrome refuses to use its sandbox when running as root.
// UserDataDir defaults to ~/.config/astonish/browser/ so that login
// sessions, cookies, and site data persist across restarts.
func DefaultConfig() BrowserConfig {
	return BrowserConfig{
		Headless:          false,
		ViewportWidth:     1280,
		ViewportHeight:    720,
		NoSandbox:         os.Getuid() == 0,
		UserDataDir:       defaultProfileDir(),
		NavigationTimeout: 30 * time.Second,
	}
}

// ConfigOverrides holds optional user settings from config.yaml that override
// the browser defaults. Pointer fields (nil = use default) and zero-value
// fields (empty string / 0 = use default) follow the same convention as
// config.BrowserAppConfig.
type ConfigOverrides struct {
	Headless            *bool
	ViewportWidth       int
	ViewportHeight      int
	NoSandbox           *bool
	ChromePath          string
	UserDataDir         string
	NavigationTimeout   int // seconds; 0 = use default
	Proxy               string
	RemoteCDPURL        string
	HandoffBindAddress  string
	HandoffPort         int
	FingerprintSeed     string
	FingerprintPlatform string
}

// OverrideConfig applies optional overrides to the default config.
// Zero/nil values are ignored (the default is preserved). This is used by the
// launcher to merge user config from config.yaml with sensible defaults.
func OverrideConfig(o ConfigOverrides) BrowserConfig {
	cfg := DefaultConfig()
	if o.Headless != nil {
		cfg.Headless = *o.Headless
	}
	if o.ViewportWidth > 0 {
		cfg.ViewportWidth = o.ViewportWidth
	}
	if o.ViewportHeight > 0 {
		cfg.ViewportHeight = o.ViewportHeight
	}
	if o.NoSandbox != nil {
		cfg.NoSandbox = *o.NoSandbox
	}
	if o.ChromePath != "" {
		cfg.ChromePath = o.ChromePath
	}
	if o.UserDataDir != "" {
		cfg.UserDataDir = o.UserDataDir
	}
	if o.NavigationTimeout > 0 {
		cfg.NavigationTimeout = time.Duration(o.NavigationTimeout) * time.Second
	}
	if o.Proxy != "" {
		cfg.Proxy = o.Proxy
	}
	if o.RemoteCDPURL != "" {
		cfg.RemoteCDPURL = o.RemoteCDPURL
	}
	if o.HandoffBindAddress != "" {
		cfg.HandoffBindAddress = o.HandoffBindAddress
	}
	if o.HandoffPort > 0 {
		cfg.HandoffPort = o.HandoffPort
	}
	if o.FingerprintSeed != "" {
		cfg.FingerprintSeed = o.FingerprintSeed
	}
	if o.FingerprintPlatform != "" {
		cfg.FingerprintPlatform = o.FingerprintPlatform
	}
	return cfg
}

// defaultProfileDir returns the default persistent browser profile directory.
// Falls back to empty string (temp dir) if the config directory can't be resolved.
func defaultProfileDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "astonish", "browser")
}

// Manager manages a singleton browser instance. The browser is
// launched lazily on first tool invocation and cleaned up when the session ends.
// By default, Chrome runs in headed mode (with Xvfb on Linux) for maximum
// stealth. Falls back to headless if Xvfb is unavailable.
type Manager struct {
	mu        sync.Mutex
	browser   *rod.Browser
	incognito *rod.Browser // ephemeral context with isolated cookies/storage (nil until requested)
	launch    *launcher.Launcher
	config    BrowserConfig
	pages     map[proto.TargetTargetID]*PageState
	pagesMu   sync.RWMutex
	activePg  *rod.Page // most recently active page
	cdpURL    string    // CDP WebSocket endpoint URL (set after launch)
	xvfb      *Xvfb     // virtual display for headed mode on Linux (nil if not used)
	logger    *log.Logger

	// Detected Chrome version (set after launch).
	chromeVersion string

	// Handoff state (human-in-the-loop). The handoff server persists across
	// tool calls so that browser_request_human can return immediately and
	// browser_handoff_complete can wait later.
	handoff       *HandoffServer
	handoffReason string
}

// NewManager creates a Manager with the given config. The browser is NOT
// launched until GetOrLaunch is called.
func NewManager(cfg BrowserConfig) *Manager {
	if cfg.ViewportWidth == 0 {
		cfg.ViewportWidth = 1280
	}
	if cfg.ViewportHeight == 0 {
		cfg.ViewportHeight = 720
	}
	if cfg.NavigationTimeout == 0 {
		cfg.NavigationTimeout = 30 * time.Second
	}
	return &Manager{
		config: cfg,
		pages:  make(map[proto.TargetTargetID]*PageState),
		logger: log.New(os.Stderr, "[browser] ", log.LstdFlags),
	}
}

// GetOrLaunch returns the current browser, launching it if necessary.
// On first launch, the browser is configured with anti-detection measures:
//   - Headed mode by default (with Xvfb on Linux) for realistic fingerprint
//   - go-rod/stealth JS evasions on every page
//   - Realistic User-Agent and Client Hints matched to actual Chrome version
//   - Automation flags stripped
//
// If headed mode fails (no display, no Xvfb), falls back to headless with a warning.
func (m *Manager) GetOrLaunch() (*rod.Browser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser != nil {
		return m.browser, nil
	}

	// Remote CDP mode: connect to an external browser instead of launching locally.
	// This is used with anti-detect browsers (AdsPower, Browserless, etc.) that
	// handle fingerprint spoofing at the binary level.
	if m.config.RemoteCDPURL != "" {
		return m.connectRemote()
	}

	headless := m.config.Headless

	// If headed mode is requested (default), ensure a display is available.
	if !headless {
		headless = m.ensureDisplay()
	}

	l := launcher.New().
		Headless(headless).
		NoSandbox(m.config.NoSandbox).
		// Remove the default --enable-automation flag that rod sets.
		// This flag adds "Chrome is being controlled by automated test software"
		// and sets navigator.webdriver = true.
		Delete("enable-automation").
		// Remove flags that signal automation. Normal Chrome doesn't set these;
		// their presence is detectable by anti-bot systems.
		Delete("disable-popup-blocking").
		Delete("disable-default-apps").
		// Prevent Blink from exposing AutomationControlled feature, which
		// websites check via navigator.webdriver and other signals.
		Set(flags.Flag("disable-blink-features"), "AutomationControlled").
		// Disable Cross-Origin-Opener-Policy enforcement. Google OAuth
		// popups set COOP: same-origin, which prevents the parent page
		// (e.g. Reddit) from accessing window.closed on the popup. This
		// breaks OAuth callback detection. Appending to the existing
		// disable-features list (which rod sets to "site-per-process,TranslateUI").
		Append(flags.Flag("disable-features"), "CrossOriginOpenerPolicy").
		// Prevent Chrome from throttling timers and rendering in background
		// tabs/windows. Important for consistent behavior in headed Xvfb mode.
		Set(flags.Flag("disable-background-timer-throttling")).
		Set(flags.Flag("disable-backgrounding-occluded-windows")).
		Set(flags.Flag("disable-renderer-backgrounding"))

	if m.config.ChromePath != "" {
		l = l.Bin(m.config.ChromePath)
	}
	if m.config.UserDataDir != "" {
		// Ensure the profile directory exists with restricted permissions.
		// Chrome writes cookies, session tokens, and localStorage here.
		if err := os.MkdirAll(m.config.UserDataDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create browser profile dir: %w", err)
		}
		l = l.UserDataDir(m.config.UserDataDir)
	}
	if m.config.Proxy != "" {
		l = l.Set(flags.Flag("proxy-server"), m.config.Proxy)
	}

	// CloakBrowser fingerprint flags. These are silently ignored by stock
	// Chromium, so it's safe to set them unconditionally when configured.
	if m.config.FingerprintSeed != "" {
		l = l.Set(flags.Flag("fingerprint"), m.config.FingerprintSeed)
	}
	if m.config.FingerprintPlatform != "" {
		l = l.Set(flags.Flag("fingerprint-platform"), m.config.FingerprintPlatform)
	}

	u, err := l.Launch()
	if err != nil {
		// If headed launch failed, try headless as fallback.
		if !headless {
			m.logger.Printf("Headed launch failed (%v), falling back to headless", err)
			m.stopXvfb()
			l = l.Headless(true)
			u, err = l.Launch()
		}
		if err != nil {
			return nil, fmt.Errorf("failed to launch browser: %w", err)
		}
	}

	b := rod.New().ControlURL(u).NoDefaultDevice()
	if err := b.Connect(); err != nil {
		l.Kill()
		m.stopXvfb()
		return nil, fmt.Errorf("failed to connect to browser: %w", err)
	}

	m.browser = b
	m.launch = l
	m.cdpURL = u

	// Detect Chrome version for dynamic UA matching.
	m.detectChromeVersion()

	return b, nil
}

// connectRemote connects to an external browser via CDP WebSocket endpoint.
// Used for anti-detect browsers that handle fingerprinting at the binary level.
// No local Chrome is launched; the external browser is fully managed externally.
// Stealth JS evasions and WebGL patches are still applied to pages since they
// don't hurt and provide an extra layer of protection.
// Must be called with m.mu held.
func (m *Manager) connectRemote() (*rod.Browser, error) {
	m.logger.Printf("Connecting to remote browser at %s", m.config.RemoteCDPURL)

	b := rod.New().ControlURL(m.config.RemoteCDPURL).NoDefaultDevice()
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to remote browser at %s: %w", m.config.RemoteCDPURL, err)
	}

	m.browser = b
	m.cdpURL = m.config.RemoteCDPURL

	// Detect version from the remote browser for UA matching.
	m.detectChromeVersion()

	m.logger.Printf("Connected to remote browser (version: %s)", m.chromeVersion)
	return b, nil
}

// ensureDisplay ensures a display is available for headed mode.
// On Linux without $DISPLAY, it attempts to start Xvfb.
// Returns true if we should fall back to headless, false if display is ready.
func (m *Manager) ensureDisplay() bool {
	// macOS and Windows have native displays (or the user is running with a GUI).
	if runtime.GOOS != "linux" {
		return false
	}

	// If DISPLAY is already set, a display server is running.
	if os.Getenv("DISPLAY") != "" {
		return false
	}

	// No display available. Try to start Xvfb.
	xvfb := NewXvfb(m.logger)
	if err := xvfb.Start(m.config.ViewportWidth, m.config.ViewportHeight); err != nil {
		m.logger.Printf("WARNING: Cannot start Xvfb (%v). Falling back to headless mode.", err)
		m.logger.Printf("For better stealth against strict sites, install xvfb: apt install xvfb")
		return true // fall back to headless
	}

	m.xvfb = xvfb
	return false // display is ready
}

// detectChromeVersion queries the browser for its actual version and constructs
// matching UA string and Client Hints. This prevents mismatches between the
// reported UA and the actual Chrome binary version, which is a detection signal.
func (m *Manager) detectChromeVersion() {
	if m.browser == nil {
		return
	}

	version, err := m.browser.Version()
	if err != nil {
		return
	}

	// version.Product is like "Chrome/131.0.6778.204"
	m.chromeVersion = version.Product
}

// stopXvfb stops the Xvfb process if one was started by us.
func (m *Manager) stopXvfb() {
	if m.xvfb != nil {
		m.xvfb.Stop()
		m.xvfb = nil
	}
}

// CDPURL returns the Chrome DevTools Protocol WebSocket endpoint URL.
// Returns empty string if the browser has not been launched yet.
func (m *Manager) CDPURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cdpURL
}

// HandoffConfig returns the resolved handoff configuration with defaults applied.
func (m *Manager) HandoffConfig() HandoffCfg {
	bind := m.config.HandoffBindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	port := m.config.HandoffPort
	if port == 0 {
		port = 9222
	}
	return HandoffCfg{BindAddress: bind, Port: port}
}

// StartHandoff creates and starts a handoff server, storing it on the Manager
// so it persists across tool calls. Returns the HandoffInfo (with listen address)
// and an error. If a handoff is already active, returns an error.
func (m *Manager) StartHandoff(opts HandoffOpts) (*HandoffInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.handoff != nil && m.handoff.IsActive() {
		return nil, fmt.Errorf("a browser handoff session is already active")
	}

	h := NewHandoffServer(nil)
	info, err := h.Start(opts)
	if err != nil {
		return nil, err
	}

	m.handoff = h
	m.handoffReason = opts.Reason
	return info, nil
}

// WaitHandoff blocks until the active handoff session completes or the context
// is cancelled. Returns an error if no handoff is active. Stops and cleans up
// the handoff server after the wait completes.
func (m *Manager) WaitHandoff(ctx context.Context) error {
	m.mu.Lock()
	h := m.handoff
	m.mu.Unlock()

	if h == nil || !h.IsActive() {
		return fmt.Errorf("no active handoff session")
	}

	err := h.WaitForCompletion(ctx)

	// Clean up the handoff server after completion (or timeout)
	m.mu.Lock()
	if m.handoff == h {
		_ = m.handoff.Stop()
		m.handoff = nil
		m.handoffReason = ""
	}
	m.mu.Unlock()

	return err
}

// StopHandoff stops any active handoff session. Safe to call when no handoff
// is running.
func (m *Manager) StopHandoff() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.handoff != nil {
		_ = m.handoff.Stop()
		m.handoff = nil
		m.handoffReason = ""
	}
}

// HandoffActive returns true if a handoff session is currently running.
func (m *Manager) HandoffActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handoff != nil && m.handoff.IsActive()
}

// CurrentPage returns the most recently active page, creating one if none exists.
func (m *Manager) CurrentPage() (*rod.Page, error) {
	b, err := m.GetOrLaunch()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activePg != nil {
		// Verify the page is still open
		_, infoErr := m.activePg.Info()
		if infoErr == nil {
			return m.activePg, nil
		}
		// Page was closed, fall through
		m.activePg = nil
	}

	// Try to get an existing page
	pages, err := b.Pages()
	if err == nil && !pages.Empty() {
		pg := pages.Last()
		m.activePg = pg
		m.ensurePageState(pg)
		return pg, nil
	}

	// Create a new blank page with configured viewport
	pg, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}

	if err := pg.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  m.config.ViewportWidth,
		Height: m.config.ViewportHeight,
	}); err != nil {
		return nil, fmt.Errorf("failed to set viewport: %w", err)
	}

	m.activePg = pg
	m.ensurePageState(pg)
	return pg, nil
}

// SetActivePage sets a specific page as the active page.
func (m *Manager) SetActivePage(pg *rod.Page) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activePg = pg
	m.ensurePageState(pg)
}

// NewIncognitoPage creates a new page in an isolated (incognito) browser context.
// The page has its own cookie jar and storage — it cannot see the persistent
// profile's login sessions. Use this for testing login flows, verifying
// unauthenticated behavior, or browsing without leaking personal cookies.
// The incognito page becomes the active page.
func (m *Manager) NewIncognitoPage() (*rod.Page, error) {
	b, err := m.GetOrLaunch()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Lazily create the incognito context (one per session is enough)
	if m.incognito == nil {
		inc, err := b.Incognito()
		if err != nil {
			return nil, fmt.Errorf("failed to create incognito context: %w", err)
		}
		m.incognito = inc
	}

	pg, err := m.incognito.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("failed to create incognito page: %w", err)
	}

	if err := pg.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  m.config.ViewportWidth,
		Height: m.config.ViewportHeight,
	}); err != nil {
		return nil, fmt.Errorf("failed to set viewport: %w", err)
	}

	m.activePg = pg
	m.ensurePageState(pg)
	return pg, nil
}

// GetPage returns a page by its CDP target ID.
func (m *Manager) GetPage(targetID proto.TargetTargetID) (*rod.Page, error) {
	b, err := m.GetOrLaunch()
	if err != nil {
		return nil, err
	}

	pg, err := b.PageFromTarget(targetID)
	if err != nil {
		return nil, fmt.Errorf("page not found for target %s: %w", targetID, err)
	}
	return pg, nil
}

// applyStealthToPage configures anti-detection measures on a page so that
// websites see a normal browser instead of a headless automation tool.
// Uses go-rod/stealth for comprehensive JS evasions and sets a realistic
// User-Agent with Client Hints matched to the actual Chrome version.
func (m *Manager) applyStealthToPage(pg *rod.Page) {
	// 1. Inject go-rod/stealth JS evasions (runs before any page script).
	// Covers: navigator.webdriver, plugins (realistic PluginArray), languages,
	// window.chrome (csi, loadTimes, runtime), media codecs,
	// hardwareConcurrency, permissions, WebGL vendor/renderer,
	// outerWidth/outerHeight, iframe contentWindow, and more.
	if _, err := pg.EvalOnNewDocument(stealth.JS); err != nil {
		m.logger.Printf("WARNING: failed to inject stealth JS: %v", err)
	}

	// 2. Override User-Agent at the CDP level using actual Chrome version.
	// This controls both the HTTP User-Agent header and the navigator.userAgent
	// JS property, plus Client Hints (Sec-CH-UA, Sec-CH-UA-Platform, etc.).
	ua, majorVersion := m.buildUserAgent()
	if err := (proto.NetworkSetUserAgentOverride{
		UserAgent:      ua,
		AcceptLanguage: "en-US,en;q=0.9",
		Platform:       defaultPlatform,
		UserAgentMetadata: &proto.EmulationUserAgentMetadata{
			Brands: []*proto.EmulationUserAgentBrandVersion{
				{Brand: "Not_A Brand", Version: "8"},
				{Brand: "Chromium", Version: majorVersion},
				{Brand: "Google Chrome", Version: majorVersion},
			},
			FullVersionList: []*proto.EmulationUserAgentBrandVersion{
				{Brand: "Not_A Brand", Version: "8.0.0.0"},
				{Brand: "Chromium", Version: majorVersion + ".0.0.0"},
				{Brand: "Google Chrome", Version: majorVersion + ".0.0.0"},
			},
			Platform:        "Windows",
			PlatformVersion: "10.0.0",
			Architecture:    "x86",
			Model:           "",
			Mobile:          false,
		},
	}).Call(pg); err != nil {
		m.logger.Printf("WARNING: failed to set User-Agent override: %v", err)
	}

	// 3. Inject WebGL consistency patches to align capability values and
	// extension lists with the "Intel Iris OpenGL Engine" identity claimed
	// by stealth.JS. Without this, fingerprinters detect SwiftShader behavior
	// (fewer extensions, different MAX_VIEWPORT_DIMS, etc.) contradicting the
	// spoofed renderer string.
	if _, err := pg.EvalOnNewDocument(webglConsistencyJS); err != nil {
		m.logger.Printf("WARNING: failed to inject WebGL consistency JS: %v", err)
	}
}

// buildUserAgent returns a UA string and major version number based on the
// actual Chrome version detected at launch. Falls back to a reasonable default
// if the version is unknown.
func (m *Manager) buildUserAgent() (ua string, majorVersion string) {
	majorVersion = "131"
	fullVersion := "131.0.0.0"

	if m.chromeVersion != "" {
		// chromeVersion is like "Chrome/131.0.6778.204" or "HeadlessChrome/131.0.6778.204"
		parts := m.chromeVersion
		if idx := len("Chrome/"); len(parts) > idx {
			for i, c := range parts {
				if c == '/' {
					parts = parts[i+1:]
					break
				}
			}
			fullVersion = parts
			// Extract major version (everything before first dot).
			for i, c := range parts {
				if c == '.' {
					majorVersion = parts[:i]
					break
				}
			}
		}
	}

	ua = fmt.Sprintf(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
		fullVersion,
	)
	return ua, majorVersion
}

// ensurePageState creates a PageState for the page if one doesn't exist.
// Applies stealth (user-agent, webdriver override) so that every page presents
// as a normal browser. Must be called with m.mu held.
//
// Event listeners (console, network, errors) are NOT attached here to avoid
// triggering Runtime.enable, which is a primary bot-detection signal. Listeners
// are attached lazily via PageState.AttachListeners() when explicitly requested.
func (m *Manager) ensurePageState(pg *rod.Page) {
	info, err := pg.Info()
	if err != nil {
		return
	}
	targetID := info.TargetID

	m.pagesMu.Lock()
	defer m.pagesMu.Unlock()
	if _, ok := m.pages[targetID]; ok {
		return
	}

	// Apply anti-detection overrides before any navigation happens on this page.
	m.applyStealthToPage(pg)

	ps := NewPageState()
	m.pages[targetID] = ps
}

// PageStateFor returns the PageState for a given page.
func (m *Manager) PageStateFor(pg *rod.Page) *PageState {
	info, err := pg.Info()
	if err != nil {
		return nil
	}
	m.pagesMu.RLock()
	defer m.pagesMu.RUnlock()
	return m.pages[info.TargetID]
}

// IsRunning returns true if a browser is currently connected.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.browser != nil
}

// NavigationTimeout returns the configured navigation timeout.
func (m *Manager) NavigationTimeout() time.Duration {
	return m.config.NavigationTimeout
}

// Cleanup closes the browser and kills the process. The persistent profile
// directory (cookies, localStorage, etc.) is preserved for future sessions.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop any active handoff session
	if m.handoff != nil {
		_ = m.handoff.Stop()
		m.handoff = nil
		m.handoffReason = ""
	}

	// Close incognito context first (it's a child of the main browser)
	if m.incognito != nil {
		_ = m.incognito.Close()
		m.incognito = nil
	}
	if m.browser != nil {
		_ = m.browser.Close()
		m.browser = nil
	}
	if m.launch != nil {
		m.launch.Kill()
		// Only clean up the user data dir if we're using a temp profile.
		// Persistent profiles must survive across restarts.
		if m.config.UserDataDir == "" {
			m.launch.Cleanup()
		}
		m.launch = nil
	}

	// Stop Xvfb virtual display if we started one.
	m.stopXvfb()

	m.activePg = nil

	m.pagesMu.Lock()
	m.pages = make(map[proto.TargetTargetID]*PageState)
	m.pagesMu.Unlock()
}
