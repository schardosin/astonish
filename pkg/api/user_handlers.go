package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"

	"github.com/schardosin/astonish/pkg/mailer"
	"github.com/schardosin/astonish/pkg/store"
)

// RegisterUserRoutes registers admin user management endpoints.
// These require platform mode and an authenticated admin/owner.
func RegisterUserRoutes(router *mux.Router, pa *PlatformAuth) {
	router.HandleFunc("/api/admin/users", pa.handleListUsers).Methods("GET")
	router.HandleFunc("/api/admin/users/invite", pa.handleInviteUser).Methods("POST")
	router.HandleFunc("/api/admin/users/{id}", pa.handleGetUser).Methods("GET")
	router.HandleFunc("/api/admin/users/{id}", pa.handleRemoveUser).Methods("DELETE")
	router.HandleFunc("/api/admin/users/{id}/password", pa.handleSetUserPassword).Methods("PUT")
	router.HandleFunc("/api/admin/users/{id}/status", pa.handleSetUserStatus).Methods("PUT")
	router.HandleFunc("/api/admin/users/{id}/role", pa.handleSetUserOrgRole).Methods("PUT")
}

// requireAdmin extracts the platform user and returns nil (with an error response) if not admin/owner.
func requireAdmin(w http.ResponseWriter, r *http.Request) *PlatformUser {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return nil
	}
	if user.Role != "owner" && user.Role != "admin" {
		respondError(w, http.StatusForbidden, "admin or owner role required")
		return nil
	}
	return user
}

// --- Handler: POST /api/admin/users/invite ---
// Adds a user to the caller's organization. Creates the user on the platform if new.

type inviteUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	SendInvite  *bool  `json:"send_invite"` // pointer so we can detect absence (default true)
}

func (pa *PlatformAuth) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	caller := requireAdmin(w, r)
	if caller == nil {
		return
	}

	var req inviteUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	if req.Email == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.DisplayName == "" {
		respondError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	// Validate role.
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "member" && req.Role != "admin" && req.Role != "owner" {
		respondError(w, http.StatusBadRequest, "role must be member, admin, or owner")
		return
	}
	// Only owners can assign owner role.
	if req.Role == "owner" && caller.Role != "owner" {
		respondError(w, http.StatusForbidden, "only owners can assign the owner role")
		return
	}

	ctx := r.Context()

	// Look up the caller's org.
	org, err := pa.pgStore.Organizations().GetBySlug(ctx, caller.OrgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve organization")
		return
	}

	// Look up or create the user.
	var target *store.User
	created := false

	existing, _ := pa.pgStore.Users().GetByEmail(ctx, req.Email)
	if existing != nil {
		target = existing
	} else {
		// Create user on the platform (no password, no platform role).
		target = &store.User{
			ID:          uuid.New().String(),
			Email:       req.Email,
			DisplayName: req.DisplayName,
			Status:      "active",
			CreatedAt:   time.Now(),
		}
		if err := pa.pgStore.Users().Create(ctx, target); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
		created = true
	}

	// Check if already a member of this org.
	existingRole, _ := pa.pgStore.Organizations().GetMemberRole(ctx, target.ID, org.ID)
	if existingRole != "" {
		respondError(w, http.StatusConflict, "user is already a member of this organization")
		return
	}

	// Add to org.
	if err := pa.pgStore.Organizations().AddMember(ctx, target.ID, org.ID, req.Role); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add user to organization")
		return
	}

	// Provision personal schema in the org DB.
	if orgDS, err := pa.pgStore.ForOrg(caller.OrgSlug); err == nil {
		_ = orgDS.ProvisionPersonalSchema(ctx, target.ID)
	}

	// Send welcome email (default: true).
	sendInvite := req.SendInvite == nil || *req.SendInvite
	if sendInvite {
		scheme := "https"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		} else if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1")) {
			scheme = "http"
		}
		appURL := scheme + "://" + r.Host
		mailer.SendAsync(ctx, mailer.OrgInvite{
			Recipient:   req.Email,
			DisplayName: target.DisplayName,
			OrgName:     org.Name,
			AppURL:      appURL,
			IsNewUser:   created,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":           target.ID,
			"email":        target.Email,
			"display_name": target.DisplayName,
			"role":         req.Role,
			"status":       target.Status,
		},
		"created": created,
	})
}

