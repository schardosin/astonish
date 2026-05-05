package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// PlatformAuthMiddleware returns an HTTP middleware that validates JWT tokens
// for platform mode. It replaces the device-auth middleware when the storage
// backend is "postgres".
//
// The middleware:
// 1. Allows SPA static assets to pass (the React app serves its own login page).
// 2. Allows loopback addresses to pass (CLI/local tools).
// 3. Allows auth endpoints to pass (/api/auth/*).
// 4. Allows platform setup endpoints to pass (/api/platform/mode, /api/platform/init/*).
// 5. Allows migration endpoints to pass (/api/migration/*) — needed before first user exists.
// 6. Validates the access token cookie.
// 7. On valid token: populates TenantContext for downstream TenantMiddleware.
// 8. On expired token: returns 401 so frontend can attempt refresh.
// 9. On missing/invalid token: returns 401 for API requests.
func PlatformAuthMiddleware(pa *PlatformAuth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Allow SPA static assets without authentication.
		// The React app contains the login/register page — it needs to load
		// before the user can authenticate. Only /api/* paths require auth.
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Always allow auth endpoints (register, login, refresh, setup-status, etc.)
		if strings.HasPrefix(path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow platform setup endpoints — the frontend needs to check deployment
		// mode and init status before any user has registered.
		if strings.HasPrefix(path, "/api/platform/") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow migration endpoints — migration runs before first user registration
		// when transitioning from file-based to platform mode.
		if strings.HasPrefix(path, "/api/migration/") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow loopback addresses (CLI, local tools)
		if isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Check for access token in cookie
		cookie, err := r.Cookie(accessCookieName)
		if err != nil || cookie.Value == "" {
			handleUnauthenticated(w, r)
			return
		}

		// Validate the JWT
		claims, err := pa.jwt.ValidateAccessToken(cookie.Value)
		if err != nil {
			handleUnauthenticated(w, r)
			return
		}

		// Populate TenantContext for downstream middleware
		teamSlug := claims.DefaultTeamSlug
		// Allow per-request team override via header
		if headerTeam := r.Header.Get("X-Astonish-Team"); headerTeam != "" {
			teamSlug = headerTeam
		}
		// Allow team override via query parameter (for WebSocket connections
		// which cannot set custom headers from the browser).
		if qTeam := r.URL.Query().Get("team"); qTeam != "" && teamSlug == claims.DefaultTeamSlug {
			teamSlug = qTeam
		}

		// Validate that non-admin users are members of the requested team.
		// Admins/owners can access any team in the org for management.
		if teamSlug != "" && claims.Role != "owner" && claims.Role != "admin" {
			orgDS, dsErr := pa.pgStore.ForOrg(claims.OrgSlug)
			if dsErr != nil {
				slog.Warn("team membership check: failed to access org", "org", claims.OrgSlug, "err", dsErr)
				respondError(w, http.StatusInternalServerError, "failed to verify team membership")
				return
			}
			isMember, memberErr := orgDS.Teams().IsTeamMember(r.Context(), claims.UserID, teamSlug)
			if memberErr != nil {
				slog.Warn("team membership check failed", "user", claims.UserID, "team", teamSlug, "err", memberErr)
				respondError(w, http.StatusInternalServerError, "failed to verify team membership")
				return
			}
			if !isMember {
				respondError(w, http.StatusForbidden, "you are not a member of this team")
				return
			}
		}

		tc := &pgstore.TenantContext{
			OrgSlug:  claims.OrgSlug,
			TeamSlug: teamSlug,
			UserID:   claims.UserID,
		}

		// Store tenant context and user info in request context
		ctx := pgstore.WithTenantContext(r.Context(), tc)
		ctx = WithPlatformUser(ctx, &PlatformUser{
			ID:          claims.UserID,
			Email:       claims.Email,
			DisplayName: claims.DisplayName,
			OrgSlug:     claims.OrgSlug,
			TeamSlug:    teamSlug,
			Role:        claims.Role,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleUnauthenticated returns 401 for API requests.
// The frontend SPA checks for 401 and shows the login form.
func handleUnauthenticated(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","message":"authentication required"}`))
}

// isLoopbackRequest checks if the request originates from localhost.
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// --- PlatformUser context ---

// PlatformUser represents the authenticated user for the current request.
type PlatformUser struct {
	ID          string
	Email       string
	DisplayName string
	OrgSlug     string
	TeamSlug    string
	Role        string
}

type platformUserKey struct{}

// WithPlatformUser stores the authenticated platform user in the context.
func WithPlatformUser(ctx context.Context, user *PlatformUser) context.Context {
	return context.WithValue(ctx, platformUserKey{}, user)
}

// GetPlatformUser retrieves the authenticated platform user from the request context.
// Returns nil if not in platform mode or user is not authenticated.
func GetPlatformUser(r *http.Request) *PlatformUser {
	u, _ := r.Context().Value(platformUserKey{}).(*PlatformUser)
	return u
}

// PlatformUserFromContext retrieves the platform user from a context directly.
func PlatformUserFromContext(ctx context.Context) *PlatformUser {
	u, _ := ctx.Value(platformUserKey{}).(*PlatformUser)
	return u
}

// GetPlatformUserFromServices is a helper that extracts user info from Services context.
// Falls back to checking for platform user in context.
func GetPlatformUserFromServices(svc *store.Services, r *http.Request) *PlatformUser {
	if svc == nil || svc.Mode != store.ModePlatform {
		return nil
	}
	return GetPlatformUser(r)
}
