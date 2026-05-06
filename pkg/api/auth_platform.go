package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// PlatformAuth manages authentication for platform (multi-tenant) mode.
// It handles user registration, login, JWT token lifecycle, and automated
// org/team provisioning for the first user.
type PlatformAuth struct {
	jwt          *JWTIssuer
	authCfg      config.PlatformAuthConfig
	pgStore      *pgstore.PGStore
	storeCfg     config.StorageConfig
	migrationMgr *MigrationManager
}

// NewPlatformAuth creates a new platform auth manager.
func NewPlatformAuth(authCfg config.PlatformAuthConfig, pgStore *pgstore.PGStore, storeCfg config.StorageConfig) *PlatformAuth {
	jwtSecret := authCfg.GetJWTSecret()
	return &PlatformAuth{
		jwt:      NewJWTIssuer(jwtSecret, authCfg.GetAccessTokenTTL(), authCfg.GetRefreshTokenTTL()),
		authCfg:  authCfg,
		pgStore:  pgStore,
		storeCfg: storeCfg,
	}
}

// JWTIssuer returns the JWT issuer for use by middleware.
func (pa *PlatformAuth) JWTIssuer() *JWTIssuer {
	return pa.jwt
}

// SetMigrationManager sets the migration manager for setup-status checks.
func (pa *PlatformAuth) SetMigrationManager(mm *MigrationManager) {
	pa.migrationMgr = mm
}

// RegisterPlatformAuthRoutes registers platform auth endpoints.
// These routes are always accessible (not behind auth middleware).
func RegisterPlatformAuthRoutes(router *mux.Router, pa *PlatformAuth) {
	router.HandleFunc("/api/auth/register", pa.handleRegister).Methods("POST")
	router.HandleFunc("/api/auth/login", pa.handleLogin).Methods("POST")
	router.HandleFunc("/api/auth/refresh", pa.handleRefresh).Methods("POST")
	router.HandleFunc("/api/auth/logout", pa.handleLogout).Methods("POST")
	router.HandleFunc("/api/auth/me", pa.handleMe).Methods("GET")
	router.HandleFunc("/api/auth/setup-status", pa.handleSetupStatus).Methods("GET")
}

// --- Handler: POST /api/auth/register ---

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type authResponse struct {
	User         authUserResponse `json:"user"`
	Org          authOrgResponse  `json:"org"`
	AccessToken  string           `json:"access_token,omitempty"`
	ExpiresIn    int              `json:"expires_in"`
}

type authUserResponse struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	PlatformRole string `json:"platform_role,omitempty"`
}

type authOrgResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (pa *PlatformAuth) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate input
	if err := validateEmail(req.Email); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePassword(req.Password); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = strings.Split(req.Email, "@")[0]
	}

	// Check if registration is allowed
	if !pa.authCfg.IsRegistrationAllowed() {
		respondError(w, http.StatusForbidden, "registration is disabled; contact your administrator for an invitation")
		return
	}

	ctx := r.Context()

	// Check if email is already taken
	existing, _ := pa.pgStore.Users().GetByEmail(ctx, req.Email)
	if existing != nil {
		respondError(w, http.StatusConflict, "email already registered")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to process password")
		return
	}

	// Create user
	user := &store.User{
		ID:           uuid.New().String(),
		Email:        strings.ToLower(strings.TrimSpace(req.Email)),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		PasswordHash: string(hash),
		Status:       "active",
		CreatedAt:    time.Now(),
	}

	if err := pa.pgStore.Users().Create(ctx, user); err != nil {
		slog.Error("failed to create user", "email", user.Email, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Auto-provision org if this is the first user
	org, role, err := pa.ensureOrgForUser(ctx, user)
	if err != nil {
		slog.Error("failed to provision org for user", "user", user.ID, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to provision organization")
		return
	}

	// Issue tokens and set cookies
	pa.issueTokensAndRespond(w, r, user, org, role)
}

// --- Handler: POST /api/auth/login ---

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (pa *PlatformAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	ctx := r.Context()

	// Find user by email
	user, err := pa.pgStore.Users().GetByEmail(ctx, strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil || user == nil {
		respondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// Check account status
	if user.Status != "active" {
		respondError(w, http.StatusForbidden, "account is suspended")
		return
	}

	// Verify password
	if user.PasswordHash == "" {
		// OIDC-only user, no password set
		respondError(w, http.StatusUnauthorized, "this account uses external login (OIDC)")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// Update last login time
	user.LastLoginAt = time.Now()
	_ = pa.pgStore.Users().Update(ctx, user)

	// Get user's org
	orgs, err := pa.pgStore.Organizations().GetUserOrgs(ctx, user.ID)
	if err != nil || len(orgs) == 0 {
		respondError(w, http.StatusInternalServerError, "user has no organization membership")
		return
	}

	// Use first org (multi-org selection could be added later)
	membership := orgs[0]
	org, err := pa.pgStore.Organizations().GetByID(ctx, membership.OrgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load organization")
		return
	}

	pa.issueTokensAndRespond(w, r, user, org, membership.Role)
}

// --- Handler: POST /api/auth/refresh ---

func (pa *PlatformAuth) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// Read refresh token from cookie
	cookie, err := r.Cookie("astonish_refresh")
	if err != nil {
		respondError(w, http.StatusUnauthorized, "no refresh token")
		return
	}

	// Validate the refresh token
	claims, err := pa.jwt.ValidateRefreshToken(cookie.Value)
	if err != nil {
		if errors.Is(err, ErrTokenExpired) {
			// Clear the expired cookie
			clearAuthCookies(w)
			respondError(w, http.StatusUnauthorized, "refresh token expired; please login again")
			return
		}
		respondError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	ctx := r.Context()

	// Verify user still exists and is active
	user, err := pa.pgStore.Users().GetByID(ctx, claims.UserID)
	if err != nil || user == nil || user.Status != "active" {
		clearAuthCookies(w)
		respondError(w, http.StatusUnauthorized, "account not found or suspended")
		return
	}

	// Get current org membership (may have changed since token was issued)
	orgs, err := pa.pgStore.Organizations().GetUserOrgs(ctx, user.ID)
	if err != nil || len(orgs) == 0 {
		clearAuthCookies(w)
		respondError(w, http.StatusUnauthorized, "no organization membership")
		return
	}

	// Find the org from the refresh token claims, or default to first
	var membership *store.OrgMembership
	for _, m := range orgs {
		if m.OrgSlug == claims.OrgSlug {
			membership = m
			break
		}
	}
	if membership == nil {
		membership = orgs[0]
	}

	org, err := pa.pgStore.Organizations().GetByID(ctx, membership.OrgID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load organization")
		return
	}

	// Get user's default team
	teamSlug := pa.resolveDefaultTeam(ctx, user.ID, org)

	// Issue new access token
	accessToken, err := pa.jwt.IssueAccessToken(
		user.ID, user.Email, user.DisplayName,
		org.Slug, teamSlug, membership.Role, user.PlatformRole,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	setAccessTokenCookie(w, accessToken, pa.jwt.AccessTokenTTL())

	respondJSON(w, http.StatusOK, authResponse{
		User: authUserResponse{
			ID:           user.ID,
			Email:        user.Email,
			DisplayName:  user.DisplayName,
			Role:         membership.Role,
			PlatformRole: user.PlatformRole,
		},
		Org: authOrgResponse{
			ID:   org.ID,
			Name: org.Name,
			Slug: org.Slug,
		},
		ExpiresIn: int(pa.jwt.AccessTokenTTL().Seconds()),
	})
}

// --- Handler: POST /api/auth/logout ---

func (pa *PlatformAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookies(w)
	respondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// --- Handler: GET /api/auth/me ---

func (pa *PlatformAuth) handleMe(w http.ResponseWriter, r *http.Request) {
	// Read access token from cookie
	cookie, err := r.Cookie("astonish_access")
	if err != nil {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	claims, err := pa.jwt.ValidateAccessToken(cookie.Value)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"user": authUserResponse{
			ID:           claims.UserID,
			Email:        claims.Email,
			DisplayName:  claims.DisplayName,
			Role:         claims.Role,
			PlatformRole: claims.PlatformRole,
		},
		"org": authOrgResponse{
			Name: "",
			Slug: claims.OrgSlug,
		},
		"team": claims.DefaultTeamSlug,
	})
}

// --- Handler: GET /api/auth/setup-status ---
// Returns whether the platform has been set up (has at least one org).
// Used by the frontend to decide whether to show register or login.

func (pa *PlatformAuth) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count, err := pa.pgStore.Organizations().Count(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check setup status")
		return
	}

	resp := map[string]any{
		"initialized":        count > 0,
		"allow_registration": pa.authCfg.IsRegistrationAllowed(),
		"auth_mode":          pa.authCfg.Mode,
	}

	// Check if migration is available (fresh DB + file data exists)
	if count == 0 && pa.migrationMgr != nil {
		resp["migration_available"] = pa.migrationMgr.IsMigrationAvailable()
	}

	respondJSON(w, http.StatusOK, resp)
}

// --- Internal helpers ---

// ensureOrgForUser handles org provisioning and membership for a new user.
// If no org exists, one is auto-created (first-user setup).
// If an org exists, the user is added as a member (when registration is allowed).
func (pa *PlatformAuth) ensureOrgForUser(ctx context.Context, user *store.User) (*store.Organization, string, error) {
	orgStore := pa.pgStore.Organizations()

	// Check if any org exists
	count, err := orgStore.Count(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to count orgs: %w", err)
	}

	if count == 0 {
		// First user: auto-provision organization
		return pa.provisionFirstOrg(ctx, user)
	}

	// Org exists: add user as member to the default org
	slug := pa.authCfg.GetDefaultOrgSlug()
	org, err := orgStore.GetBySlug(ctx, slug)
	if err != nil {
		return nil, "", fmt.Errorf("default org %q not found: %w", slug, err)
	}

	role := "member"
	if err := orgStore.AddMember(ctx, user.ID, org.ID, role); err != nil {
		return nil, "", fmt.Errorf("failed to add user to org: %w", err)
	}

	// Create default team membership
	pa.ensureDefaultTeamMembership(ctx, user.ID, org)

	return org, role, nil
}

// provisionFirstOrg creates the default org, team, and makes the user an owner.
func (pa *PlatformAuth) provisionFirstOrg(ctx context.Context, user *store.User) (*store.Organization, string, error) {
	orgSlug := pa.authCfg.GetDefaultOrgSlug()
	orgName := pa.authCfg.GetDefaultOrgName()
	dbName := pgstore.OrgDBName(pa.pgStore.InstanceSuffix(), orgSlug)

	org := &store.Organization{
		ID:        uuid.New().String(),
		Name:      orgName,
		Slug:      orgSlug,
		DBName:    dbName,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	// Create org record in platform DB
	if err := pa.pgStore.Organizations().Create(ctx, org); err != nil {
		return nil, "", fmt.Errorf("failed to create org: %w", err)
	}

	// Provision org database (CREATE DATABASE + run migrations)
	if err := pa.pgStore.ProvisionOrg(ctx, org.ID, orgSlug); err != nil {
		return nil, "", fmt.Errorf("failed to provision org database: %w", err)
	}

	// Make user the org owner
	role := "owner"
	if err := pa.pgStore.Organizations().AddMember(ctx, user.ID, org.ID, role); err != nil {
		return nil, "", fmt.Errorf("failed to add owner to org: %w", err)
	}

	// First user is automatically promoted to platform superadmin
	if err := pa.pgStore.Users().SetPlatformRole(ctx, user.ID, "superadmin"); err != nil {
		slog.Warn("failed to set platform superadmin role on first user", "error", err)
	} else {
		user.PlatformRole = "superadmin"
	}

	// Create default team within the org
	orgDataStore, err := pa.pgStore.ForOrg(orgSlug)
	if err != nil {
		slog.Warn("failed to get org data store for default team creation", "error", err)
		return org, role, nil
	}

	defaultTeam := &store.Team{
		ID:         uuid.New().String(),
		Name:       "General",
		Slug:       "general",
		SchemaName: pgstore.TeamSchemaName("general"),
		CreatedAt:  time.Now(),
	}
	if err := orgDataStore.Teams().CreateTeam(ctx, defaultTeam); err != nil {
		slog.Warn("failed to create default team", "error", err)
		return org, role, nil
	}

	// Provision team schema
	if err := orgDataStore.ProvisionTeam(ctx, "general"); err != nil {
		slog.Warn("failed to provision default team schema", "error", err)
		return org, role, nil
	}

	// Add user to default team as admin
	if err := orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   user.ID,
		TeamID:   defaultTeam.ID,
		Role:     "admin",
		JoinedAt: time.Now(),
	}); err != nil {
		slog.Warn("failed to add user to default team", "error", err)
	}

	// Provision personal schema for the user
	if err := orgDataStore.ProvisionPersonalSchema(ctx, user.ID); err != nil {
		slog.Warn("failed to provision personal schema", "error", err)
	}

	slog.Info("first-user setup complete",
		"user", user.Email,
		"org", orgSlug,
		"team", "general",
	)

	return org, role, nil
}

// ensureDefaultTeamMembership adds a new user to the default team if they're not already a member.
func (pa *PlatformAuth) ensureDefaultTeamMembership(ctx context.Context, userID string, org *store.Organization) {
	orgDataStore, err := pa.pgStore.ForOrg(org.Slug)
	if err != nil {
		return
	}

	// Find the "general" team (or first team)
	teams, err := orgDataStore.Teams().ListTeams(ctx)
	if err != nil || len(teams) == 0 {
		return
	}

	defaultTeam := teams[0]
	for _, t := range teams {
		if t.Slug == "general" {
			defaultTeam = t
			break
		}
	}

	_ = orgDataStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   userID,
		TeamID:   defaultTeam.ID,
		Role:     "member",
		JoinedAt: time.Now(),
	})

	// Provision personal schema for the user
	_ = orgDataStore.ProvisionPersonalSchema(ctx, userID)
}

// resolveDefaultTeam finds the user's default team slug within an org.
func (pa *PlatformAuth) resolveDefaultTeam(ctx context.Context, userID string, org *store.Organization) string {
	orgDataStore, err := pa.pgStore.ForOrg(org.Slug)
	if err != nil {
		return "general"
	}

	memberships, err := orgDataStore.Teams().GetUserTeams(ctx, userID)
	if err != nil || len(memberships) == 0 {
		return "general"
	}

	// Find the team slug for the first membership
	team, err := orgDataStore.Teams().GetTeam(ctx, memberships[0].TeamID)
	if err != nil || team == nil {
		return "general"
	}
	return team.Slug
}

// issueTokensAndRespond creates JWT tokens, sets cookies, and sends the auth response.
func (pa *PlatformAuth) issueTokensAndRespond(w http.ResponseWriter, r *http.Request, user *store.User, org *store.Organization, role string) {
	ctx := r.Context()
	teamSlug := pa.resolveDefaultTeam(ctx, user.ID, org)

	// Issue access token
	accessToken, err := pa.jwt.IssueAccessToken(
		user.ID, user.Email, user.DisplayName,
		org.Slug, teamSlug, role, user.PlatformRole,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to issue access token")
		return
	}

	// Issue refresh token
	refreshToken, err := pa.jwt.IssueRefreshToken(user.ID, org.Slug)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to issue refresh token")
		return
	}

	// Store refresh token hash in login_sessions for revocation support
	tokenHash := hashRefreshToken(refreshToken)
	loginSession := &store.LoginSession{
		TokenHash: tokenHash,
		UserID:    user.ID,
		OrgID:     org.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(pa.jwt.RefreshTokenTTL()),
		UserAgent: r.UserAgent(),
		IPAddress: clientIP(r),
	}
	if err := pa.pgStore.LoginSessions().Create(ctx, loginSession); err != nil {
		slog.Warn("failed to persist login session", "error", err)
		// Non-fatal: tokens still work, just can't be revoked
	}

	// Set cookies
	setAccessTokenCookie(w, accessToken, pa.jwt.AccessTokenTTL())
	setRefreshTokenCookie(w, refreshToken, pa.jwt.RefreshTokenTTL())

	respondJSON(w, http.StatusOK, authResponse{
		User: authUserResponse{
			ID:           user.ID,
			Email:        user.Email,
			DisplayName:  user.DisplayName,
			Role:         role,
			PlatformRole: user.PlatformRole,
		},
		Org: authOrgResponse{
			ID:   org.ID,
			Name: org.Name,
			Slug: org.Slug,
		},
		ExpiresIn: int(pa.jwt.AccessTokenTTL().Seconds()),
	})
}

// --- Cookie helpers ---

const (
	accessCookieName  = "astonish_access"
	refreshCookieName = "astonish_refresh"
)

func setAccessTokenCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false, // Set true in production with HTTPS
	})
}

func setRefreshTokenCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/api/auth/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false, // Set true in production with HTTPS
	})
}

func clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/auth/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// --- Validation helpers ---

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func validateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("email is required")
	}
	if !emailRegex.MatchString(email) {
		return errors.New("invalid email format")
	}
	if len(email) > 254 {
		return errors.New("email too long")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return errors.New("password too long")
	}
	return nil
}

func hashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func clientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return strings.TrimPrefix(strings.TrimSuffix(ip, "]"), "[")
}
