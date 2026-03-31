package launcher

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/sandbox"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	"github.com/schardosin/astonish/web"
)

// StudioServer wraps the HTTP server with lifecycle management.
type StudioServer struct {
	server       *http.Server
	listener     net.Listener
	port         int
	Auth         *api.AuthManager // nil means no auth (direct CLI mode)
	sessionStore *persistentsession.FileStore
}

// StudioOption configures optional StudioServer behavior.
type StudioOption func(*StudioServer)

// WithAuth enables device authorization on the Studio server.
func WithAuth(am *api.AuthManager) StudioOption {
	return func(s *StudioServer) { s.Auth = am }
}

// WithSessionStore injects a shared FileStore for session persistence.
// When set, the Studio chat agent reuses this store instead of creating its own,
// ensuring a single FileStore instance across the daemon process.
func WithSessionStore(store *persistentsession.FileStore) StudioOption {
	return func(s *StudioServer) { s.sessionStore = store }
}

// NewStudioServer creates a configured Studio server without starting it.
func NewStudioServer(port int, opts ...StudioOption) (*StudioServer, error) {
	s := &StudioServer{port: port}
	for _, opt := range opts {
		opt(s)
	}

	// Wire Studio Chat initialization (lazy, runs on first chat request)
	sharedStore := s.sessionStore // capture for closure
	api.SetStudioChatInitFunc(func(ctx context.Context) (*api.StudioChatComponents, error) {
		appCfg, err := config.LoadAppConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}

		result, err := NewWiredChatAgent(ctx, &ChatFactoryConfig{
			AppConfig:    appCfg,
			ProviderName: appCfg.General.DefaultProvider,
			ModelName:    appCfg.General.DefaultModel,
			DebugMode:    false,
			AutoApprove:  false,
			WorkspaceDir: "",
			IsDaemon:     false,
			SessionStore: sharedStore,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize chat agent: %w", err)
		}

		// Wire scheduler access via daemon HTTP API
		daemonPort := appCfg.Daemon.GetPort()
		tools.SetSchedulerAccess(&tools.SchedulerHTTPAccess{
			BaseURL: fmt.Sprintf("http://localhost:%d", daemonPort),
		})

		return &api.StudioChatComponents{
			ChatAgent:         result.ChatAgent,
			LLM:               result.LLM,
			SessionService:    result.SessionService,
			ProviderName:      result.ProviderName,
			ModelName:         result.ModelName,
			Compactor:         result.Compactor,
			InternalToolCount: len(result.InternalTools),
			MemoryActive:      result.MemoryManager != nil,
			SandboxEnabled:    sandbox.IsSandboxEnabled(&appCfg.Sandbox),
			StartupNotices:    result.StartupNotices,
			ShutdownSandbox:   result.ShutdownSandbox,
			Cleanup:           result.Cleanup,
		}, nil
	})

	router := mux.NewRouter()

	// Register auth endpoints first (they are always accessible)
	if s.Auth != nil {
		api.RegisterAuthRoutes(router, s.Auth)
	}

	// Register API routes
	api.RegisterRoutes(router)

	// Try to get web assets (embedded or filesystem)
	webFS := getWebAssets()

	var handler http.Handler

	if webFS != nil {
		// Wrap router + SPA into a single handler
		spaHandler := spaFileServer(http.FS(webFS))
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Let mux handle /api/* routes
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				router.ServeHTTP(w, r)
				return
			}
			// Everything else is SPA
			spaHandler.ServeHTTP(w, r)
		})
	} else {
		slog.Warn("no web assets found, run 'npm run build' in the web directory first")
		fallback := noAssetsHandler()
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				router.ServeHTTP(w, r)
				return
			}
			fallback.ServeHTTP(w, r)
		})
	}

	// Apply auth middleware if enabled (wraps both API and SPA)
	if s.Auth != nil {
		handler = api.AuthMiddleware(s.Auth, handler)
	}

	// Apply rate limiting for remote (non-loopback) requests.
	// Auth endpoints get stricter limits (brute-force protection);
	// general API endpoints get a generous budget.
	handler = api.RateLimitMiddleware(api.NewDefaultRateLimitConfig(), handler)

	// Apply security headers (CSP, X-Frame-Options, etc.) for Studio responses.
	// Placed inside auth/rate-limit but outside the subdomain proxy so that
	// proxied container apps are not affected.
	handler = api.CSPMiddleware(handler)

	// Wrap with subdomain proxy check OUTSIDE auth — subdomain-proxied
	// requests serve the container's app, not Studio, so they bypass
	// Studio authentication. The auth cookie is scoped to the Studio
	// domain and won't be sent on subdomain requests anyway.
	studioHandler := handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if containerName, port, ok := api.GetSubdomainRouter().Lookup(r.Host); ok {
			api.ServeSubdomainProxy(w, r, containerName, port)
			return
		}
		studioHandler.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	s.server = &http.Server{
		Handler:      handler,
		ReadTimeout:  0, // SSE streaming needs no read timeout
		WriteTimeout: 0, // SSE streaming needs no write timeout
		IdleTimeout:  120 * time.Second,
	}
	s.listener = listener

	return s, nil
}

// Port returns the port the server is listening on.
func (s *StudioServer) Port() int {
	return s.port
}

// Serve starts serving HTTP requests. Blocks until the server is shut down.
// Returns http.ErrServerClosed on graceful shutdown.
func (s *StudioServer) Serve() error {
	return s.server.Serve(s.listener)
}

// Shutdown gracefully shuts down the server with a timeout.
func (s *StudioServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// RunStudio starts the Studio web server (blocking, for CLI use).
func RunStudio(port int) error {
	studio, err := NewStudioServer(port)
	if err != nil {
		fmt.Printf("\n")
		fmt.Printf("  Failed to start Astonish Studio\n")
		fmt.Printf("\n")
		fmt.Printf("  Error: %v\n", err)
		fmt.Printf("\n")
		if isPortInUse(err) {
			fmt.Printf("  Port %d is already in use. Try one of these:\n", port)
			fmt.Printf("     - Stop the other process using port %d\n", port)
			fmt.Printf("     - Use a different port: astonish studio --port 9394\n")
		}
		fmt.Printf("\n")
		return err
	}

	fmt.Printf("\n")
	fmt.Printf("  Astonish Studio is running!\n")
	fmt.Printf("\n")
	fmt.Printf("  Local:   http://localhost:%d\n", port)
	fmt.Printf("\n")
	fmt.Printf("  Press Ctrl+C to stop\n")
	fmt.Printf("\n")

	return studio.Serve()
}

// isPortInUse checks if the error indicates the port is already in use
func isPortInUse(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "address already in use") ||
		contains(errStr, "bind: address already in use") ||
		contains(errStr, "Only one usage of each socket address")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getWebAssets returns the web assets filesystem
// Priority: 1. Filesystem (for dev), 2. Embedded (for production)
func getWebAssets() fs.FS {
	// First, try filesystem (for development)
	if dir := findWebDir(); dir != "" {
		slog.Info("serving web assets from filesystem", "dir", dir)
		return os.DirFS(dir)
	}

	// Fall back to embedded assets (for production binary)
	if embeddedFS := web.GetDistFS(); embeddedFS != nil {
		slog.Info("serving web assets from embedded binary")
		return embeddedFS
	}

	return nil
}

// findWebDir looks for the web/dist directory on filesystem
func findWebDir() string {
	// Check relative to current directory
	paths := []string{
		"web/dist",
		"../web/dist",
		"../../web/dist",
	}

	// Also check relative to executable
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(exeDir, "web/dist"),
			filepath.Join(exeDir, "../web/dist"),
		)
	}

	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			// Check if index.html exists
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				absPath, _ := filepath.Abs(path)
				return absPath
			}
		}
	}

	return ""
}

// noAssetsHandler returns a handler for when web assets are not found.
func noAssetsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Astonish Studio</title></head>
<body style="font-family: sans-serif; padding: 40px; background: #0d121f; color: white;">
<h1>Astonish Studio</h1>
<p>Web assets not found. Please build the frontend first:</p>
<pre style="background: #1a1a2e; padding: 20px; border-radius: 8px;">
cd web
npm install
npm run build
</pre>
<p>Then restart the studio server.</p>
</body>
</html>`)
			return
		}
		http.NotFound(w, r)
	})
}

// spaFileServer returns a handler that serves SPA files with fallback to index.html
func spaFileServer(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Skip API routes - let them 404 naturally so mux routes work
		if len(path) >= 4 && path[:4] == "/api" {
			http.NotFound(w, r)
			return
		}

		// Check if file exists
		f, err := fs.Open(path)
		if err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		} else {
			f.Close()
		}

		fileServer.ServeHTTP(w, r)
	})
}
