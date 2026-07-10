package api

import (
	"encoding/json"
	"net/http"

	"github.com/schardosin/astonish/pkg/store"
)

type userDefaultModelPayload struct {
	DefaultProvider string `json:"defaultProvider"`
	DefaultModel    string `json:"defaultModel"`
}

func GetUserDefaultModelHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.PersonalSettings == nil {
		http.Error(w, "personal settings store unavailable", http.StatusServiceUnavailable)
		return
	}

	settings, err := svc.PersonalSettings.Get(r.Context())
	if err != nil {
		http.Error(w, "failed to load user settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := userDefaultModelPayload{
		DefaultProvider: settings.DefaultProvider,
		DefaultModel:    settings.DefaultModel,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func PatchUserDefaultModelHandler(w http.ResponseWriter, r *http.Request) {
	svc := store.FromRequest(r)
	if svc == nil || svc.PersonalSettings == nil {
		http.Error(w, "personal settings store unavailable", http.StatusServiceUnavailable)
		return
	}

	var body userDefaultModelPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := svc.PersonalSettings.Save(r.Context(), &store.PersonalSettings{
		DefaultProvider: body.DefaultProvider,
		DefaultModel:    body.DefaultModel,
	}); err != nil {
		http.Error(w, "failed to save user settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
