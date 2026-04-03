package credentials

import (
	"fmt"
	"regexp"
)

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
func SubstitutePlaceholders(text string, store *Store) string {
	if store == nil {
		return text
	}

	return credentialPlaceholderRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := credentialPlaceholderRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		name, field := parts[1], parts[2]
		return resolveField(store, name, field, match)
	})
}

// SubstituteMap deep-walks a map and substitutes credential placeholders in
// all string values. Returns a new map (the original is not modified).
func SubstituteMap(m map[string]any, store *Store) map[string]any {
	if store == nil || m == nil {
		return m
	}
	if !mapContainsPlaceholder(m) {
		return m
	}
	return substituteMapRecursive(m, store)
}

// SubstituteMapInPlace modifies the map's string values in-place, replacing
// credential placeholders with real values.
//
// WARNING: If the args map is shared by reference (e.g. with ADK session
// events), the mutation will corrupt the stored event. Prefer
// SubstituteAndRestore which restores the original placeholders after use.
func SubstituteMapInPlace(m map[string]any, store *Store) {
	if store == nil || m == nil {
		return
	}
	if !mapContainsPlaceholder(m) {
		return
	}
	for k, v := range m {
		m[k] = substituteValue(v, store)
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
// If there are no placeholders or the store is nil, returns a no-op function.
func SubstituteAndRestore(m map[string]any, store *Store) (restore func()) {
	noop := func() {}

	if store == nil || m == nil {
		return noop
	}
	if !mapContainsPlaceholder(m) {
		return noop
	}

	// Snapshot original values for keys that contain placeholders.
	originals := snapshotPlaceholderValues(m)

	// Substitute in-place so the tool gets real values.
	for k, v := range m {
		m[k] = substituteValue(v, store)
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

func substituteMapRecursive(m map[string]any, store *Store) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = substituteValue(v, store)
	}
	return result
}

func substituteValue(v any, store *Store) any {
	switch val := v.(type) {
	case string:
		if ContainsPlaceholder(val) {
			return SubstitutePlaceholders(val, store)
		}
		return val
	case map[string]any:
		return substituteMapRecursive(val, store)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = substituteValue(item, store)
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
func resolveField(store *Store, name, field, fallback string) string {
	if store == nil {
		return fallback
	}

	// Reload to pick up any changes from CLI
	store.Reload()

	cred := store.Get(name)
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
			_, headerValue, err := store.Resolve(name)
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
