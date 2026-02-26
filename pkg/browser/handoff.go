package browser

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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
	CheckOrigin: func(_ *http.Request) bool { return true },
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

// Start begins proxying CDP WebSocket connections. The server listens on
// the configured address and proxies all WebSocket traffic to the browser's
// CDP endpoint. It also serves a /json/version endpoint for chrome://inspect
// discovery and a /handoff/done endpoint for signaling completion.
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

	addr := fmt.Sprintf("%s:%d", opts.BindAddress, opts.Port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	h.listener = listener
	h.doneCh = make(chan struct{})
	h.active = true

	mux := http.NewServeMux()

	// /json/version — chrome://inspect uses this for discovery
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"Browser":"Astonish Browser Handoff","Protocol-Version":"1.3","webSocketDebuggerUrl":"%s"}`, opts.CDPURL)
	})

	// /json — list targets (chrome://inspect also queries this)
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a minimal target list pointing to the real CDP endpoint
		fmt.Fprintf(w, `[{"description":"","devtoolsFrontendUrl":"","id":"page","title":"Astonish Browser","type":"page","url":"","webSocketDebuggerUrl":"%s"}]`, opts.CDPURL)
	})

	// /handoff/done — user can POST here to signal completion
	mux.HandleFunc("/handoff/done", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "use POST or GET", http.StatusMethodNotAllowed)
			return
		}
		h.SignalDone()
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "OK — browser control returned to agent")
	})

	// WebSocket proxy — forward DevTools connections to the real CDP endpoint
	mux.HandleFunc("/devtools/", h.makeWSProxy(opts.CDPURL))

	h.http = &http.Server{Handler: mux}

	srv := h.http // capture for goroutine (avoid race with Stop setting h.http = nil)
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			h.logger.Printf("[handoff] HTTP server error: %v", serveErr)
		}
	}()

	h.logger.Printf("[handoff] CDP proxy listening on %s (reason: %s)", listener.Addr().String(), opts.Reason)

	actualAddr := listener.Addr().String()

	return &HandoffInfo{
		InspectURL:    fmt.Sprintf("chrome://inspect → Configure → %s", actualAddr),
		ListenAddress: actualAddr,
	}, nil
}

// makeWSProxy returns an HTTP handler that proxies WebSocket connections
// to the target CDP endpoint.
func (h *HandoffServer) makeWSProxy(targetURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade the incoming connection
		clientConn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			h.logger.Printf("[handoff] Failed to upgrade client WebSocket: %v", err)
			return
		}
		defer clientConn.Close()

		// Connect to the real CDP endpoint
		targetConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
		if err != nil {
			h.logger.Printf("[handoff] Failed to connect to CDP endpoint: %v", err)
			return
		}
		defer targetConn.Close()

		h.logger.Printf("[handoff] User connected via DevTools")

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

		h.logger.Printf("[handoff] User disconnected from DevTools")

		// Grace period: wait a bit before signaling done, in case the user
		// reconnects (e.g. switching DevTools tabs).
		go func() {
			time.Sleep(10 * time.Second)
			h.SignalDone()
		}()
	}
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
