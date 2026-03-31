package browser

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// disconnectGrace is how long we wait after all WebSocket connections close
// before auto-signaling done. This gives the user time to reconnect (e.g.
// switching DevTools panels or refreshing).
const disconnectGrace = 10 * time.Second

// HandoffServer exposes the browser's CDP WebSocket endpoint so a human can
// connect with chrome://inspect and interact with the same browser session.
// This enables solving CAPTCHAs, navigating complex flows, and other tasks
// that require human judgment.
type HandoffServer struct {
	mu       sync.Mutex
	listener net.Listener
	http     *http.Server
	active   bool
	doneCh   chan struct{}
	logger   *log.Logger

	// WebSocket connection tracking for auto-done detection.
	// When all connections close, we start a grace timer. If no new
	// connections arrive before it fires, we signal done automatically.
	wsConns       atomic.Int32
	graceMu       sync.Mutex
	graceTimer    *time.Timer
	graceDuration time.Duration // 0 means use disconnectGrace default
}

// HandoffOpts configures a browser handoff session.
type HandoffOpts struct {
	// CDPURL is the rod browser's CDP WebSocket endpoint (ws://...).
	CDPURL string
	// Port to expose the CDP proxy on. Default: 9222.
	Port int
	// BindAddress controls network binding. "127.0.0.1" for local-only,
	// "0.0.0.0" for remote access. Default: "127.0.0.1".
	BindAddress string
	// Timeout is the maximum time to wait for the user. Default: 5 minutes.
	Timeout time.Duration
	// Reason describes why handoff is needed (shown to user).
	Reason string
}

// HandoffInfo describes an active handoff session.
type HandoffInfo struct {
	// InspectURL provides chrome://inspect instructions.
	InspectURL string
	// ListenAddress is where the proxy is listening (e.g. "127.0.0.1:9222").
	ListenAddress string
	// CurrentPageURL is the browser's current page when handoff started.
	CurrentPageURL string
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host // no port in Host header
		}
		return host == "127.0.0.1" || host == "::1" || host == "localhost"
	},
}

// NewHandoffServer creates a new handoff server.
func NewHandoffServer(logger *log.Logger) *HandoffServer {
	if logger == nil {
		logger = log.Default()
	}
	return &HandoffServer{
		logger: logger,
	}
}

// Start begins proxying CDP connections. The server listens on the configured
// address and transparently proxies both HTTP discovery endpoints (/json,
// /json/version, /json/list) and WebSocket connections (/devtools/*) to the
// real Chrome instance launched by rod. Discovery responses are rewritten so
// that webSocketDebuggerUrl fields point back through the proxy, allowing
// chrome://inspect on a remote machine to connect seamlessly.
func (h *HandoffServer) Start(opts HandoffOpts) (*HandoffInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.active {
		return nil, fmt.Errorf("handoff already active")
	}
	if opts.CDPURL == "" {
		return nil, fmt.Errorf("CDP URL is required")
	}

	if opts.Port == 0 {
		opts.Port = 9222
	}
	if opts.BindAddress == "" {
		opts.BindAddress = "127.0.0.1"
	}

	// Extract the internal Chrome HTTP host:port from the rod CDP URL.
	// Rod returns URLs like ws://127.0.0.1:PORT/devtools/browser/UUID.
	// The same host:port serves HTTP discovery at /json, /json/version, etc.
	internalHost, err := cdpURLToHTTPHost(opts.CDPURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CDP URL: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", opts.BindAddress, opts.Port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	h.listener = listener
	h.doneCh = make(chan struct{})
	h.active = true

	actualAddr := listener.Addr().String()

	mux := http.NewServeMux()

	// /json/version — proxy to the real browser and rewrite WebSocket URLs
	mux.HandleFunc("/json/version", h.proxyDiscovery(internalHost, "/json/version"))

	// /json and /json/list — proxy real target lists with rewritten URLs
	mux.HandleFunc("/json", h.proxyDiscovery(internalHost, "/json"))
	mux.HandleFunc("/json/list", h.proxyDiscovery(internalHost, "/json/list"))

	// /handoff/done — user can POST here to signal completion
	mux.HandleFunc("/handoff/done", func(w http.ResponseWriter, r *http.Request) {
		h.logger.Printf("[handoff] /handoff/done hit: method=%s remoteAddr=%s", r.Method, r.RemoteAddr)
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "use POST or GET", http.StatusMethodNotAllowed)
			return
		}
		h.SignalDone()
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "OK — browser control returned to agent")
	})

	// WebSocket proxy — forward any /devtools/* path to the same path on the
	// internal Chrome instance. This handles both browser-level and page-level
	// WebSocket connections that DevTools opens.
	mux.HandleFunc("/devtools/", h.makeWSProxy(internalHost))

	h.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	srv := h.http // capture for goroutine (avoid race with Stop setting h.http = nil)
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			h.logger.Printf("[handoff] HTTP server error: %v", serveErr)
		}
	}()

	h.logger.Printf("[handoff] CDP proxy listening on %s → %s (reason: %s)", actualAddr, internalHost, opts.Reason)

	return &HandoffInfo{
		InspectURL:    fmt.Sprintf("chrome://inspect → Configure → %s", actualAddr),
		ListenAddress: actualAddr,
	}, nil
}

