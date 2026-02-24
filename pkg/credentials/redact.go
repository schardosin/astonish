package credentials

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

const (
	// minSignatureLen is the minimum length of a secret value to be tracked
	// for redaction. Shorter values would cause too many false positives.
	minSignatureLen = 8

	// redactedPrefix is the prefix used in redaction replacement text.
	redactedPrefix = "[REDACTED:"
	// redactedSuffix is the suffix used in redaction replacement text.
	redactedSuffix = "]"

	// storeKeyRedactName is the redaction label for the encryption key itself.
	storeKeyRedactName = "store-encryption-key"
)

// Redactor scans text for known credential values and replaces them with
// redaction markers. It tracks multiple encodings of each secret (raw,
// base64, URL-encoded) to catch common transformation attempts.
type Redactor struct {
	mu sync.RWMutex
	// signatures maps secret value variants to their credential name.
	// Multiple entries may map to the same name (raw, base64, url-encoded).
	signatures map[string]string
}

// NewRedactor creates an empty Redactor.
func NewRedactor() *Redactor {
	return &Redactor{
		signatures: make(map[string]string),
	}
}

// UpdateFromCredentials rebuilds the signature list from all stored credentials.
// Called after any credential is added, updated, or removed.
func (r *Redactor) UpdateFromCredentials(creds map[string]*Credential) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Preserve non-credential signatures (like the store key and OAuth tokens)
	preserved := make(map[string]string)
	for sig, name := range r.signatures {
		if strings.HasSuffix(name, "/token") || name == storeKeyRedactName {
			preserved[sig] = name
		}
	}

	r.signatures = preserved

	for name, cred := range creds {
		r.addSecretFieldsLocked(name, cred)
	}
}

// AddSecret adds a single secret value to the redaction list.
// Used for dynamic values like OAuth access tokens.
func (r *Redactor) AddSecret(name, value string) {
	if len(value) < minSignatureLen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addVariantsLocked(name, value)
}

// RemoveByName removes all signatures associated with a credential name.
func (r *Redactor) RemoveByName(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for sig, n := range r.signatures {
		if n == name || strings.HasPrefix(n, name+"/") {
			delete(r.signatures, sig)
		}
	}
}

// Redact scans text for any known credential values and replaces them
// with [REDACTED:credential-name] markers.
func (r *Redactor) Redact(text string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.signatures) == 0 {
		return text
	}

	for sig, name := range r.signatures {
		if strings.Contains(text, sig) {
			text = strings.ReplaceAll(text, sig, fmt.Sprintf("%s%s%s", redactedPrefix, name, redactedSuffix))
		}
	}
	return text
}

// RedactMap deep-walks a map and redacts all string values. Returns a new map
// (the original is not modified). Used for tool output redaction.
func (r *Redactor) RedactMap(m map[string]any) map[string]any {
	r.mu.RLock()
	if len(r.signatures) == 0 {
		r.mu.RUnlock()
		return m
	}
	r.mu.RUnlock()

	return r.redactMapRecursive(m)
}

func (r *Redactor) redactMapRecursive(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = r.redactValue(v)
	}
	return result
}

func (r *Redactor) redactValue(v any) any {
	switch val := v.(type) {
	case string:
		return r.Redact(val)
	case map[string]any:
		return r.redactMapRecursive(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = r.redactValue(item)
		}
		return result
	default:
		return v
	}
}

// addSecretFieldsLocked extracts all secret-bearing fields from a credential
// and adds their variants to the signature list. Must be called with mu held.
func (r *Redactor) addSecretFieldsLocked(name string, cred *Credential) {
	switch cred.Type {
	case CredAPIKey:
		r.addVariantsLocked(name, cred.Value)
	case CredBearer:
		r.addVariantsLocked(name, cred.Token)
	case CredBasic:
		r.addVariantsLocked(name, cred.Password)
		// Also track the base64-encoded "user:pass" since that's what goes on the wire
		basicAuth := cred.Username + ":" + cred.Password
		if len(basicAuth) >= minSignatureLen {
			b64 := base64.StdEncoding.EncodeToString([]byte(basicAuth))
			r.signatures[b64] = name
		}
	case CredPassword:
		r.addVariantsLocked(name, cred.Password)
	case CredOAuthClientCreds:
		r.addVariantsLocked(name, cred.ClientSecret)
		// Client ID is often semi-public but let's protect it too
		r.addVariantsLocked(name, cred.ClientID)
	case CredOAuthAuthCode:
		r.addVariantsLocked(name, cred.ClientSecret)
		r.addVariantsLocked(name, cred.ClientID)
		r.addVariantsLocked(name, cred.AccessToken)
		r.addVariantsLocked(name, cred.RefreshToken)
	}
}

// addVariantsLocked adds a secret value and its common encodings to the
// signature list. Must be called with mu held.
func (r *Redactor) addVariantsLocked(name, value string) {
	if len(value) < minSignatureLen {
		return
	}

	// Raw value
	r.signatures[value] = name

	// Base64-encoded (common in headers, JSON payloads)
	b64 := base64.StdEncoding.EncodeToString([]byte(value))
	if b64 != value && len(b64) >= minSignatureLen {
		r.signatures[b64] = name
	}

	// URL-encoded (common in query strings, form bodies)
	urlEnc := url.QueryEscape(value)
	if urlEnc != value && len(urlEnc) >= minSignatureLen {
		r.signatures[urlEnc] = name
	}
}

// SignatureCount returns the number of tracked signatures (for testing).
func (r *Redactor) SignatureCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.signatures)
}
