package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// LoginResult contains the information returned after a successful login.
type LoginResult struct {
	UserEmail   string
	DisplayName string
	OrgSlug     string
	OrgName     string
	TeamSlug    string
	Role        string
}

// LoginWithPassword authenticates with email and password against the remote server.
// It stores the tokens and remote config on success.
func LoginWithPassword(serverURL, email, password string) (*LoginResult, error) {
	c := NewWithConfig(serverURL)

	reqBody := map[string]string{
		"email":       email,
		"password":    password,
		"client_type": "cli",
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
			ID           string `json:"id"`
			Email        string `json:"email"`
			DisplayName  string `json:"display_name"`
			Role         string `json:"role"`
			PlatformRole string `json:"platform_role"`
		} `json:"user"`
		Org struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"org"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}

	if authResp.AccessToken == "" {
		return nil, fmt.Errorf("server did not return tokens (ensure server version supports CLI login)")
	}

	// Determine default team from the access token claims (we'd need to decode JWT,
	// but we can just call /api/auth/me with the new token to get the team)
	teamSlug := getTeamFromMe(c, authResp.AccessToken)

	// Store tokens
	ts, err := NewTokenStore()
	if err != nil {
		return nil, fmt.Errorf("init token store: %w", err)
	}

	tokens := &Tokens{
		AccessToken:      authResp.AccessToken,
		RefreshToken:     authResp.RefreshToken,
		AccessExpiresAt:  time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second),
		RefreshExpiresAt: time.Now().Add(90 * 24 * time.Hour), // 90 days default
	}
	if err := ts.Save(tokens); err != nil {
		return nil, fmt.Errorf("save tokens: %w", err)
	}

	// Save remote config
	cfg := &RemoteConfig{
		URL:       serverURL,
		Org:       authResp.Org.Slug,
		Team:      teamSlug,
		UserEmail: authResp.User.Email,
	}
	if err := SaveRemoteConfig(cfg); err != nil {
		return nil, fmt.Errorf("save remote config: %w", err)
	}

	return &LoginResult{
		UserEmail:   authResp.User.Email,
		DisplayName: authResp.User.DisplayName,
		OrgSlug:     authResp.Org.Slug,
		OrgName:     authResp.Org.Name,
		TeamSlug:    teamSlug,
		Role:        authResp.User.Role,
	}, nil
}

// LoginWithSSO initiates an SSO/OIDC login flow by opening the browser and
// listening for a callback on a local port.
func LoginWithSSO(serverURL string) (*LoginResult, error) {
	// Find a free port for the callback
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)

	// Channel to receive the tokens
	type callbackResult struct {
		accessToken  string
		refreshToken string
		expiresIn    int
		err          error
	}
	resultCh := make(chan callbackResult, 1)

	// Set up HTTP server for the callback
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		accessToken := r.URL.Query().Get("access_token")
		refreshToken := r.URL.Query().Get("refresh_token")
		if accessToken == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no access token in callback"
			}
			resultCh <- callbackResult{err: fmt.Errorf("SSO callback error: %s", errMsg)}
			fmt.Fprintf(w, "<html><body><h2>Login Failed</h2><p>%s</p><p>You can close this tab.</p></body></html>", errMsg)
			return
		}
		resultCh <- callbackResult{
			accessToken:  accessToken,
			refreshToken: refreshToken,
			expiresIn:    900, // default 15 min if not specified
		}
		fmt.Fprint(w, "<html><body><h2>Login Successful</h2><p>You can close this tab and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	// Open browser to the SSO authorize endpoint
	authorizeURL := fmt.Sprintf("%s/api/auth/oidc/authorize?redirect_uri=%s&cli=true", serverURL, callbackURL)
	if err := openBrowser(authorizeURL); err != nil {
		return nil, fmt.Errorf("failed to open browser: %w\nPlease open this URL manually:\n%s", err, authorizeURL)
	}

	// Wait for callback (with timeout)
	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}

		// Get user info from the token
		c := NewWithConfig(serverURL)
		meResp := getMeWithToken(c, result.accessToken)

		// Determine team
		teamSlug := ""
		if meResp != nil {
			teamSlug = meResp.Team
		}

		// Store tokens
		ts, err := NewTokenStore()
		if err != nil {
			return nil, fmt.Errorf("init token store: %w", err)
		}
		tokens := &Tokens{
			AccessToken:      result.accessToken,
			RefreshToken:     result.refreshToken,
			AccessExpiresAt:  time.Now().Add(time.Duration(result.expiresIn) * time.Second),
			RefreshExpiresAt: time.Now().Add(90 * 24 * time.Hour),
		}
		if err := ts.Save(tokens); err != nil {
			return nil, fmt.Errorf("save tokens: %w", err)
		}

		// Build result
		loginResult := &LoginResult{}
		if meResp != nil {
			loginResult.UserEmail = meResp.Email
			loginResult.DisplayName = meResp.DisplayName
			loginResult.OrgSlug = meResp.OrgSlug
			loginResult.TeamSlug = teamSlug
			loginResult.Role = meResp.Role
		}

		// Save remote config
		cfg := &RemoteConfig{
			URL:       serverURL,
			Org:       loginResult.OrgSlug,
			Team:      teamSlug,
			UserEmail: loginResult.UserEmail,
		}
		if err := SaveRemoteConfig(cfg); err != nil {
			return nil, fmt.Errorf("save remote config: %w", err)
		}

		return loginResult, nil

	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("SSO login timed out (5 minutes). Please try again")
	}
}

// Logout clears stored tokens and remote config.
func Logout() error {
	ts, err := NewTokenStore()
	if err == nil {
		_ = ts.Remove()
	}
	return RemoveRemoteConfig()
}

// --- Helpers ---

// meResponse represents the /api/auth/me response.
type meResponse struct {
	Email       string
	DisplayName string
	OrgSlug     string
	Team        string
	Role        string
}

// getTeamFromMe calls /api/auth/me to determine the user's default team.
func getTeamFromMe(c *Client, accessToken string) string {
	me := getMeWithToken(c, accessToken)
	if me != nil {
		return me.Team
	}
	return ""
}

// getMeWithToken calls /api/auth/me with a specific access token.
func getMeWithToken(c *Client, accessToken string) *meResponse {
	req, err := http.NewRequest("GET", c.baseURL+"/api/auth/me", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var body struct {
		User struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
		} `json:"user"`
		Org struct {
			Slug string `json:"slug"`
		} `json:"org"`
		Team string `json:"team"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}

	return &meResponse{
		Email:       body.User.Email,
		DisplayName: body.User.DisplayName,
		OrgSlug:     body.Org.Slug,
		Team:        body.Team,
		Role:        body.User.Role,
	}
}

// openBrowser opens a URL in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