// cdpURLToHTTPHost extracts the host:port from a rod CDP WebSocket URL.
// For example, "ws://127.0.0.1:44519/devtools/browser/abc" returns "127.0.0.1:44519".
func cdpURLToHTTPHost(cdpURL string) (string, error) {
	parsed, err := url.Parse(cdpURL)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("no host in CDP URL %q", cdpURL)
	}
	return parsed.Host, nil
}

// proxyDiscovery returns an HTTP handler that fetches a discovery endpoint
// from the internal Chrome instance and rewrites all webSocketDebuggerUrl
// values to route through the proxy. The rewrite target is derived from the
// incoming request's Host header, so chrome://inspect sees URLs that point
// back to the address it used to connect (e.g. 192.168.1.x:9222), not the
// listener's literal address (e.g. [::]:9222 or 0.0.0.0:9222).
func (h *HandoffServer) proxyDiscovery(internalHost, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		internalURL := fmt.Sprintf("http://%s%s", internalHost, path)

		h.logger.Printf("[handoff] Discovery request: path=%s r.Host=%s internalURL=%s", path, r.Host, internalURL)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(internalURL)
		if err != nil {
			h.logger.Printf("[handoff] Failed to fetch %s: %v", internalURL, err)
			http.Error(w, "failed to reach browser", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "failed to read browser response", http.StatusBadGateway)
			return
		}

		h.logger.Printf("[handoff] Discovery raw response (%s): %s", path, string(body))

		// Use the request's Host header as the rewrite target. This ensures
		// chrome://inspect sees URLs matching the address it connected to
		// (e.g. "192.168.1.100:9222"), not the raw listener address
		// (e.g. "[::]:9222" or "0.0.0.0:9222").
		proxyAddr := r.Host
		rewritten := strings.ReplaceAll(string(body), internalHost, proxyAddr)

		h.logger.Printf("[handoff] Discovery rewritten (%s): internalHost=%s -> proxyAddr=%s result=%s", path, internalHost, proxyAddr, rewritten)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, rewritten)
	}
}

