package credentials

import (
	"fmt"
	"regexp"
)

// CredentialResolver is the interface used by the substitution engine to
// look up credential values. Both the file-based *credentials.Store and
// the PG-backed store.CredentialStore satisfy this (the latter via an adapter
// in the agent package that converts store.Credential → credentials.Credential).
//
// This abstraction keeps the substitution logic decoupled from any specific
// storage backend, enabling multi-tenant credential isolation in platform mode.
type CredentialResolver interface {
	// Get retrieves a credential by name. Returns nil if not found.
	Get(name string) *Credential
	// Resolve returns the HTTP header key and value for a credential.
	// Used for OAuth auth-code token refresh.
	Resolve(name string) (headerKey, headerValue string, err error)
	// Reload re-reads credentials from the backing store.
	Reload() error
}

// Placeholder format: {{CREDENTIAL:credential-name:field}}
// Fields: password, token, value, client_secret, access_token, refresh_token
var credentialPlaceholderRe = regexp.MustCompile(`\{\{CREDENTIAL:([^:}]+):([^}]+)\}\}`)

// FormatPlaceholder returns a credential placeholder token.
// The LLM sees these instead of raw secret values.
func FormatPlaceholder(credName, field string) string {
	return fmt.Sprintf("{{CREDENTIAL:%s:%s}}", credName, field)
}

// ContainsPlaceholder reports whether text contains any credential placeholder.
func ContainsPlaceholder(text string) bool {
	return credentialPlaceholderRe.MatchString(text)
}

// SubstitutePlaceholders replaces all {{CREDENTIAL:name:field}} tokens in text
// with the actual secret values from the credential store. Unresolvable
// placeholders are left as-is (the tool will fail naturally, giving the LLM
// a chance to correct).
//
// This runs inside a BeforeToolCallback — after the LLM has chosen the tool
// and args, but before the tool executes. The real values never enter the
// conversation history.
func SubstitutePlaceholders(text string, resolver CredentialResolver) string {
	if resolver == nil {
		return text
	}

	return credentialPlaceholderRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := credentialPlaceholderRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		name, field := parts[1], parts[2]
		return resolveField(resolver, name, field, match)
	})
}

// SubstituteMap deep-walks a map and substitutes credential placeholders in
// all string values. Returns a new map (the original is not modified).
func SubstituteMap(m map[string]any, resolver CredentialResolver) map[string]any {
	if resolver == nil || m == nil {
		return m
	}
	if !mapContainsPlaceholder(m) {
		return m
	}
	return substituteMapRecursive(m, resolver)
}

// SubstituteMapInPlace modifies the map's string values in-place, replacing
// credential placeholders with real values.
//
// WARNING: If the args map is shared by reference (e.g. with ADK session
// events), the mutation will corrupt the stored event. Prefer
// SubstituteAndRestore which restores the original placeholders after use.
func SubstituteMapInPlace(m map[string]any, resolver CredentialResolver) {
	if resolver == nil || m == nil {
		return
	}
	if !mapContainsPlaceholder(m) {
		return
	}
	for k, v := range m {
		m[k] = substituteValue(v, resolver)
	}
}

// SubstituteAndRestore substitutes credential placeholders in the args map
// in-place (so the tool receives real values) and returns a restore function
// that puts the original placeholder values back.
//
// This is designed for ADK's BeforeToolCallback / AfterToolCallback pair:
//
//   - BeforeToolCallback calls SubstituteAndRestore → tool runs with real values
//   - AfterToolCallback calls the returned restore function → the shared args
//     map reverts to placeholders, so the session event (which holds the same
//     map by reference) never persists real secrets.
//
// If there are no placeholders or the resolver is nil, returns a no-op function.
func SubstituteAndRestore(m map[string]any, resolver CredentialResolver) (restore func()) {
	noop := func() {}

	if resolver == nil || m == nil {
		return noop
	}
	if !mapContainsPlaceholder(m) {
		return noop
	}

	// Snapshot original values for keys that contain placeholders.
	originals := snapshotPlaceholderValues(m)

	// Substitute in-place so the tool gets real values.
	for k, v := range m {
		m[k] = substituteValue(v, resolver)
	}

	// Return a function that restores the originals.
	return func() {
		for k, v := range originals {
			m[k] = v
		}
	}
}

// snapshotPlaceholderValues returns a shallow copy of map entries whose values
// contain credential placeholders. Only top-level keys are tracked — this is
// sufficient because ADK tool args are flat JSON objects (nested structures
// are rare and the top-level value is what gets restored).
func snapshotPlaceholderValues(m map[string]any) map[string]any {
	snap := make(map[string]any)
	for k, v := range m {
		if containsPlaceholderValue(v) {
			snap[k] = v
		}
	}
	return snap
}

func substituteMapRecursive(m map[string]any, resolver CredentialResolver) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = substituteValue(v, resolver)
	}
	return result
}

func substituteValue(v any, resolver CredentialResolver) any {
	switch val := v.(type) {
	case string:
		if ContainsPlaceholder(val) {
			return SubstitutePlaceholders(val, resolver)
		}
		return val
	case map[string]any:
		return substituteMapRecursive(val, resolver)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = substituteValue(item, resolver)
		}
		return result
	default:
		return v
	}
}

// mapContainsPlaceholder quickly checks whether any string in the map contains
// a placeholder, to avoid unnecessary deep copies.
func mapContainsPlaceholder(m map[string]any) bool {
	for _, v := range m {
		if containsPlaceholderValue(v) {
			return true
		}
	}
	return false
}

func containsPlaceholderValue(v any) bool {
	switch val := v.(type) {
	case string:
		return ContainsPlaceholder(val)
	case map[string]any:
		return mapContainsPlaceholder(val)
	case []any:
		for _, item := range val {
			if containsPlaceholderValue(item) {
				return true
			}
		}
	}
	return false
}

// resolveField looks up a single credential field value.
// Returns the fallback string if the credential or field is not found.
func resolveField(resolver CredentialResolver, name, field, fallback string) string {
	if resolver == nil {
		return fallback
	}

	// Reload to pick up any changes from CLI or other processes
	resolver.Reload()

	cred := resolver.Get(name)
	if cred == nil {
		return fallback
	}

	switch field {
	case "password":
		if cred.Password != "" {
			return cred.Password
		}
	case "token":
		// For OAuth auth-code, get a fresh (possibly refreshed) token
		if cred.Type == CredOAuthAuthCode {
			_, headerValue, err := resolver.Resolve(name)
			if err == nil {
				// Extract token from "Bearer <token>"
				const prefix = "Bearer "
				if len(headerValue) > len(prefix) {
					return headerValue[len(prefix):]
				}
			}
		}
		if cred.Token != "" {
			return cred.Token
		}
	case "value":
		if cred.Value != "" {
			return cred.Value
		}
	case "username":
		if cred.Username != "" {
			return cred.Username
		}
	case "header":
		if cred.Header != "" {
			return cred.Header
		}
	case "client_secret":
		if cred.ClientSecret != "" {
			return cred.ClientSecret
		}
	case "client_id":
		if cred.ClientID != "" {
			return cred.ClientID
		}
	case "access_token":
		if cred.AccessToken != "" {
			return cred.AccessToken
		}
	case "refresh_token":
		if cred.RefreshToken != "" {
			return cred.RefreshToken
		}
	}

	return fallback
}
