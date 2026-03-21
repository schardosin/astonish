package fleet

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/credentials"
)

// ResolvedCredential holds a credential resolved from the encrypted store at runtime.
// The logical name is how agents refer to the credential; the actual secrets are
// injected into the environment and never exposed in prompts.
type ResolvedCredential struct {
	// LogicalName is how the credential is referred to in the fleet plan (e.g., "github", "jira").
	LogicalName string
	// StoreName is the credential name in the encrypted store.
	StoreName string
	// Type is the credential type from the store (e.g., "bearer", "basic", "api_key", "password").
	// Empty string means the credential was resolved from a flat secret.
	Type string
	// Token holds the primary secret value (bearer token, API key value, or flat secret).
	Token string
	// Username holds the username for basic/password credentials.
	Username string
	// Password holds the password for basic/password credentials.
	Password string
}

// ResolveCredentials resolves all credential references in a fleet plan from the
// encrypted credential store. For each entry in plan.Credentials, it tries:
//  1. Named credential (store.Get) with type-specific field extraction
//  2. Flat secret (store.GetSecret) treated as a bearer-style token
//
// Returns a map keyed by logical name. Returns an error listing any credentials
// that could not be resolved.
func ResolveCredentials(plan *FleetPlan, store *credentials.Store) (map[string]*ResolvedCredential, error) {
	if len(plan.Credentials) == 0 {
		return nil, nil
	}

	if store == nil {
		return nil, fmt.Errorf("credential store is not available; cannot resolve fleet plan credentials")
	}

	resolved := make(map[string]*ResolvedCredential, len(plan.Credentials))
	var missing []string

	for logicalName, storeName := range plan.Credentials {
		rc := resolveOne(store, logicalName, storeName)
		if rc == nil {
			missing = append(missing, fmt.Sprintf("%s (store: %q)", logicalName, storeName))
			continue
		}
		resolved[logicalName] = rc
	}

	if len(missing) > 0 {
		return resolved, fmt.Errorf("credentials not found in store: %s", strings.Join(missing, ", "))
	}

	return resolved, nil
}

// resolveOne attempts to resolve a single credential from the store.
func resolveOne(store *credentials.Store, logicalName, storeName string) *ResolvedCredential {
	// Try named credential first
	cred := store.Get(storeName)
	if cred != nil {
		rc := &ResolvedCredential{
			LogicalName: logicalName,
			StoreName:   storeName,
			Type:        string(cred.Type),
		}

		switch cred.Type {
		case credentials.CredBearer:
			rc.Token = cred.Token
		case credentials.CredAPIKey:
			rc.Token = cred.Value
		case credentials.CredBasic:
			rc.Username = cred.Username
			rc.Password = cred.Password
			// Also set Token for convenience (some tools just need a single secret)
			rc.Token = cred.Password
		case credentials.CredPassword:
			rc.Username = cred.Username
			rc.Password = cred.Password
			rc.Token = cred.Password
		case credentials.CredOAuthClientCreds, credentials.CredOAuthAuthCode:
			rc.Token = cred.AccessToken
			if rc.Token == "" {
				rc.Token = cred.Token
			}
		}

		return rc
	}

	// Fall back to flat secret
	secret := store.GetSecret(storeName)
	if secret != "" {
		return &ResolvedCredential{
			LogicalName: logicalName,
			StoreName:   storeName,
			Type:        "", // flat secret has no typed structure
			Token:       secret,
		}
	}

	return nil
}

// GitHubToken returns the resolved token for the "github" logical credential,
// or empty string if not available. This is the primary facilitator for the
// GitHub ecosystem: the token is injected as GH_TOKEN into gh CLI commands.
func GitHubToken(resolved map[string]*ResolvedCredential) string {
	if resolved == nil {
		return ""
	}
	if rc, ok := resolved["github"]; ok && rc.Token != "" {
		return rc.Token
	}
	return ""
}
