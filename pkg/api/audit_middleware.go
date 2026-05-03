package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/store"
)

// AuditMiddleware logs API requests to the audit store.
//
// Only active in platform mode — personal mode uses the noop audit store
// which silently discards entries. Audit entries are written asynchronously
// so they don't add latency to request handling.
//
// The middleware captures:
//   - UserID from the authenticated platform user (or "anonymous")
//   - TeamID from the tenant context (if available)
//   - Action: HTTP method (e.g. "GET", "POST", "DELETE")
//   - Resource: matched route pattern + actual path (e.g. "GET /api/sessions")
//   - IP address from X-Forwarded-For or RemoteAddr
//   - Status code from the response
func AuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc := store.FromRequest(r)
		if svc == nil || svc.Audit == nil || svc.Mode != store.ModePlatform {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap the response writer to capture status code.
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(sw, r)

		// Build audit entry asynchronously to avoid blocking the response.
		audit := svc.Audit
		userID := "anonymous"
		teamID := ""
		if pu := GetPlatformUser(r); pu != nil {
			userID = pu.ID
			teamID = pu.TeamSlug
		}

		// Use the matched route template for a clean resource description.
		resource := r.Method + " " + r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if tpl, err := route.GetPathTemplate(); err == nil {
				resource = r.Method + " " + tpl
			}
		}

		entry := &store.AuditEntry{
			Timestamp: time.Now(),
			UserID:    userID,
			TeamID:    teamID,
			Action:    r.Method,
			Resource:  resource,
			Detail: map[string]any{
				"path":   r.URL.Path,
				"status": sw.statusCode,
			},
			IPAddress: clientIP(r),
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := audit.Log(ctx, entry); err != nil {
				slog.Warn("failed to write audit log", "error", err, "user", userID, "resource", resource)
			}
		}()
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.statusCode = code
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.wroteHeader = true
	}
	return sw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher so that SSE streaming works through the
// audit middleware wrapper. Without this, w.(http.Flusher) type assertions
// fail and SSE endpoints return "Streaming unsupported".
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// clientIP is defined in auth_platform.go — reused here for audit logging.
