package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
	"golang.org/x/crypto/bcrypt"
)

// platformAdminGuard checks platform admin auth and resolves the PGStore.
// Returns (admin, pgStore) or writes an HTTP error and returns (nil, nil).
func platformAdminGuard(w http.ResponseWriter, r *http.Request) (*PlatformUser, *pgstore.PGStore) {
	admin := RequirePlatformAdmin(w, r)
	if admin == nil {
		return nil, nil
	}
	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return nil, nil
	}
	return admin, pgStore
}

// --- Organization Endpoints ---

// PlatformAdminListOrgsHandler handles GET /api/platform/admin/orgs
func PlatformAdminListOrgsHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	orgs, err := pgStore.Organizations().List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}

	// Enrich with member/team counts
	type orgEntry struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Slug        string    `json:"slug"`
		Status      string    `json:"status"`
		CreatedAt   time.Time `json:"created_at"`
		MemberCount int       `json:"member_count"`
		TeamCount   int       `json:"team_count"`
	}

	entries := make([]orgEntry, 0, len(orgs))
	for _, org := range orgs {
		entry := orgEntry{
			ID:        org.ID,
			Name:      org.Name,
			Slug:      org.Slug,
			Status:    org.Status,
			CreatedAt: org.CreatedAt,
		}
		// Get member count
		members, err := pgStore.Organizations().ListMembers(r.Context(), org.ID)
		if err == nil {
			entry.MemberCount = len(members)
		}
		// Get team count from org data store
		if orgDS, err := pgStore.ForOrg(org.Slug); err == nil {
			if teams, err := orgDS.Teams().ListTeams(r.Context()); err == nil {
				entry.TeamCount = len(teams)
			}
		}
		entries = append(entries, entry)
	}

	respondJSON(w, http.StatusOK, map[string]any{"organizations": entries})
}

// PlatformAdminCreateOrgHandler handles POST /api/platform/admin/orgs
func PlatformAdminCreateOrgHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	var req struct {
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		OwnerEmail string `json:"owner_email,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Slug == "" {
		// Auto-derive slug from name
		req.Slug = slugifyOrgName(req.Name)
	}
	if !isValidOrgSlug(req.Slug) {
		respondError(w, http.StatusBadRequest, "slug must contain only lowercase letters, numbers, hyphens, or underscores")
		return
	}

	ctx := r.Context()

	// Check slug uniqueness
	if existing, _ := pgStore.Organizations().GetBySlug(ctx, req.Slug); existing != nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("organization with slug %q already exists", req.Slug))
		return
	}

	// Create org record
	org := &store.Organization{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Slug:      req.Slug,
		DBName:    pgstore.OrgDBName(pgStore.InstanceSuffix(), req.Slug),
		Status:    "active",
		CreatedAt: time.Now(),
	}
	if err := pgStore.Organizations().Create(ctx, org); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create organization: %v", err))
		return
	}

	// Provision org database
	if err := pgStore.ProvisionOrg(ctx, org.ID, req.Slug); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to provision org database: %v", err))
		return
	}

	// Create default "General" team
	orgDS, err := pgStore.ForOrg(req.Slug)
	if err != nil {
		slog.Warn("failed to connect to new org DB for team creation", "error", err)
	} else {
		defaultTeam := &store.Team{
			ID:         uuid.New().String(),
			Name:       "General",
			Slug:       "general",
			SchemaName: pgstore.TeamSchemaName("general"),
			CreatedAt:  time.Now(),
		}
		if err := orgDS.Teams().CreateTeam(ctx, defaultTeam); err != nil {
			slog.Warn("failed to create default team in new org", "error", err)
		} else {
			if err := orgDS.ProvisionTeam(ctx, "general"); err != nil {
				slog.Warn("failed to provision default team schema", "error", err)
			}

			// If owner email is specified, add them to the org and team
			if req.OwnerEmail != "" {
				user, _ := pgStore.Users().GetByEmail(ctx, req.OwnerEmail)
				if user != nil {
					_ = pgStore.Organizations().AddMember(ctx, user.ID, org.ID, "owner")
					_ = orgDS.Teams().AddMember(ctx, &store.TeamMembership{
						UserID:   user.ID,
						TeamID:   defaultTeam.ID,
						Role:     "admin",
						JoinedAt: time.Now(),
					})
					_ = orgDS.ProvisionPersonalSchema(ctx, user.ID)
				}
			}
		}
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"organization": org,
		"message":      fmt.Sprintf("Organization %q created successfully", req.Name),
	})
}

// PlatformAdminGetOrgHandler handles GET /api/platform/admin/orgs/{slug}
func PlatformAdminGetOrgHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	slug := mux.Vars(r)["slug"]

	ctx := r.Context()
	org, err := pgStore.Organizations().GetBySlug(ctx, slug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	// Get members
	members, _ := pgStore.Organizations().ListMembers(ctx, org.ID)

	// Get teams
	var teams []*store.Team
	if orgDS, err := pgStore.ForOrg(slug); err == nil {
		teams, _ = orgDS.Teams().ListTeams(ctx)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"organization": org,
		"members":      members,
		"teams":        teams,
	})
}

// PlatformAdminUpdateOrgHandler handles PATCH /api/platform/admin/orgs/{slug}
func PlatformAdminUpdateOrgHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	slug := mux.Vars(r)["slug"]
	var req struct {
		Name   *string `json:"name,omitempty"`
		Status *string `json:"status,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	org, err := pgStore.Organizations().GetBySlug(ctx, slug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	if req.Name != nil {
		org.Name = *req.Name
	}
	if req.Status != nil {
		switch *req.Status {
		case "active", "suspended":
			org.Status = *req.Status
		default:
			respondError(w, http.StatusBadRequest, "status must be 'active' or 'suspended'")
			return
		}
	}

	if err := pgStore.Organizations().Update(ctx, org); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update organization")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"organization": org})
}

