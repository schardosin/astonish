// Package api — authz.go provides unified authorization helpers for the Astonish platform.
//
// Three tiers of access control:
//
//	Platform level: superadmin (manages all orgs, users, OIDC providers)
//	Org level:      owner > admin > member
//	Team level:     admin > member (team admins can manage their team)
//
// Two styles of helpers:
//
//	Require* — gate a handler; write HTTP 401/403 and return nil if denied.
//	Can*     — pure boolean checks; no side effects.
package api

import (
	"net/http"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// ---------------------------------------------------------------------------
// Require* helpers — use these at the top of handlers to gate access.
// They extract the user from the request, check the permission, and if denied,
// write an appropriate HTTP error response and return nil.
// ---------------------------------------------------------------------------

// RequireAuth extracts the authenticated user from the request.
// Returns nil (and writes 401) if not authenticated.
func RequireAuth(w http.ResponseWriter, r *http.Request) *PlatformUser {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return nil
	}
	return user
}

// RequirePlatformAdmin requires the caller to be a platform superadmin.
func RequirePlatformAdmin(w http.ResponseWriter, r *http.Request) *PlatformUser {
	user := RequireAuth(w, r)
	if user == nil {
		return nil
	}
	if !IsPlatformAdmin(user) {
		respondError(w, http.StatusForbidden, "platform superadmin access required")
		return nil
	}
	return user
}

// RequireOrgAdmin requires the caller to be an org-level admin or owner.
func RequireOrgAdmin(w http.ResponseWriter, r *http.Request) *PlatformUser {
	user := RequireAuth(w, r)
	if user == nil {
		return nil
	}
	if !CanManageOrg(user) {
		respondError(w, http.StatusForbidden, "organization admin or owner role required")
		return nil
	}
	return user
}

// RequireOrgOwner requires the caller to be an org-level owner.
func RequireOrgOwner(w http.ResponseWriter, r *http.Request) *PlatformUser {
	user := RequireAuth(w, r)
	if user == nil {
		return nil
	}
	if !IsOrgOwner(user) {
		respondError(w, http.StatusForbidden, "organization owner role required")
		return nil
	}
	return user
}

// RequireTeamAdmin requires the caller to be a team-level admin, org admin, or org owner.
// In personal mode, always returns true (no multi-tenant restrictions).
// Writes an HTTP error and returns false if denied.
func RequireTeamAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !isPlatformMode(r) {
		return true
	}
	user := RequireAuth(w, r)
	if user == nil {
		return false
	}
	if CanManageOrg(user) {
		return true
	}
	// Check team-level admin role.
	if canManageCurrentTeam(r, user) {
		return true
	}
	respondError(w, http.StatusForbidden, "team admin access required")
	return false
}

// RequirePlatformServices extracts the store.Services from the request and verifies
// the system is running in platform mode. Returns nil (and writes 503) if not.
// Use this at the top of handlers that are exclusively platform-mode features.
func RequirePlatformServices(w http.ResponseWriter, r *http.Request) *store.Services {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		respondError(w, http.StatusServiceUnavailable, "this feature is only available in platform mode")
		return nil
	}
	return svc
}

// ---------------------------------------------------------------------------
// Can* helpers — pure boolean checks, no HTTP side effects.
// Use these for conditional logic (e.g., "show edit button in response?").
// ---------------------------------------------------------------------------

// IsPlatformAdmin returns true if the user is a platform superadmin.
func IsPlatformAdmin(user *PlatformUser) bool {
	return user != nil && user.PlatformRole == "superadmin"
}

// CanManageOrg returns true if the user can manage org-level resources (admin or owner).
func CanManageOrg(user *PlatformUser) bool {
	return user != nil && (user.Role == "owner" || user.Role == "admin")
}

// IsOrgOwner returns true if the user is the org owner.
func IsOrgOwner(user *PlatformUser) bool {
	return user != nil && user.Role == "owner"
}

// CanManageTeam returns true if the user can manage team-level resources.
// This includes org admins/owners and team-level admins.
// In personal mode, always returns true (single-user, no restrictions).
func CanManageTeam(r *http.Request, user *PlatformUser) bool {
	if !isPlatformMode(r) {
		return true
	}
	if user == nil {
		return false
	}
	if CanManageOrg(user) {
		return true
	}
	return canManageCurrentTeam(r, user)
}

// CanManageTeamByID checks whether the user can manage a specific team by ID.
// Use this when you have the orgDataStore and teamID already resolved.
func CanManageTeamByID(r *http.Request, user *PlatformUser, orgDS store.OrgDataStore, teamID string) bool {
	if user == nil {
		return false
	}
	if CanManageOrg(user) {
		return true
	}
	role, err := orgDS.Teams().GetMemberRole(r.Context(), user.ID, teamID)
	if err != nil {
		return false
	}
	return role == "admin"
}

// IsTeamAdmin is a convenience wrapper that checks whether the current request's
// user can manage team-level resources. Equivalent to CanManageTeam(r, GetPlatformUser(r)).
func IsTeamAdmin(r *http.Request) bool {
	return CanManageTeam(r, GetPlatformUser(r))
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// canManageCurrentTeam checks if the user is a team-level admin for the
// team indicated by the request's TenantContext.
func canManageCurrentTeam(r *http.Request, user *PlatformUser) bool {
	tc := pgstore.TenantContextFrom(r.Context())
	if tc == nil || tc.OrgSlug == "" || tc.TeamSlug == "" {
		return false
	}
	svc := store.FromRequest(r)
	if svc == nil || svc.TenantRouter == nil {
		return false
	}
	orgStore, err := svc.TenantRouter.ForOrg(tc.OrgSlug)
	if err != nil {
		return false
	}
	team, err := orgStore.Teams().GetTeamBySlug(r.Context(), tc.TeamSlug)
	if err != nil || team == nil {
		return false
	}
	role, err := orgStore.Teams().GetMemberRole(r.Context(), user.ID, team.ID)
	if err != nil {
		return false
	}
	return role == "admin"
}
