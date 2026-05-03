package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// RegisterTeamRoutes registers team management endpoints.
// These require platform mode and an authenticated user.
func RegisterTeamRoutes(router *mux.Router, pa *PlatformAuth) {
	// Team CRUD
	router.HandleFunc("/api/teams", pa.handleListTeams).Methods("GET")
	router.HandleFunc("/api/teams", pa.handleCreateTeam).Methods("POST")
	router.HandleFunc("/api/teams/{slug}", pa.handleGetTeam).Methods("GET")
	router.HandleFunc("/api/teams/{slug}", pa.handleDeleteTeam).Methods("DELETE")

	// Team membership
	router.HandleFunc("/api/teams/{slug}/members", pa.handleListTeamMembers).Methods("GET")
	router.HandleFunc("/api/teams/{slug}/members", pa.handleAddTeamMember).Methods("POST")
	router.HandleFunc("/api/teams/{slug}/members/{userID}", pa.handleRemoveTeamMember).Methods("DELETE")
	router.HandleFunc("/api/teams/{slug}/members/{userID}/role", pa.handleSetTeamRole).Methods("PUT")

	// Org info
	router.HandleFunc("/api/org", pa.handleGetOrg).Methods("GET")
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

	teams, err := orgDataStore.Teams().ListTeams(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list teams")
		return
	}

	respondJSON(w, http.StatusOK, teams)
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
	if user.Role != "owner" && user.Role != "admin" {
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
		SchemaName: pgstore.TeamSchemaName(req.Slug),
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
	if user.Role != "owner" && user.Role != "admin" {
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

	respondJSON(w, http.StatusOK, members)
}

// --- Handler: POST /api/teams/{slug}/members ---

type addMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func (pa *PlatformAuth) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if user.Role != "owner" && user.Role != "admin" {
		respondError(w, http.StatusForbidden, "only org admins can add team members")
		return
	}

	slug := mux.Vars(r)["slug"]
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.UserID == "" {
		respondError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
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

	if err := orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   req.UserID,
		TeamID:   team.ID,
		Role:     req.Role,
		JoinedAt: time.Now(),
	}); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add member")
		return
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
	if user.Role != "owner" && user.Role != "admin" {
		respondError(w, http.StatusForbidden, "only org admins can remove team members")
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
	if user.Role != "owner" && user.Role != "admin" {
		respondError(w, http.StatusForbidden, "only org admins can change roles")
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
