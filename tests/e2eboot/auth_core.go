// auth_core.go contains testing.T-free versions of registerUser / loginUser
// for use by the inspector binary.
package e2eboot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// registerBootstrapUser POSTs /api/auth/register with the default credentials.
// Returns nil on success or 2xx; a wrapped error otherwise (including "already
// exists" — caller may choose to ignore).
func registerBootstrapUser(baseURL string) error {
	body := map[string]string{
		"email":    InspectorDefaultEmail,
		"password": InspectorDefaultPassword,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", baseURL+"/api/auth/register", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed: %d %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// loginBootstrapUser POSTs /api/auth/login and returns the access token.
func loginBootstrapUser(baseURL string) (string, error) {
	body := map[string]string{
		"email":       InspectorDefaultEmail,
		"password":    InspectorDefaultPassword,
		"client_type": "cli",
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", baseURL+"/api/auth/login", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("login returned empty access_token")
	}
	return result.AccessToken, nil
}
