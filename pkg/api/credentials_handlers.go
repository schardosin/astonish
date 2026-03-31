package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/credentials"
)

// --- Credential API types ---

// credentialListItem represents a single credential in the list response.
type credentialListItem struct {
	Name string                     `json:"name"`
	Type credentials.CredentialType `json:"type"`
}

// credentialListResponse is the response for GET /api/credentials.
type credentialListResponse struct {
	Credentials  []credentialListItem `json:"credentials"`
	Secrets      []string             `json:"secrets"`
	HasMasterKey bool                 `json:"has_master_key"`
}

// credentialDetailResponse returns full credential fields (for reveal).
type credentialDetailResponse struct {
	Name         string                     `json:"name"`
	Type         credentials.CredentialType `json:"type"`
	Header       string                     `json:"header,omitempty"`
	Value        string                     `json:"value,omitempty"`
	Token        string                     `json:"token,omitempty"`
	Username     string                     `json:"username,omitempty"`
	Password     string                     `json:"password,omitempty"`
	AuthURL      string                     `json:"auth_url,omitempty"`
	ClientID     string                     `json:"client_id,omitempty"`
	ClientSecret string                     `json:"client_secret,omitempty"`
	Scope        string                     `json:"scope,omitempty"`
	TokenURL     string                     `json:"token_url,omitempty"`
	AccessToken  string                     `json:"access_token,omitempty"`
	RefreshToken string                     `json:"refresh_token,omitempty"`
	TokenExpiry  string                     `json:"token_expiry,omitempty"`
}

// credentialSaveRequest is the body for POST /api/credentials.
type credentialSaveRequest struct {
	Name       string                 `json:"name"`
	Credential credentials.Credential `json:"credential"`
}

// secretSaveRequest is the body for PUT /api/secrets/{key}.
type secretSaveRequest struct {
	Value string `json:"value"`
}

// masterKeyRequest is the body for POST /api/credentials/master-key.
type masterKeyRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// verifyMasterKeyRequest is the body for POST /api/credentials/verify-master-key.
type verifyMasterKeyRequest struct {
	Password string `json:"password"`
}

// --- Helpers ---

// requireMasterKey checks the X-Master-Key header against the store.
// Returns true if access is granted (no master key set, or valid key provided).
// Returns false and writes an HTTP error if access is denied.
func requireMasterKey(w http.ResponseWriter, r *http.Request, store *credentials.Store) bool {
	if !store.HasMasterKey() {
		return true
	}
	masterKey := r.Header.Get("X-Master-Key")
	if masterKey == "" {
		respondError(w, http.StatusForbidden, `{"error":"master_key_required"}`)
		return false
	}
	if !store.VerifyMasterKey(masterKey) {
		respondError(w, http.StatusForbidden, `{"error":"invalid_master_key"}`)
		return false
	}
	return true
}

// --- Handlers ---

// ListCredentialsHandler returns all credential names/types and secret keys.
// GET /api/credentials
func ListCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	// Reload to pick up cross-process changes
	if err := store.Reload(); err != nil {
		slog.Warn("failed to reload credential store", "error", err)
	}

	creds := store.List()
	secrets := store.ListSecrets()
	sort.Strings(secrets)

	items := make([]credentialListItem, 0, len(creds))
	for name, credType := range creds {
		items = append(items, credentialListItem{Name: name, Type: credType})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	resp := credentialListResponse{
		Credentials:  items,
		Secrets:      secrets,
		HasMasterKey: store.HasMasterKey(),
	}

	respondJSON(w, http.StatusOK, resp)
}

// GetCredentialHandler reveals a credential's full values.
// GET /api/credentials/{name}
func GetCredentialHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	if !requireMasterKey(w, r, store) {
		return
	}

	name := mux.Vars(r)["name"]
	if err := store.Reload(); err != nil {
		slog.Warn("failed to reload credential store", "error", err)
	}

	cred := store.Get(name)
	if cred == nil {
		respondError(w, http.StatusNotFound, "Credential not found")
		return
	}

	resp := credentialDetailResponse{
		Name:         name,
		Type:         cred.Type,
		Header:       cred.Header,
		Value:        cred.Value,
		Token:        cred.Token,
		Username:     cred.Username,
		Password:     cred.Password,
		AuthURL:      cred.AuthURL,
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		Scope:        cred.Scope,
		TokenURL:     cred.TokenURL,
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		TokenExpiry:  cred.TokenExpiry,
	}

	respondJSON(w, http.StatusOK, resp)
}

// SaveCredentialHandler creates or updates a named credential.
// POST /api/credentials
func SaveCredentialHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	var req credentialSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Credential name is required")
		return
	}

	if err := store.Set(req.Name, &req.Credential); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save credential: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": req.Name})
}

// DeleteCredentialHandler removes a credential or flat secret.
// DELETE /api/credentials/{name}
func DeleteCredentialHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	name := mux.Vars(r)["name"]

	// Try HTTP credential first
	if store.Get(name) != nil {
		if err := store.Remove(name); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to remove credential: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "removed": name})
		return
	}

	// Try flat secret
	if store.GetSecret(name) != "" {
		if err := store.RemoveSecret(name); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to remove secret: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "removed": name})
		return
	}

	respondError(w, http.StatusNotFound, "Credential or secret not found")
}

// GetSecretHandler reveals a flat secret value.
// GET /api/secrets/{key}
func GetSecretHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	if !requireMasterKey(w, r, store) {
		return
	}

	key := mux.Vars(r)["key"]
	if err := store.Reload(); err != nil {
		slog.Warn("failed to reload credential store", "error", err)
	}

	value := store.GetSecret(key)
	if value == "" {
		respondError(w, http.StatusNotFound, "Secret not found")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"key": key, "value": value})
}

// SaveSecretHandler creates or updates a flat secret.
// PUT /api/secrets/{key}
func SaveSecretHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	key := mux.Vars(r)["key"]

	var req secretSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	if req.Value == "" {
		respondError(w, http.StatusBadRequest, "Secret value is required")
		return
	}

	if err := store.SetSecret(key, req.Value); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save secret: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "key": key})
}

// SetMasterKeyHandler sets, changes, or removes the master key.
// POST /api/credentials/master-key
func SetMasterKeyHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	var req masterKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// If a master key already exists, verify the current password
	if store.HasMasterKey() {
		if !store.VerifyMasterKey(req.Current) {
			respondError(w, http.StatusForbidden, `{"error":"invalid_current_key"}`)
			return
		}
	}

	if err := store.SetMasterKey(req.New); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to set master key: "+err.Error())
		return
	}

	action := "set"
	if req.New == "" {
		action = "removed"
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": action})
}

// VerifyMasterKeyHandler checks if a password matches the master key.
// POST /api/credentials/verify-master-key
func VerifyMasterKeyHandler(w http.ResponseWriter, r *http.Request) {
	store := getAPICredentialStore()
	if store == nil {
		respondError(w, http.StatusServiceUnavailable, "Credential store not available")
		return
	}

	var req verifyMasterKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	valid := store.VerifyMasterKey(req.Password)

	respondJSON(w, http.StatusOK, map[string]bool{"valid": valid})
}