// PlatformAdminDeleteOrgHandler handles DELETE /api/platform/admin/orgs/{slug}
// Permanently deletes an org — only allowed if status is 'suspended'.
func PlatformAdminDeleteOrgHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	slug := mux.Vars(r)["slug"]

	ctx := r.Context()
	org, err := pgStore.Organizations().GetBySlug(ctx, slug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	if org.Status != "suspended" {
		respondError(w, http.StatusBadRequest, "organization must be suspended before permanent deletion")
		return
	}

	// Decommission (drops the org database)
	if err := pgStore.DecommissionOrg(ctx, slug); err != nil {
		slog.Warn("failed to decommission org database", "slug", slug, "error", err)
	}

	// Update status to decommissioned
	org.Status = "decommissioned"
	_ = pgStore.Organizations().Update(ctx, org)

	respondJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("Organization %q permanently deleted", slug),
		"deleted": true,
	})
}

// --- User Endpoints ---

// PlatformAdminListUsersHandler handles GET /api/platform/admin/users
func PlatformAdminListUsersHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	ctx := r.Context()
	users, err := pgStore.Users().List(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	// Enrich with org memberships
	type userEntry struct {
		ID           string               `json:"id"`
		Email        string               `json:"email"`
		DisplayName  string               `json:"display_name"`
		PlatformRole string               `json:"platform_role,omitempty"`
		Status       string               `json:"status"`
		CreatedAt    time.Time            `json:"created_at"`
		Orgs         []*store.OrgMembership `json:"orgs"`
	}

	entries := make([]userEntry, 0, len(users))
	for _, u := range users {
		entry := userEntry{
			ID:           u.ID,
			Email:        u.Email,
			DisplayName:  u.DisplayName,
			PlatformRole: u.PlatformRole,
			Status:       u.Status,
			CreatedAt:    u.CreatedAt,
		}
		orgs, _ := pgStore.Organizations().GetUserOrgs(ctx, u.ID)
		entry.Orgs = orgs
		entries = append(entries, entry)
	}

	respondJSON(w, http.StatusOK, map[string]any{"users": entries})
}

// PlatformAdminCreateUserHandler handles POST /api/platform/admin/users
func PlatformAdminCreateUserHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.DisplayName == "" {
		respondError(w, http.StatusBadRequest, "email and display_name are required")
		return
	}

	ctx := r.Context()

	// Check email uniqueness
	if existing, _ := pgStore.Users().GetByEmail(ctx, req.Email); existing != nil {
		respondError(w, http.StatusConflict, "a user with this email already exists")
		return
	}

	// Hash password (optional — empty means SSO-only user)
	var passwordHash string
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		passwordHash = string(hash)
	}

	user := &store.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		PasswordHash: passwordHash,
		Status:       "active",
		CreatedAt:    time.Now(),
	}

	if err := pgStore.Users().Create(ctx, user); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create user: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"user":    user,
		"message": fmt.Sprintf("User %q created successfully", req.Email),
	})
}

// PlatformAdminGetUserHandler handles GET /api/platform/admin/users/{id}
func PlatformAdminGetUserHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	userID := mux.Vars(r)["id"]

	ctx := r.Context()
	user, err := pgStore.Users().GetByID(ctx, userID)
	if err != nil || user == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	orgs, _ := pgStore.Organizations().GetUserOrgs(ctx, userID)

	respondJSON(w, http.StatusOK, map[string]any{
		"user": user,
		"orgs": orgs,
	})
}

