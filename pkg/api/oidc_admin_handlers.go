package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/schardosin/astonish/pkg/store"
)

// --- OIDC Provider Admin CRUD Endpoints ---
// All handlers require platform superadmin access.

// PlatformAdminListOIDCProvidersHandler handles GET /api/platform/admin/oidc-providers
func PlatformAdminListOIDCProvidersHandler(w http.ResponseWriter, r *http.Request) {
	if requirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	providers, err := pgStore.OIDCProviders().List(r.Context(), "*")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list OIDC providers")
		return
	}
	if providers == nil {
		providers = []*store.OIDCProvider{}
	}

	// Redact client secrets in list response
	type providerResponse struct {
		ID        string   `json:"id"`
		OrgID     string   `json:"org_id,omitempty"`
		Name      string   `json:"name"`
		IssuerURL string   `json:"issuer_url"`
		ClientID  string   `json:"client_id"`
		Scopes    []string `json:"scopes"`
		TeamClaim string   `json:"team_claim,omitempty"`
		Enabled   bool     `json:"enabled"`
		CreatedAt string   `json:"created_at"`
	}

	var resp []providerResponse
	for _, p := range providers {
		resp = append(resp, providerResponse{
			ID:        p.ID,
			OrgID:     p.OrgID,
			Name:      p.Name,
			IssuerURL: p.IssuerURL,
			ClientID:  p.ClientID,
			Scopes:    p.Scopes,
			TeamClaim: p.TeamClaim,
			Enabled:   p.Enabled,
			CreatedAt: p.CreatedAt.Format(time.RFC3339),
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"providers": resp})
}

// PlatformAdminCreateOIDCProviderHandler handles POST /api/platform/admin/oidc-providers
func PlatformAdminCreateOIDCProviderHandler(w http.ResponseWriter, r *http.Request) {
	if requirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	var req struct {
		OrgID        string   `json:"org_id"`
		Name         string   `json:"name"`
		IssuerURL    string   `json:"issuer_url"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Scopes       []string `json:"scopes"`
		TeamClaim    string   `json:"team_claim"`
		Enabled      *bool    `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validation
	if req.IssuerURL == "" {
		respondError(w, http.StatusBadRequest, "issuer_url is required")
		return
	}
	if req.ClientID == "" {
		respondError(w, http.StatusBadRequest, "client_id is required")
		return
	}
	if req.ClientSecret == "" {
		respondError(w, http.StatusBadRequest, "client_secret is required")
		return
	}

	// Normalize issuer URL (remove trailing slash)
	req.IssuerURL = strings.TrimRight(req.IssuerURL, "/")

	// Default values
	if req.Name == "" {
		req.Name = req.IssuerURL
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"openid", "email", "profile"}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	provider := &store.OIDCProvider{
		ID:           uuid.New().String(),
		OrgID:        req.OrgID,
		Name:         req.Name,
		IssuerURL:    req.IssuerURL,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		Scopes:       req.Scopes,
		TeamClaim:    req.TeamClaim,
		Enabled:      enabled,
		CreatedAt:    time.Now(),
	}

	if err := pgStore.OIDCProviders().Create(r.Context(), provider); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			respondError(w, http.StatusConflict, "OIDC provider with this issuer URL already exists for this org")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to create OIDC provider")
		return
	}

	// Return without secret
	respondJSON(w, http.StatusCreated, map[string]any{
		"provider": map[string]any{
			"id":         provider.ID,
			"org_id":     provider.OrgID,
			"name":       provider.Name,
			"issuer_url": provider.IssuerURL,
			"client_id":  provider.ClientID,
			"scopes":     provider.Scopes,
			"team_claim": provider.TeamClaim,
			"enabled":    provider.Enabled,
			"created_at": provider.CreatedAt.Format(time.RFC3339),
		},
	})
}

// PlatformAdminGetOIDCProviderHandler handles GET /api/platform/admin/oidc-providers/{id}
func PlatformAdminGetOIDCProviderHandler(w http.ResponseWriter, r *http.Request) {
	if requirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	id := mux.Vars(r)["id"]
	provider, err := pgStore.OIDCProviders().GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "OIDC provider not found")
		return
	}

	// Return with masked secret (show first 4 chars)
	maskedSecret := "****"
	if len(provider.ClientSecret) > 4 {
		maskedSecret = provider.ClientSecret[:4] + "****"
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"provider": map[string]any{
			"id":            provider.ID,
			"org_id":        provider.OrgID,
			"name":          provider.Name,
			"issuer_url":    provider.IssuerURL,
			"client_id":     provider.ClientID,
			"client_secret": maskedSecret,
			"scopes":        provider.Scopes,
			"team_claim":    provider.TeamClaim,
			"enabled":       provider.Enabled,
			"created_at":    provider.CreatedAt.Format(time.RFC3339),
		},
	})
}

// PlatformAdminUpdateOIDCProviderHandler handles PATCH /api/platform/admin/oidc-providers/{id}
func PlatformAdminUpdateOIDCProviderHandler(w http.ResponseWriter, r *http.Request) {
	if requirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	id := mux.Vars(r)["id"]
	provider, err := pgStore.OIDCProviders().GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "OIDC provider not found")
		return
	}

	var req struct {
		OrgID        *string  `json:"org_id"`
		Name         *string  `json:"name"`
		IssuerURL    *string  `json:"issuer_url"`
		ClientID     *string  `json:"client_id"`
		ClientSecret *string  `json:"client_secret"`
		Scopes       []string `json:"scopes"`
		TeamClaim    *string  `json:"team_claim"`
		Enabled      *bool    `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply partial updates
	if req.OrgID != nil {
		provider.OrgID = *req.OrgID
	}
	if req.Name != nil {
		provider.Name = *req.Name
	}
	if req.IssuerURL != nil {
		provider.IssuerURL = strings.TrimRight(*req.IssuerURL, "/")
	}
	if req.ClientID != nil {
		provider.ClientID = *req.ClientID
	}
	if req.ClientSecret != nil {
		provider.ClientSecret = *req.ClientSecret
	}
	if req.Scopes != nil {
		provider.Scopes = req.Scopes
	}
	if req.TeamClaim != nil {
		provider.TeamClaim = *req.TeamClaim
	}
	if req.Enabled != nil {
		provider.Enabled = *req.Enabled
	}

	if err := pgStore.OIDCProviders().Update(r.Context(), provider); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update OIDC provider")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"provider": map[string]any{
			"id":         provider.ID,
			"org_id":     provider.OrgID,
			"name":       provider.Name,
			"issuer_url": provider.IssuerURL,
			"client_id":  provider.ClientID,
			"scopes":     provider.Scopes,
			"team_claim": provider.TeamClaim,
			"enabled":    provider.Enabled,
			"created_at": provider.CreatedAt.Format(time.RFC3339),
		},
	})
}

// PlatformAdminDeleteOIDCProviderHandler handles DELETE /api/platform/admin/oidc-providers/{id}
func PlatformAdminDeleteOIDCProviderHandler(w http.ResponseWriter, r *http.Request) {
	if requirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	id := mux.Vars(r)["id"]

	// Verify it exists
	if _, err := pgStore.OIDCProviders().GetByID(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, "OIDC provider not found")
		return
	}

	if err := pgStore.OIDCProviders().Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete OIDC provider")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": "OIDC provider deleted",
	})
}
