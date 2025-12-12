package launcher

import (
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/web"
)

// RunStudio starts the Studio web server
func RunStudio(port int) error {
	router := mux.NewRouter()

	// Register API routes
	api.RegisterRoutes(router)

	// Try to get web assets (embedded or filesystem)
	webFS := getWebAssets()
	if webFS != nil {
		spaHandler := spaFileServer(http.FS(webFS))
		router.PathPrefix("/").Handler(spaHandler)
	} else {
		// No web assets found - print helpful message
		log.Printf("Warning: No web assets found. Run 'npm run build' in the web directory first.")
		router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Astonish Studio</title></head>
<body style="font-family: sans-serif; padding: 40px; background: #0d121f; color: white;">
<h1>🚀 Astonish Studio</h1>
<p>Web assets not found. Please build the frontend first:</p>
<pre style="background: #1a1a2e; padding: 20px; border-radius: 8px;">
cd web
npm install
npm run build
</pre>
<p>Then restart the studio server.</p>
<p>For development, you can run the Vite dev server instead:</p>
<pre style="background: #1a1a2e; padding: 20px; border-radius: 8px;">
cd web
npm run dev
</pre>
<p>And open <a href="http://localhost:5173" style="color: #9F7AEA;">http://localhost:5173</a></p>
</body>
</html>`)
				return
			}
			http.NotFound(w, r)
		})
	}

	addr := fmt.Sprintf(":%d", port)

	// Create listener first to check if port is available
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("\n")
		fmt.Printf("  ❌ Failed to start Astonish Studio\n")
		fmt.Printf("\n")
		fmt.Printf("  Error: %v\n", err)
		fmt.Printf("\n")
		if isPortInUse(err) {
			fmt.Printf("  💡 Port %d is already in use. Try one of these:\n", port)
			fmt.Printf("     • Stop the other process using port %d\n", port)
			fmt.Printf("     • Use a different port: astonish studio --port 9394\n")
		}
		fmt.Printf("\n")
		return err
	}

	// Print startup message only after we know the port is available
	fmt.Printf("\n")
	fmt.Printf("  🚀 Astonish Studio is running!\n")
	fmt.Printf("\n")
	fmt.Printf("  ➜  Local:   http://localhost:%d\n", port)
	fmt.Printf("\n")
	fmt.Printf("  Press Ctrl+C to stop\n")
	fmt.Printf("\n")

	// Serve using the listener we already created
	return http.Serve(listener, router)
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
		log.Printf("Serving web assets from filesystem: %s", dir)
		return os.DirFS(dir)
	}

	// Fall back to embedded assets (for production binary)
	if embeddedFS := web.GetDistFS(); embeddedFS != nil {
		log.Printf("Serving web assets from embedded binary")
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