// --- Handler: GET /api/admin/users ---
// Query params: ?org=<slug> to filter by org (defaults to caller's org).

func (pa *PlatformAuth) handleListUsers(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	ctx := r.Context()
	orgSlug := r.URL.Query().Get("org")
	if orgSlug == "" {
		orgSlug = user.OrgSlug
	}

	org, err := pa.pgStore.Organizations().GetBySlug(ctx, orgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	members, err := pa.pgStore.Organizations().ListMembers(ctx, org.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	// Strip password hashes before returning.
	type userResponse struct {
		ID          string    `json:"id"`
		Email       string    `json:"email"`
		DisplayName string    `json:"display_name"`
		Status      string    `json:"status"`
		Role        string    `json:"role"`
		JoinedAt    time.Time `json:"joined_at"`
		CreatedAt   time.Time `json:"created_at"`
		HasOIDC     bool      `json:"has_oidc"`
	}

	result := make([]userResponse, 0, len(members))
	for _, m := range members {
		result = append(result, userResponse{
			ID:          m.ID,
			Email:       m.Email,
			DisplayName: m.DisplayName,
			Status:      m.Status,
			Role:        m.Role,
			JoinedAt:    m.JoinedAt,
			CreatedAt:   m.CreatedAt,
			HasOIDC:     m.OIDCIssuer != "",
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"users": result})
}

// --- Handler: GET /api/admin/users/{id} ---

func (pa *PlatformAuth) handleGetUser(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	ctx := r.Context()
	targetID := mux.Vars(r)["id"]

	target, err := pa.pgStore.Users().GetByID(ctx, targetID)
	if err != nil || target == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	// Get the user's org memberships.
	orgs, err := pa.pgStore.Organizations().GetUserOrgs(ctx, target.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get user orgs")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":           target.ID,
			"email":        target.Email,
			"display_name": target.DisplayName,
			"status":       target.Status,
			"created_at":   target.CreatedAt,
			"has_oidc":     target.OIDCIssuer != "",
		},
		"orgs": orgs,
	})
}

// --- Handler: DELETE /api/admin/users/{id} ---
// Removes a user from the caller's organization (does NOT delete from platform).

func (pa *PlatformAuth) handleRemoveUser(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	ctx := r.Context()
	targetID := mux.Vars(r)["id"]

	// Prevent self-removal.
	if targetID == user.ID {
		respondError(w, http.StatusBadRequest, "cannot remove yourself from the organization")
		return
	}

	// Look up the caller's org.
	org, err := pa.pgStore.Organizations().GetBySlug(ctx, user.OrgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve organization")
		return
	}

	// Verify target user is actually in this org.
	role, err := pa.pgStore.Organizations().GetMemberRole(ctx, targetID, org.ID)
	if err != nil || role == "" {
		respondError(w, http.StatusNotFound, "user is not a member of this organization")
		return
	}

	// Only owners can remove other owners.
	if role == "owner" && user.Role != "owner" {
		respondError(w, http.StatusForbidden, "only owners can remove other owners")
		return
	}

	// Remove all team memberships in this org for the target user.
	if orgDS, err := pa.pgStore.ForOrg(user.OrgSlug); err == nil {
		teams, _ := orgDS.Teams().ListTeams(ctx)
		for _, t := range teams {
			_ = orgDS.Teams().RemoveMember(ctx, targetID, t.ID)
		}
	}

	// Remove org membership.
	if err := pa.pgStore.Organizations().RemoveMember(ctx, targetID, org.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove user from organization")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Handler: PUT /api/admin/users/{id}/password ---

type setPasswordRequest struct {
	Password string `json:"password"`
}

func (pa *PlatformAuth) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	ctx := r.Context()
	targetID := mux.Vars(r)["id"]

	var req setPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Password) < 8 {
		respondError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	target, err := pa.pgStore.Users().GetByID(ctx, targetID)
	if err != nil || target == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	// OIDC-only users can still get a local password set (enables dual auth).
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	target.PasswordHash = string(hash)
	if err := pa.pgStore.Users().Update(ctx, target); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update password")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "password_updated"})
}

// --- Handler: PUT /api/admin/users/{id}/status ---
// Body: { "status": "active" | "disabled" }

type setUserStatusRequest struct {
	Status string `json:"status"`
}

func (pa *PlatformAuth) handleSetUserStatus(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	ctx := r.Context()
	targetID := mux.Vars(r)["id"]

	// Prevent self-disable.
	if targetID == user.ID {
		respondError(w, http.StatusBadRequest, "cannot change your own status")
		return
	}

	var req setUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		respondError(w, http.StatusBadRequest, "status must be active or disabled")
		return
	}

	target, err := pa.pgStore.Users().GetByID(ctx, targetID)
	if err != nil || target == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	target.Status = req.Status
	if err := pa.pgStore.Users().Update(ctx, target); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update status")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// --- Handler: PUT /api/admin/users/{id}/role ---
// Body: { "role": "owner" | "admin" | "member" }
// Changes the user's org-level role.

type setUserOrgRoleRequest struct {
	Role string `json:"role"`
}

func (pa *PlatformAuth) handleSetUserOrgRole(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	ctx := r.Context()
	targetID := mux.Vars(r)["id"]

	var req setUserOrgRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role != "owner" && req.Role != "admin" && req.Role != "member" {
		respondError(w, http.StatusBadRequest, "role must be owner, admin, or member")
		return
	}

	// Only owners can promote to owner.
	if req.Role == "owner" && user.Role != "owner" {
		respondError(w, http.StatusForbidden, "only owners can promote to owner")
		return
	}

	// Prevent self-demotion for safety.
	if targetID == user.ID {
		respondError(w, http.StatusBadRequest, "cannot change your own role")
		return
	}

	// Resolve the caller's org.
	org, err := pa.pgStore.Organizations().GetBySlug(ctx, user.OrgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve organization")
		return
	}

	// Get the target's current role.
	currentRole, err := pa.pgStore.Organizations().GetMemberRole(ctx, targetID, org.ID)
	if err != nil {
		respondError(w, http.StatusNotFound, "user is not a member of this organization")
		return
	}

	// Only owners can change another owner's role.
	if currentRole == "owner" && user.Role != "owner" {
		respondError(w, http.StatusForbidden, "only owners can change an owner's role")
		return
	}

	// Prevent demoting the last owner — org must always have at least one.
	if currentRole == "owner" && req.Role != "owner" {
		members, err := pa.pgStore.Organizations().ListMembers(ctx, org.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to check owner count")
			return
		}
		ownerCount := 0
		for _, m := range members {
			if m.Role == "owner" {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			respondError(w, http.StatusBadRequest, "cannot demote the last owner; promote another user to owner first")
			return
		}
	}

	if err := pa.pgStore.Organizations().AddMember(ctx, targetID, org.ID, req.Role); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update role")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "role_updated", "role": req.Role})
}

// --- Helper: resolveUserByEmailOrID ---
// Resolves a user_id from either an email address or direct user_id.
// Returns the user ID or an error.

var errUserNotFound = errors.New("user not found; the user must be invited to the organization first")

func (pa *PlatformAuth) resolveUserByEmailOrID(r *http.Request, email, userID string) (string, error) {
	ctx := r.Context()
	if userID != "" {
		return userID, nil
	}
	if email != "" {
		user, err := pa.pgStore.Users().GetByEmail(ctx, email)
		if err != nil || user == nil {
			return "", errUserNotFound
		}
		return user.ID, nil
	}
	return "", errUserNotFound
}
