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

// respondError writes a JSON error response with the given status code.
// The response body is {"error": "<msg>"} with Content-Type: application/json.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
