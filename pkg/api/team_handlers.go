package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/schardosin/astonish/pkg/mailer"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/entstore"
)

// RegisterTeamRoutes registers team management endpoints.
// These require platform mode and an authenticated user.
func RegisterTeamRoutes(router *mux.Router, pa *PlatformAuth) {
	// Team CRUD
	router.HandleFunc("/api/teams", pa.handleListTeams).Methods("GET")
	router.HandleFunc("/api/teams", pa.handleCreateTeam).Methods("POST")
	router.HandleFunc("/api/teams/{slug}", pa.handleGetTeam).Methods("GET")
	router.HandleFunc("/api/teams/{slug}", pa.handleDeleteTeam).Methods("DELETE")

	// Organization switching (authenticated, multi-org support)
	router.HandleFunc("/api/orgs", pa.handleListUserOrgs).Methods("GET")
	router.HandleFunc("/api/orgs/switch", pa.handleSwitchOrg).Methods("POST")

	// Team membership
	router.HandleFunc("/api/teams/{slug}/members", pa.handleListTeamMembers).Methods("GET")
	router.HandleFunc("/api/teams/{slug}/members", pa.handleAddTeamMember).Methods("POST")
	router.HandleFunc("/api/teams/{slug}/members/{userID}", pa.handleRemoveTeamMember).Methods("DELETE")
	router.HandleFunc("/api/teams/{slug}/members/{userID}/role", pa.handleSetTeamRole).Methods("PUT")

	// Org info
	router.HandleFunc("/api/org", pa.handleGetOrg).Methods("GET")

	// Team members with delivery channels (for scheduler UI)
	router.HandleFunc("/api/team/members/channels", TeamMemberChannelsHandler(pa)).Methods("GET")
}

// --- Handler: GET /api/teams ---

