package credentials

import (
	"fmt"
	"regexp"
	"sync"
)

// User-facing secret tag syntax: <<<secret_value>>>
// Users wrap secrets in triple angle brackets when providing them in chat.
// The system extracts the raw value before the LLM sees the message.
var userSecretTagRe = regexp.MustCompile(`<<<(.+?)>>>`)

// Internal token format after extraction: <<<SECRET_N>>>
// These are what the LLM sees and passes through to tool args.
var pendingSecretTokenRe = regexp.MustCompile(`<<<SECRET_(\d+)>>>`)

// PendingVault stores secrets extracted from user messages before they reach
// the LLM. Each session should have its own vault.
//
// Flow:
//  1. User types: "password <<<hunter2>>>"
//  2. Extract() strips the raw value, stores it, returns "password <<<SECRET_1>>>"
//  3. LLM sees <<<SECRET_1>>> and passes it to tool args (e.g., save_credential)
//  4. BeforeToolCallback calls SubstituteString() to resolve <<<SECRET_1>>> → "hunter2"
//  5. Tool executes with the real value
//  6. AfterToolCallback restores <<<SECRET_1>>> in the args map
type PendingVault struct {
	mu       sync.Mutex
	secrets  map[string]string // "<<<SECRET_1>>>" → "hunter2"
	counter  int
	redactor *Redactor // optional: immediately register raw values as known secrets
}

// NewPendingVault creates a new per-session secret vault. If a Redactor is
// provided, extracted secrets are immediately registered so they are caught
// by all redaction pipelines even if the credential is never formally saved.
func NewPendingVault(redactor *Redactor) *PendingVault {
	return &PendingVault{
		secrets:  make(map[string]string),
		redactor: redactor,
	}
}

// Extract scans text for <<<value>>> tags, extracts the raw secret values,
// stores them in the vault, and replaces them with <<<SECRET_N>>> tokens.
// Returns the sanitized text. If no tags are found, returns text unchanged.
//
// The raw values are immediately registered with the Redactor (if present)
// as a safety net — even if they somehow leak, the redactor will catch them.
func (v *PendingVault) Extract(text string) string {
	if !userSecretTagRe.MatchString(text) {
		return text
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	return userSecretTagRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := userSecretTagRe.FindStringSubmatch(match)
		if len(parts) != 2 || parts[1] == "" {
			return match // malformed or empty — leave as-is
		}

		rawValue := parts[1]

		// Check if this exact value was already extracted (dedup within message)
		for token, stored := range v.secrets {
			if stored == rawValue {
				return token
			}
		}

		v.counter++
		token := fmt.Sprintf("<<<SECRET_%d>>>", v.counter)
		v.secrets[token] = rawValue

		// Register with redactor immediately as safety net
		if v.redactor != nil {
			v.redactor.AddTransientSecret(rawValue)
		}

		return token
	})
}

// Resolve returns the raw secret value for a <<<SECRET_N>>> token.
// Returns the value and true if found, or ("", false) if not.
func (v *PendingVault) Resolve(token string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	val, ok := v.secrets[token]
	return val, ok
}

// ContainsPendingSecret reports whether text contains any <<<SECRET_N>>> token.
func ContainsPendingSecret(text string) bool {
	return pendingSecretTokenRe.MatchString(text)
}

// SubstituteString replaces all <<<SECRET_N>>> tokens in text with real values
// from the vault. Unresolvable tokens are left as-is.
func (v *PendingVault) SubstituteString(text string) string {
	if !pendingSecretTokenRe.MatchString(text) {
		return text
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	return pendingSecretTokenRe.ReplaceAllStringFunc(text, func(match string) string {
		if val, ok := v.secrets[match]; ok {
			return val
		}
		return match
	})
}

// SubstituteMap deep-walks a map and substitutes <<<SECRET_N>>> tokens in
// all string values. Modifies the map in-place. Returns a restore function
// that puts the original tokens back (same pattern as credential placeholders).
func (v *PendingVault) SubstituteAndRestore(m map[string]any) func() {
	noop := func() {}
	if v == nil || m == nil {
		return noop
	}
	if !mapContainsPendingSecret(m) {
		return noop
	}

	// Snapshot originals
	originals := make(map[string]any)
	for k, val := range m {
		if containsPendingSecretValue(val) {
			originals[k] = val
		}
	}

	// Substitute in-place
	for k, val := range m {
		m[k] = v.substituteValue(val)
	}

	return func() {
		for k, val := range originals {
			m[k] = val
		}
	}
}

func (v *PendingVault) substituteValue(val any) any {
	switch vv := val.(type) {
	case string:
		if ContainsPendingSecret(vv) {
			return v.SubstituteString(vv)
		}
		return vv
	case map[string]any:
		result := make(map[string]any, len(vv))
		for k, item := range vv {
			result[k] = v.substituteValue(item)
		}
		return result
	case []any:
		result := make([]any, len(vv))
		for i, item := range vv {
			result[i] = v.substituteValue(item)
		}
		return result
	default:
		return val
	}
}

func mapContainsPendingSecret(m map[string]any) bool {
	for _, v := range m {
		if containsPendingSecretValue(v) {
			return true
		}
	}
	return false
}

func containsPendingSecretValue(v any) bool {
	switch val := v.(type) {
	case string:
		return ContainsPendingSecret(val)
	case map[string]any:
		return mapContainsPendingSecret(val)
	case []any:
		for _, item := range val {
			if containsPendingSecretValue(item) {
				return true
			}
		}
	}
	return false
}
