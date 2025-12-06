package launcher

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/api"
)

// RunStudio starts the Studio web server
func RunStudio(port int) error {
	router := mux.NewRouter()

	// Register API routes
	api.RegisterRoutes(router)

	// Serve web assets from file system
	webDir := findWebDir()
	if webDir != "" {
		log.Printf("Serving web assets from: %s", webDir)
		spaHandler := spaFileServer(http.Dir(webDir))
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
	
	// Print startup message
	fmt.Printf("\n")
	fmt.Printf("  🚀 Astonish Studio is running!\n")
	fmt.Printf("\n")
	fmt.Printf("  ➜  Local:   http://localhost:%d\n", port)
	fmt.Printf("\n")
	fmt.Printf("  Press Ctrl+C to stop\n")
	fmt.Printf("\n")

	return http.ListenAndServe(addr, router)
}

// findWebDir looks for the web/dist directory
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

