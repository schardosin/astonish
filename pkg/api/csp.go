package api

import "net/http"

// cspPolicy defines the Content-Security-Policy header for the Studio UI.
// The policy restricts resource loading to the same origin with targeted
// exceptions for Tailwind CSS inline styles and WebSocket connections.
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
	"frame-src 'none'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// CSPMiddleware adds Content-Security-Policy and other security headers
// to all responses. It should be applied inside auth but outside the
// subdomain proxy (proxied container apps have their own CSP).
func CSPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", cspPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