// PlatformAdminUpdateUserHandler handles PATCH /api/platform/admin/users/{id}
func PlatformAdminUpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	userID := mux.Vars(r)["id"]
	var req struct {
		DisplayName  *string `json:"display_name,omitempty"`
		Status       *string `json:"status,omitempty"`
		PlatformRole *string `json:"platform_role,omitempty"`
		Password     *string `json:"password,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	user, err := pgStore.Users().GetByID(ctx, userID)
	if err != nil || user == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Status != nil {
		switch *req.Status {
		case "active", "suspended", "deactivated":
			user.Status = *req.Status
		default:
			respondError(w, http.StatusBadRequest, "status must be 'active', 'suspended', or 'deactivated'")
			return
		}
	}
	if req.Password != nil && *req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		user.PasswordHash = string(hash)
	}
	if req.PlatformRole != nil {
		newRole := *req.PlatformRole
		if newRole != "" && newRole != "superadmin" {
			respondError(w, http.StatusBadRequest, "platform_role must be 'superadmin' or empty string")
			return
		}
		// Prevent demoting the last superadmin
		if newRole == "" && user.PlatformRole == "superadmin" {
			count, _ := pgStore.Users().CountByPlatformRole(ctx, "superadmin")
			if count <= 1 {
				respondError(w, http.StatusBadRequest, "cannot demote the last platform superadmin")
				return
			}
		}
		user.PlatformRole = newRole
	}

	if err := pgStore.Users().Update(ctx, user); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"user": user})
}

// PlatformAdminDeleteUserHandler handles DELETE /api/platform/admin/users/{id}
func PlatformAdminDeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	admin, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	userID := mux.Vars(r)["id"]

	// Cannot delete yourself
	if userID == admin.ID {
		respondError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}

	ctx := r.Context()
	user, err := pgStore.Users().GetByID(ctx, userID)
	if err != nil || user == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	// Cannot delete the last superadmin
	if user.PlatformRole == "superadmin" {
		count, _ := pgStore.Users().CountByPlatformRole(ctx, "superadmin")
		if count <= 1 {
			respondError(w, http.StatusBadRequest, "cannot delete the last platform superadmin")
			return
		}
	}

	if err := pgStore.Users().Delete(ctx, userID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("User %q deleted", user.Email),
		"deleted": true,
	})
}

// PlatformAdminAddUserToOrgHandler handles POST /api/platform/admin/users/{id}/orgs
func PlatformAdminAddUserToOrgHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	userID := mux.Vars(r)["id"]
	var req struct {
		OrgSlug  string `json:"org_slug"`
		Role     string `json:"role"`
		TeamSlug string `json:"team_slug,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.OrgSlug == "" {
		respondError(w, http.StatusBadRequest, "org_slug is required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}

	ctx := r.Context()

	// Verify user exists
	user, err := pgStore.Users().GetByID(ctx, userID)
	if err != nil || user == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	// Verify org exists
	org, err := pgStore.Organizations().GetBySlug(ctx, req.OrgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	// Add membership
	if err := pgStore.Organizations().AddMember(ctx, userID, org.ID, req.Role); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to add user to org: %v", err))
		return
	}

	// Add to team if specified
	if req.TeamSlug != "" {
		if orgDS, err := pgStore.ForOrg(req.OrgSlug); err == nil {
			teams, _ := orgDS.Teams().ListTeams(ctx)
			for _, t := range teams {
				if t.Slug == req.TeamSlug {
					_ = orgDS.Teams().AddMember(ctx, &store.TeamMembership{
						UserID:   userID,
						TeamID:   t.ID,
						Role:     "member",
						JoinedAt: time.Now(),
					})
					break
				}
			}
			_ = orgDS.ProvisionPersonalSchema(ctx, userID)
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("User added to organization %q as %s", req.OrgSlug, req.Role),
	})
}

// PlatformAdminRemoveUserFromOrgHandler handles DELETE /api/platform/admin/users/{id}/orgs/{slug}
func PlatformAdminRemoveUserFromOrgHandler(w http.ResponseWriter, r *http.Request) {
	_, pgStore := platformAdminGuard(w, r)
	if pgStore == nil {
		return
	}

	vars := mux.Vars(r)
	userID := vars["id"]
	orgSlug := vars["slug"]

	ctx := r.Context()
	org, err := pgStore.Organizations().GetBySlug(ctx, orgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	if err := pgStore.Organizations().RemoveMember(ctx, userID, org.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove user from org")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("User removed from organization %q", orgSlug),
	})
}

// --- Helpers ---

func isValidOrgSlug(s string) bool {
	return slugRegex.MatchString(s)
}

func slugifyOrgName(name string) string {
	s := strings.ToLower(name)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "org"
	}
	return s
}

// getPlatformPGStore returns the platform PGStore singleton.
// It's set during daemon initialization.
var platformPGStoreInstance *pgstore.PGStore

func SetPlatformPGStore(pg *pgstore.PGStore) {
	platformPGStoreInstance = pg
}

func getPlatformPGStore() *pgstore.PGStore {
	return platformPGStoreInstance
}
