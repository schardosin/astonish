//go:build e2e

package e2eboot

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func registerUser(t *testing.T, baseURL string) {
	t.Helper()
	body := map[string]string{
		"email":    defaultEmail,
		"password": defaultPassword,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", baseURL+"/api/auth/register", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("[e2eboot] create register request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("[e2eboot] register request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("[e2eboot] register failed: %d %s", resp.StatusCode, string(respBody))
	}
	t.Log("[e2eboot] User registered successfully")
}

func loginUser(t *testing.T, baseURL string) string {
	t.Helper()
	body := map[string]string{
		"email":       defaultEmail,
		"password":    defaultPassword,
		"client_type": "cli",
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", baseURL+"/api/auth/login", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("[e2eboot] create login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("[e2eboot] login request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("[e2eboot] login failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("[e2eboot] decode login response: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("[e2eboot] login returned empty access_token")
	}
	return result.AccessToken
}
