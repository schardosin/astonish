package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

// defaultUserAgent is a realistic Chrome UA string used to avoid headless
// detection by websites. Matches a recent stable Chrome on Windows.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

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
	Headless          bool          // Default: true
	ChromePath        string        // Empty = auto-download via rod launcher
	ViewportWidth     int           // Default: 1280
	ViewportHeight    int           // Default: 720
	NoSandbox         bool          // For running as root in containers
	UserDataDir       string        // Browser profile dir. Default: ~/.config/astonish/browser/
	NavigationTimeout time.Duration // Max time to wait for page load (default: 30s)
}

// DefaultConfig returns a BrowserConfig with sensible defaults.
// NoSandbox is enabled automatically when running as root (uid 0),
// because Chrome refuses to use its sandbox when running as root.
// UserDataDir defaults to ~/.config/astonish/browser/ so that login
// sessions, cookies, and site data persist across restarts.
func DefaultConfig() BrowserConfig {
	return BrowserConfig{
		Headless:          true,
		ViewportWidth:     1280,
		ViewportHeight:    720,
		NoSandbox:         os.Getuid() == 0,
		UserDataDir:       defaultProfileDir(),
		NavigationTimeout: 30 * time.Second,
	}
}

// OverrideConfig applies optional overrides to the default config.
// Zero values are ignored (the default is preserved). This is used by the
// launcher to merge user config from config.yaml with sensible defaults.
func OverrideConfig(headless *bool, viewportWidth, viewportHeight int, noSandbox *bool, chromePath, userDataDir string, navigationTimeoutSec int) BrowserConfig {
	cfg := DefaultConfig()
	if headless != nil {
		cfg.Headless = *headless
	}
	if viewportWidth > 0 {
		cfg.ViewportWidth = viewportWidth
	}
	if viewportHeight > 0 {
		cfg.ViewportHeight = viewportHeight
	}
	if noSandbox != nil {
		cfg.NoSandbox = *noSandbox
	}
	if chromePath != "" {
		cfg.ChromePath = chromePath
	}
	if userDataDir != "" {
		cfg.UserDataDir = userDataDir
	}
	if navigationTimeoutSec > 0 {
		cfg.NavigationTimeout = time.Duration(navigationTimeoutSec) * time.Second
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

// Manager manages a singleton headless browser instance. The browser is
// launched lazily on first tool invocation and cleaned up when the session ends.
type Manager struct {
	mu        sync.Mutex
	browser   *rod.Browser
	incognito *rod.Browser // ephemeral context with isolated cookies/storage (nil until requested)
	launch    *launcher.Launcher
	config    BrowserConfig
	pages     map[proto.TargetTargetID]*PageState
	pagesMu   sync.RWMutex
	activePg  *rod.Page // most recently active page
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
	}
}

// GetOrLaunch returns the current browser, launching it if necessary.
// On first launch, the browser is configured with anti-detection measures
// (realistic user-agent, disabled automation flags) so that websites serve
// normal content instead of blank/blocked pages.
func (m *Manager) GetOrLaunch() (*rod.Browser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser != nil {
		return m.browser, nil
	}

	l := launcher.New().
		Headless(m.config.Headless).
		NoSandbox(m.config.NoSandbox).
		// Remove the default --enable-automation flag that rod sets.
		// This flag adds "Chrome is being controlled by automated test software"
		// and sets navigator.webdriver = true.
		Delete("enable-automation").
		// Prevent Blink from exposing AutomationControlled feature, which
		// websites check via navigator.webdriver and other signals.
		Set(flags.Flag("disable-blink-features"), "AutomationControlled")

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

	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	b := rod.New().ControlURL(u).NoDefaultDevice()
	if err := b.Connect(); err != nil {
		l.Kill()
		return nil, fmt.Errorf("failed to connect to browser: %w", err)
	}

	m.browser = b
	m.launch = l

	return b, nil
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
// This sets a realistic User-Agent (including Client Hints), disables the
// navigator.webdriver flag, and patches common fingerprint checks.
func (m *Manager) applyStealthToPage(pg *rod.Page) {
	// 1. Override User-Agent at the CDP level. This controls both the HTTP
	//    User-Agent header and the navigator.userAgent JS property.
	_ = proto.NetworkSetUserAgentOverride{
		UserAgent:      defaultUserAgent,
		AcceptLanguage: "en-US,en;q=0.9",
		Platform:       defaultPlatform,
		UserAgentMetadata: &proto.EmulationUserAgentMetadata{
			Brands: []*proto.EmulationUserAgentBrandVersion{
				{Brand: "Not_A Brand", Version: "8"},
				{Brand: "Chromium", Version: "131"},
				{Brand: "Google Chrome", Version: "131"},
			},
			FullVersionList: []*proto.EmulationUserAgentBrandVersion{
				{Brand: "Not_A Brand", Version: "8.0.0.0"},
				{Brand: "Chromium", Version: "131.0.0.0"},
				{Brand: "Google Chrome", Version: "131.0.0.0"},
			},
			Platform:        "Windows",
			PlatformVersion: "10.0.0",
			Architecture:    "x86",
			Model:           "",
			Mobile:          false,
		},
	}.Call(pg)

	// 2. Inject JS that runs before any page script to patch fingerprint leaks.
	//    - navigator.webdriver → undefined (the #1 headless detection check)
	//    - navigator.plugins → non-empty array (headless has 0 plugins)
	//    - navigator.languages → realistic value
	//    - window.chrome → present (headless Chromium omits this)
	_, _ = pg.EvalOnNewDocument(`
		Object.defineProperty(navigator, 'webdriver', {
			get: () => undefined,
		});
		Object.defineProperty(navigator, 'plugins', {
			get: () => [1, 2, 3, 4, 5],
		});
		Object.defineProperty(navigator, 'languages', {
			get: () => ['en-US', 'en'],
		});
		if (!window.chrome) {
			window.chrome = { runtime: {} };
		}
	`)
}

// ensurePageState creates a PageState for the page if one doesn't exist and
// attaches event listeners. Also applies stealth (user-agent, webdriver override)
// so that every page presents as a normal browser. Must be called with m.mu held.
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

	// Attach event listeners for console, network, errors
	go pg.EachEvent(
		func(e *proto.RuntimeConsoleAPICalled) {
			var text string
			for _, arg := range e.Args {
				if text != "" {
					text += " "
				}
				if arg.Value.Nil() {
					if arg.Description != "" {
						text += arg.Description
					} else {
						text += string(arg.Type)
					}
				} else {
					text += arg.Value.String()
				}
			}
			ps.Console.Add(ConsoleMessage{
				Level:     string(e.Type),
				Text:      text,
				Timestamp: timestampToTime(float64(e.Timestamp)),
			})
		},
		func(e *proto.RuntimeExceptionThrown) {
			msg := ""
			if e.ExceptionDetails.Exception != nil {
				msg = e.ExceptionDetails.Exception.Description
			}
			if msg == "" {
				msg = e.ExceptionDetails.Text
			}
			ps.Errors.Add(PageError{
				Message:   msg,
				Timestamp: timestampToTime(float64(e.Timestamp)),
			})
		},
		func(e *proto.NetworkRequestWillBeSent) {
			ps.Network.Add(NetworkRequest{
				Method:       e.Request.Method,
				URL:          e.Request.URL,
				ResourceType: string(e.Type),
				Timestamp:    timestampToTime(float64(e.Timestamp)),
			})
		},
		func(e *proto.NetworkResponseReceived) {
			// Update the most recent matching request with response info
			ps.Network.UpdateLast(func(nr *NetworkRequest) bool {
				if nr.URL == e.Response.URL {
					nr.Status = e.Response.Status
					return true
				}
				return false
			})
		},
	)()
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
	m.activePg = nil

	m.pagesMu.Lock()
	m.pages = make(map[proto.TargetTargetID]*PageState)
	m.pagesMu.Unlock()
}
