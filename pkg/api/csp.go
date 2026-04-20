package api

import (
	"net/http"
	"strings"
)

// cspPolicy defines the Content-Security-Policy header for the Studio UI.
// The policy restricts resource loading to the same origin with targeted
// exceptions for Tailwind CSS inline styles and WebSocket connections.
//
// frame-src 'self' allows the Studio UI to embed same-origin iframes (e.g.
// the KasmVNC browser view during human-in-the-loop handoff sessions).
//
// The sha256 hash in script-src allows the inline <script> in the device
// authorization page (auth.go authPageHTML). If that script changes, the
// hash must be recomputed:
//
//	printf '%s' '<script content>' | openssl dgst -sha256 -binary | base64
const cspPolicy = "default-src 'self'; " +
	"script-src 'self' 'sha256-QPIelXUbpkDESZsTgggSaMGNOA/Le9qMm+4Wa+lXIvs='; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self' ws: wss:; " +
	"frame-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// CSPMiddleware adds Content-Security-Policy and other security headers
// to all responses. It should be applied inside auth but outside the
// subdomain proxy (proxied container apps have their own CSP).
//
// The browser VNC proxy path (/api/browser/vnc/) is exempted because
// KasmVNC's web client needs to load its own scripts and styles which
// don't comply with the Studio CSP. The VNC proxy is same-origin with
// Studio so iframe embedding is safe.
func CSPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSP for VNC proxy — KasmVNC serves its own web client with
		// inline scripts/styles that conflict with the Studio CSP.
		// Also skip X-Content-Type-Options because KasmVNC serves JS files
		// as text/plain. With "nosniff" browsers refuse to execute scripts
		// that don't have a JavaScript MIME type.
		// Skip CSP for app-preview runtime scripts — they're static JS
		// files fetched by the parent page, and strict CSP isn't needed.
		if !strings.HasPrefix(r.URL.Path, "/api/browser/vnc/") &&
			!strings.HasPrefix(r.URL.Path, "/api/app-preview/") {
			w.Header().Set("Content-Security-Policy", cspPolicy)
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}
		next.ServeHTTP(w, r)
	})
}
