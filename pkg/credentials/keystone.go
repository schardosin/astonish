package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultKeystoneDomain = "Default"

// keystoneAuthRequest is the top-level Keystone v3 auth request body.
type keystoneAuthRequest struct {
	Auth keystoneAuth `json:"auth"`
}

type keystoneAuth struct {
	Identity keystoneIdentity `json:"identity"`
	Scope    *keystoneScope   `json:"scope,omitempty"`
}

type keystoneIdentity struct {
	Methods               []string                       `json:"methods"`
	Password              *keystonePasswordAuth          `json:"password,omitempty"`
	ApplicationCredential *keystoneApplicationCredential `json:"application_credential,omitempty"`
}

type keystonePasswordAuth struct {
	User keystoneUser `json:"user"`
}

type keystoneUser struct {
	Name     string         `json:"name"`
	Password string         `json:"password"`
	Domain   keystoneDomain `json:"domain"`
}

type keystoneApplicationCredential struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

type keystoneScope struct {
	Project keystoneProject `json:"project"`
}

type keystoneProject struct {
	ID     string          `json:"id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Domain *keystoneDomain `json:"domain,omitempty"`
}

type keystoneDomain struct {
	Name string `json:"name"`
}

// keystoneTokenResponse is the subset of the Keystone token response we need.
type keystoneTokenResponse struct {
	Token struct {
		ExpiresAt string `json:"expires_at"`
	} `json:"token"`
	Error *struct {
		Code    int    `json:"code"`
		Title   string `json:"title"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// keystoneUsesAppCred reports whether the credential is configured for
// application_credential auth (takes precedence over password when both set).
func keystoneUsesAppCred(cred *Credential) bool {
	return cred.ApplicationCredentialID != "" && cred.ApplicationCredentialSecret != ""
}

// validateKeystoneCredential checks that required fields are present for the
// selected Keystone auth method.
func validateKeystoneCredential(cred *Credential) error {
	if cred.AuthURL == "" {
		return fmt.Errorf("auth_url is required for openstack_keystone credentials")
	}
	if keystoneUsesAppCred(cred) {
		return nil
	}
	if cred.Username == "" || cred.Password == "" {
		return fmt.Errorf("openstack_keystone requires either application_credential_id+application_credential_secret, or username+password")
	}
	if cred.ProjectID == "" && cred.ProjectName == "" {
		return fmt.Errorf("openstack_keystone password auth requires project_id or project_name")
	}
	return nil
}

// buildKeystoneAuthBody builds the Keystone v3 auth JSON for the credential.
func buildKeystoneAuthBody(cred *Credential) ([]byte, error) {
	if err := validateKeystoneCredential(cred); err != nil {
		return nil, err
	}

	var auth keystoneAuth
	if keystoneUsesAppCred(cred) {
		auth.Identity = keystoneIdentity{
			Methods: []string{"application_credential"},
			ApplicationCredential: &keystoneApplicationCredential{
				ID:     cred.ApplicationCredentialID,
				Secret: cred.ApplicationCredentialSecret,
			},
		}
	} else {
		userDomain := cred.UserDomain
		if userDomain == "" {
			userDomain = defaultKeystoneDomain
		}
		auth.Identity = keystoneIdentity{
			Methods: []string{"password"},
			Password: &keystonePasswordAuth{
				User: keystoneUser{
					Name:     cred.Username,
					Password: cred.Password,
					Domain:   keystoneDomain{Name: userDomain},
				},
			},
		}
		project := keystoneProject{}
		if cred.ProjectID != "" {
			project.ID = cred.ProjectID
		} else {
			projectDomain := cred.ProjectDomain
			if projectDomain == "" {
				projectDomain = defaultKeystoneDomain
			}
			project.Name = cred.ProjectName
			project.Domain = &keystoneDomain{Name: projectDomain}
		}
		auth.Scope = &keystoneScope{Project: project}
	}

	return json.Marshal(keystoneAuthRequest{Auth: auth})
}

// FetchKeystoneToken performs a Keystone v3 token request.
// The token is read from the X-Subject-Token response header; expiry comes
// from the response body token.expires_at (ISO8601).
func FetchKeystoneToken(cred *Credential) (token string, expiresAt time.Time, err error) {
	body, err := buildKeystoneAuthBody(cred)
	if err != nil {
		return "", time.Time{}, err
	}

	req, err := http.NewRequest("POST", cred.AuthURL, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build Keystone token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: oauthTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("Keystone token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read Keystone token response: %w", err)
	}

	token = resp.Header.Get("X-Subject-Token")
	if token == "" {
		var errResp keystoneTokenResponse
		_ = json.Unmarshal(respBody, &errResp)
		if errResp.Error != nil {
			return "", time.Time{}, fmt.Errorf("Keystone error: %s (%s)", errResp.Error.Title, errResp.Error.Message)
		}
		return "", time.Time{}, fmt.Errorf("Keystone response missing X-Subject-Token (status %d)", resp.StatusCode)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("Keystone token request failed with status %d", resp.StatusCode)
	}

	var tokenResp keystoneTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("parse Keystone token response: %w", err)
	}

	if tokenResp.Token.ExpiresAt != "" {
		expiresAt, err = time.Parse(time.RFC3339, tokenResp.Token.ExpiresAt)
		if err != nil {
			// Keystone sometimes returns fractional seconds; try with Nano layout.
			expiresAt, err = time.Parse("2006-01-02T15:04:05.999999Z", tokenResp.Token.ExpiresAt)
			if err != nil {
				expiresAt = time.Now().Add(time.Hour)
			}
		}
	} else {
		expiresAt = time.Now().Add(time.Hour)
	}

	return token, expiresAt, nil
}

// GetOrRefreshKeystone returns a valid Keystone token for the named credential.
// If the cached token is still valid, it is returned immediately.
// Otherwise a new token is acquired via FetchKeystoneToken.
func (tc *tokenCache) GetOrRefreshKeystone(name string, cred *Credential, redactor *Redactor) (string, error) {
	tc.mu.RLock()
	if cached, ok := tc.tokens[name]; ok {
		if time.Now().Before(cached.expiresAt) {
			tc.mu.RUnlock()
			return cached.accessToken, nil
		}
	}
	tc.mu.RUnlock()

	token, expiresAt, err := FetchKeystoneToken(cred)
	if err != nil {
		return "", err
	}

	expiresAt = expiresAt.Add(-tokenExpiryBuffer)
	if expiresAt.Before(time.Now()) {
		expiresAt = time.Now().Add(10 * time.Second)
	}

	tc.mu.Lock()
	tc.tokens[name] = &cachedToken{
		accessToken: token,
		expiresAt:   expiresAt,
	}
	tc.mu.Unlock()

	if redactor != nil {
		redactor.AddSecret(name+"/token", token)
	}

	return token, nil
}
