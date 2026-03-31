package api

import (
	"encoding/json"
	"net/http"
)

// respondJSON writes a JSON response with the given status code.
// It sets the Content-Type header to application/json automatically.
func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload) //nolint:errcheck // best-effort response encoding
}

// respondError writes an HTTP error response. This is a thin wrapper
// around http.Error for consistency with respondJSON.
func respondError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}