func (pa *PlatformAuth) handleListTeams(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	var teams []*store.Team
	if CanManageOrg(user) {
		// Admins/owners see all teams for management purposes.
		teams, err = orgDataStore.Teams().ListTeams(r.Context())
	} else {
		// Regular members only see teams they belong to.
		teams, err = orgDataStore.Teams().ListTeamsForUser(r.Context(), user.ID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list teams")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

// --- Handler: POST /api/teams ---

type createTeamRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*[a-z0-9]$`)

func (pa *PlatformAuth) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !CanManageOrg(user) {
		respondError(w, http.StatusForbidden, "only org admins can create teams")
		return
	}

	var req createTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "team name is required")
		return
	}
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}
	if len(req.Slug) < 2 || len(req.Slug) > 50 || !slugRegex.MatchString(req.Slug) {
		respondError(w, http.StatusBadRequest, "invalid slug: must be 2-50 lowercase alphanumeric characters with hyphens")
		return
	}

	ctx := r.Context()
	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	// Check for duplicate slug
	existing, _ := orgDataStore.Teams().GetTeamBySlug(ctx, req.Slug)
	if existing != nil {
		respondError(w, http.StatusConflict, "a team with this slug already exists")
		return
	}

	team := &store.Team{
		ID:         uuid.New().String(),
		Name:       req.Name,
		Slug:       req.Slug,
		SchemaName: entstore.TeamSchemaName(req.Slug),
		CreatedAt:  time.Now(),
	}

	if err := orgDataStore.Teams().CreateTeam(ctx, team); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create team")
		return
	}

	// Provision the team schema
	if err := orgDataStore.ProvisionTeam(ctx, req.Slug); err != nil {
		respondError(w, http.StatusInternalServerError, "team created but schema provisioning failed")
		return
	}

	// Add creator as team admin
	_ = orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   user.ID,
		TeamID:   team.ID,
		Role:     "admin",
		JoinedAt: time.Now(),
	})

	respondJSON(w, http.StatusCreated, team)
}

// --- Handler: GET /api/teams/{slug} ---

func (pa *PlatformAuth) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	slug := mux.Vars(r)["slug"]

	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	team, err := orgDataStore.Teams().GetTeamBySlug(r.Context(), slug)
	if err != nil || team == nil {
		respondError(w, http.StatusNotFound, "team not found")
		return
	}

	respondJSON(w, http.StatusOK, team)
}

// --- Handler: DELETE /api/teams/{slug} ---

func (pa *PlatformAuth) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !CanManageOrg(user) {
		respondError(w, http.StatusForbidden, "only org admins can delete teams")
		return
	}

	slug := mux.Vars(r)["slug"]
	if slug == "general" {
		respondError(w, http.StatusForbidden, "cannot delete the default team")
		return
	}

	ctx := r.Context()
	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	team, err := orgDataStore.Teams().GetTeamBySlug(ctx, slug)
	if err != nil || team == nil {
		respondError(w, http.StatusNotFound, "team not found")
		return
	}

	if err := orgDataStore.Teams().DeleteTeam(ctx, team.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete team")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Handler: GET /api/teams/{slug}/members ---

func (pa *PlatformAuth) handleListTeamMembers(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	slug := mux.Vars(r)["slug"]
	ctx := r.Context()

	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	team, err := orgDataStore.Teams().GetTeamBySlug(ctx, slug)
	if err != nil || team == nil {
		respondError(w, http.StatusNotFound, "team not found")
		return
	}

	members, err := orgDataStore.Teams().ListMembers(ctx, team.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	// Enrich members with user details (email, display_name) from the platform users store.
	// The users table lives in the platform DB while team_memberships is in the org DB,
	// so we look up each user individually.
	userStore := pa.pgStore.Users()
	for _, m := range members {
		u, err := userStore.GetByID(ctx, m.UserID)
		if err == nil && u != nil {
			m.Email = u.Email
			m.DisplayName = u.DisplayName
		}
	}

	// Include the caller's role in this team so the frontend can enable
	// team-admin management actions without an extra API call.
	callerTeamRole := ""
	if CanManageOrg(user) {
		callerTeamRole = "org_admin"
	} else {
		callerTeamRole, _ = orgDataStore.Teams().GetMemberRole(ctx, user.ID, team.ID)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"members":    members,
		"callerRole": callerTeamRole,
	})
}

// --- Handler: POST /api/teams/{slug}/members ---

type addMemberRequest struct {
	UserID     string `json:"user_id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	SendNotify *bool  `json:"send_notify"` // pointer so we detect absence (default true)
}

func (pa *PlatformAuth) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	slug := mux.Vars(r)["slug"]
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	team, err := orgDataStore.Teams().GetTeamBySlug(ctx, slug)
	if err != nil || team == nil {
		respondError(w, http.StatusNotFound, "team not found")
		return
	}

	// Org admins or team admins can add members.
	if !CanManageTeamByID(r, user, orgDataStore, team.ID) {
		respondError(w, http.StatusForbidden, "only org admins or team admins can add team members")
		return
	}

	// Resolve user: accept either user_id or email.
	resolvedID, err := pa.resolveUserByEmailOrID(r, req.Email, req.UserID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		respondError(w, http.StatusBadRequest, "role must be admin, member, or viewer")
		return
	}

	// Ensure the user is a member of this org. Auto-add as "member" if not.
	org, err := pa.pgStore.Organizations().GetBySlug(ctx, user.OrgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve organization")
		return
	}
	existingOrgRole, _ := pa.pgStore.Organizations().GetMemberRole(ctx, resolvedID, org.ID)
	if existingOrgRole == "" {
		if err := pa.pgStore.Organizations().AddMember(ctx, resolvedID, org.ID, "member"); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to add user to organization")
			return
		}
		// Provision personal schema in the org DB.
		_ = orgDataStore.ProvisionPersonalSchema(ctx, resolvedID)
	}

	// Add to team.
	if err := orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   resolvedID,
		TeamID:   team.ID,
		Role:     req.Role,
		JoinedAt: time.Now(),
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	// Send notification email (default: true).
	sendNotify := req.SendNotify == nil || *req.SendNotify
	if sendNotify {
		// Look up the target user's details for the email.
		targetUser, _ := pa.pgStore.Users().GetByID(ctx, resolvedID)
		if targetUser != nil && targetUser.Email != "" {
			scheme := "https"
			if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1")) {
				scheme = "http"
			}
			if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
				scheme = "https"
			}
			appURL := scheme + "://" + r.Host
			mailer.SendAsync(ctx, mailer.TeamAdded{
				Recipient:   targetUser.Email,
				DisplayName: targetUser.DisplayName,
				TeamName:    team.Name,
				OrgName:     org.Name,
				AppURL:      appURL,
			})
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// --- Handler: DELETE /api/teams/{slug}/members/{userID} ---

func (pa *PlatformAuth) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	vars := mux.Vars(r)
	slug := vars["slug"]
	targetUserID := vars["userID"]

	ctx := r.Context()
	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	team, err := orgDataStore.Teams().GetTeamBySlug(ctx, slug)
	if err != nil || team == nil {
		respondError(w, http.StatusNotFound, "team not found")
		return
	}

	// Org admins or team admins can remove members.
	if !CanManageTeamByID(r, user, orgDataStore, team.ID) {
		respondError(w, http.StatusForbidden, "only org admins or team admins can remove team members")
		return
	}

	if err := orgDataStore.Teams().RemoveMember(ctx, targetUserID, team.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Handler: PUT /api/teams/{slug}/members/{userID}/role ---

type setRoleRequest struct {
	Role string `json:"role"`
}

func (pa *PlatformAuth) handleSetTeamRole(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	vars := mux.Vars(r)
	slug := vars["slug"]
	targetUserID := vars["userID"]

	var req setRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role != "admin" && req.Role != "member" && req.Role != "viewer" {
		respondError(w, http.StatusBadRequest, "role must be admin, member, or viewer")
		return
	}

	ctx := r.Context()
	orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to access organization")
		return
	}

	team, err := orgDataStore.Teams().GetTeamBySlug(ctx, slug)
	if err != nil || team == nil {
		respondError(w, http.StatusNotFound, "team not found")
		return
	}

	// Org admins or team admins can change roles.
	if !CanManageTeamByID(r, user, orgDataStore, team.ID) {
		respondError(w, http.StatusForbidden, "only org admins or team admins can change roles")
		return
	}

	if err := orgDataStore.Teams().SetRole(ctx, targetUserID, team.ID, req.Role); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update role")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- Handler: GET /api/org ---

func (pa *PlatformAuth) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	org, err := pa.pgStore.Organizations().GetBySlug(r.Context(), user.OrgSlug)
	if err != nil || org == nil {
		respondError(w, http.StatusNotFound, "organization not found")
		return
	}

	respondJSON(w, http.StatusOK, org)
}

// --- Helpers ---

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) < 2 {
		s = s + "-team"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// --- Handler: GET /api/orgs ---
// Returns all organizations the authenticated user belongs to.

func (pa *PlatformAuth) handleListUserOrgs(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	orgs, err := pa.pgStore.Organizations().GetUserOrgs(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}

	type orgEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	}

	result := make([]orgEntry, 0, len(orgs))
	for _, m := range orgs {
		result = append(result, orgEntry{
			ID:   m.OrgID,
			Name: m.OrgName,
			Slug: m.OrgSlug,
			Role: m.Role,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"orgs":       result,
		"active_org": user.OrgSlug,
	})
}

// --- Handler: POST /api/orgs/switch ---
// Switches the authenticated user to a different organization.
// Re-issues JWT tokens scoped to the target org.

func (pa *PlatformAuth) handleSwitchOrg(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		OrgSlug string `json:"org_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrgSlug == "" {
		respondError(w, http.StatusBadRequest, "org_slug is required")
		return
	}

	ctx := r.Context()

	// Verify the user is a member of the target org
	orgs, err := pa.pgStore.Organizations().GetUserOrgs(ctx, user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to verify membership")
		return
	}

	var membership *store.OrgMembership
	for _, m := range orgs {
		if m.OrgSlug == req.OrgSlug {
			membership = m
			break
		}
	}
	if membership == nil {
		respondError(w, http.StatusForbidden, "you are not a member of this organization")
		return
	}

	// Load the target org
	org, err := pa.pgStore.Organizations().GetByID(ctx, membership.OrgID)
	if err != nil || org == nil {
		respondError(w, http.StatusInternalServerError, "failed to load organization")
		return
	}

	if org.Status != "active" {
		respondError(w, http.StatusForbidden, "organization is suspended")
		return
	}

	// Load full user for token issuance (we need PlatformRole etc.)
	fullUser, err := pa.pgStore.Users().GetByID(ctx, user.ID)
	if err != nil || fullUser == nil {
		respondError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	// Issue new tokens scoped to the target org
	pa.issueTokensAndRespond(w, r, fullUser, org, membership.Role)
}
