package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TokenResult contains the raw token and metadata from a successful authentication,
// without persisting anything to the local token store or remote config.
type TokenResult struct {
	AccessToken string
	ExpiresIn   int // seconds until expiry
	UserEmail   string
	DisplayName string
	OrgSlug     string
	OrgName     string
	TeamSlug    string
	Role        string
}

// IssueTokenPassword authenticates with email/password and returns the raw access token
// without storing credentials locally. Useful for scripting and CI/CD.
func IssueTokenPassword(serverURL, email, password, org, team string) (*TokenResult, error) {
	c := NewWithConfig(serverURL)

	reqBody := map[string]string{
		"email":       email,
		"password":    password,
		"client_type": "cli",
	}
	if org != "" {
		reqBody["org"] = org
	}
	if team != "" {
		reqBody["team"] = team
	}

	resp, err := c.doOnce("POST", "/api/auth/login", reqBody)
	if err != nil {
		return nil, fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseErrorResponse(resp)
	}

	var authResp struct {
		User struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
		} `json:"user"`
		Org struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"org"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TeamSlug    string `json:"team"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}

	if authResp.AccessToken == "" {
		return nil, fmt.Errorf("server did not return an access token")
	}

	return &TokenResult{
		AccessToken: authResp.AccessToken,
		ExpiresIn:   authResp.ExpiresIn,
		UserEmail:   authResp.User.Email,
		DisplayName: authResp.User.DisplayName,
		OrgSlug:     authResp.Org.Slug,
		OrgName:     authResp.Org.Name,
		TeamSlug:    authResp.TeamSlug,
		Role:        authResp.User.Role,
	}, nil
}

// IssueTokenSSO performs the device-code SSO flow and returns the raw access token
// without storing credentials locally. Opens a browser for authentication.
func IssueTokenSSO(serverURL, providerID string, onStatus func(status string)) (*TokenResult, error) {
	c := NewWithConfig(serverURL)

	// Step 1: Initialize the device flow
	initBody := map[string]string{}
	if providerID != "" {
		initBody["provider_id"] = providerID
	}

	resp, err := c.doOnce("POST", "/api/auth/sso/init", initBody)
	if err != nil {
		return nil, fmt.Errorf("SSO init request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseErrorResponse(resp)
	}

	var initResp struct {
		DeviceCode string `json:"device_code"`
		VerifyURL  string `json:"verify_url"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return nil, fmt.Errorf("decode SSO init response: %w", err)
	}

	// Step 2: Open the verify URL in the browser
	if onStatus != nil {
		onStatus("opening_browser")
	}
	if err := openBrowser(initResp.VerifyURL); err != nil {
		if onStatus != nil {
			onStatus("browser_failed")
		}
	}

	// Step 3: Poll for completion
	interval := time.Duration(initResp.Interval) * time.Second
	if interval < 1*time.Second {
		interval = 2 * time.Second
	}
	timeout := time.Duration(initResp.ExpiresIn) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	deadline := time.Now().Add(timeout)

	if onStatus != nil {
		onStatus("polling")
	}

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		pollResp, pollErr := c.doOnce("POST", "/api/auth/sso/poll", map[string]string{
			"device_code": initResp.DeviceCode,
		})
		if pollErr != nil {
			continue
		}

		var pollResult struct {
			Status      string          `json:"status"`
			Error       string          `json:"error,omitempty"`
			AccessToken string          `json:"access_token,omitempty"`
			ExpiresIn   int             `json:"expires_in,omitempty"`
			User        json.RawMessage `json:"user,omitempty"`
			Org         json.RawMessage `json:"org,omitempty"`
			TeamSlug    string          `json:"team,omitempty"`
		}
		decErr := json.NewDecoder(pollResp.Body).Decode(&pollResult)
		pollResp.Body.Close()
		if decErr != nil {
			continue
		}

		switch pollResult.Status {
		case "pending":
			continue
		case "failed":
			return nil, fmt.Errorf("SSO login failed: %s", pollResult.Error)
		case "complete":
			var user struct {
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
				Role        string `json:"role"`
			}
			var org struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			}
			_ = json.Unmarshal(pollResult.User, &user)
			_ = json.Unmarshal(pollResult.Org, &org)

			return &TokenResult{
				AccessToken: pollResult.AccessToken,
				ExpiresIn:   pollResult.ExpiresIn,
				UserEmail:   user.Email,
				DisplayName: user.DisplayName,
				OrgSlug:     org.Slug,
				OrgName:     org.Name,
				TeamSlug:    pollResult.TeamSlug,
				Role:        user.Role,
			}, nil
		}
	}

	return nil, fmt.Errorf("SSO login timed out — please try again")
}