// makeWSProxy returns an HTTP handler that proxies WebSocket connections
// to the internal Chrome instance. The incoming request path (e.g.
// /devtools/browser/UUID or /devtools/page/TARGET_ID) is forwarded to the
// same path on internalHost, so both browser-level and page-level DevTools
// connections work correctly.
//
// Connections are reference-counted. When the last WebSocket closes, a grace
// timer starts. If no new connections arrive within disconnectGrace, the
// handoff is automatically signaled as done.
func (h *HandoffServer) makeWSProxy(internalHost string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Build the target WebSocket URL: same path, but on the internal host
		targetURL := fmt.Sprintf("ws://%s%s", internalHost, r.URL.Path)

		h.logger.Printf("[handoff] WebSocket connect attempt: path=%s targetURL=%s", r.URL.Path, targetURL)

		// Upgrade the incoming connection
		clientConn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			h.logger.Printf("[handoff] Failed to upgrade client WebSocket: %v", err)
			return
		}
		defer clientConn.Close()

		// Connect to the real Chrome CDP endpoint at the same path
		targetConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
		if err != nil {
			h.logger.Printf("[handoff] Failed to connect to CDP endpoint %s: %v", targetURL, err)
			return
		}
		defer targetConn.Close()

		// Track this connection
		h.wsConnected()
		defer h.wsDisconnected()

		h.logger.Printf("[handoff] User connected via DevTools (path: %s)", r.URL.Path)

		// Bidirectional proxy
		errc := make(chan error, 2)

		// client → target
		go func() {
			errc <- proxyWS(clientConn, targetConn)
		}()

		// target → client
		go func() {
			errc <- proxyWS(targetConn, clientConn)
		}()

		// Wait for either direction to close
		<-errc

		h.logger.Printf("[handoff] User disconnected from DevTools (path: %s)", r.URL.Path)
	}
}

// wsConnected increments the WebSocket connection count and cancels any
// pending grace timer (the user reconnected before it fired).
func (h *HandoffServer) wsConnected() {
	count := h.wsConns.Add(1)

	h.graceMu.Lock()
	if h.graceTimer != nil {
		h.graceTimer.Stop()
		h.graceTimer = nil
		h.logger.Printf("[handoff] Grace timer cancelled (user reconnected, %d connections)", count)
	}
	h.graceMu.Unlock()
}

// wsDisconnected decrements the WebSocket connection count. When the count
// reaches zero, starts a grace timer that will signal done if no new
// connections arrive.
func (h *HandoffServer) wsDisconnected() {
	count := h.wsConns.Add(-1)
	if count > 0 {
		return
	}

	h.graceMu.Lock()
	defer h.graceMu.Unlock()

	// Don't start a timer if one is already running or if already done
	if h.graceTimer != nil {
		return
	}

	grace := h.getGraceDuration()
	h.logger.Printf("[handoff] All WebSocket connections closed, starting %s grace timer", grace)
	h.graceTimer = time.AfterFunc(grace, func() {
		h.graceMu.Lock()
		h.graceTimer = nil
		h.graceMu.Unlock()

		h.logger.Printf("[handoff] Grace period expired, auto-signaling done")
		h.SignalDone()
	})
}

// getGraceDuration returns the disconnect grace period, using the default
// if none was explicitly set.
func (h *HandoffServer) getGraceDuration() time.Duration {
	if h.graceDuration > 0 {
		return h.graceDuration
	}
	return disconnectGrace
}

// proxyWS copies WebSocket messages from src to dst until an error occurs.
func proxyWS(src, dst *websocket.Conn) error {
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			return err
		}
		if err := dst.WriteMessage(msgType, data); err != nil {
			return err
		}
	}
}

// WaitForCompletion blocks until the user signals they are done or the
// context is cancelled. Returns nil on normal completion, context error
// on cancellation/timeout.
func (h *HandoffServer) WaitForCompletion(ctx context.Context) error {
	select {
	case <-h.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SignalDone signals that the user has finished interacting with the browser.
// Safe to call multiple times.
func (h *HandoffServer) SignalDone() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active {
		select {
		case <-h.doneCh:
			// Already closed
		default:
			close(h.doneCh)
		}
	}
}

// Stop shuts down the handoff server and releases resources.
func (h *HandoffServer) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return nil
	}

	h.active = false

	// Cancel any pending grace timer
	h.graceMu.Lock()
	if h.graceTimer != nil {
		h.graceTimer.Stop()
		h.graceTimer = nil
	}
	h.graceMu.Unlock()

	// Signal done in case anyone is waiting
	select {
	case <-h.doneCh:
	default:
		close(h.doneCh)
	}

	var firstErr error

	if h.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.http.Shutdown(ctx); err != nil {
			firstErr = err
		}
		h.http = nil
	}

	if h.listener != nil {
		h.listener = nil
	}

	h.logger.Printf("[handoff] CDP proxy stopped")

	return firstErr
}

// IsActive returns whether a handoff session is currently running.
func (h *HandoffServer) IsActive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}
