package browser

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// HandoffToken represents an active VNC handoff session. The token is
// generated when the agent starts a human handoff and included in the
// iframe URL. The auth middleware validates it before allowing access
// to the VNC proxy, so that only legitimate handoff sessions can reach
// KasmVNC — not anyone who guesses a container name.
type HandoffToken struct {
	Token     string
	Container string
	CreatedAt time.Time
}

// HandoffTokenRegistry manages short-lived tokens that authorize access
// to the VNC proxy for a specific container. Tokens are created when
// browser_request_human starts KasmVNC and removed when the handoff
// completes or the container is destroyed.
//
// The registry is in-memory (lost on daemon restart), which is fine
// because handoff sessions are ephemeral — a restart kills the VNC
// session anyway and the agent retries.
type HandoffTokenRegistry struct {
	mu sync.RWMutex
	// byContainer maps container name → token string (one active handoff per container)
	byContainer map[string]*HandoffToken
	// byToken maps token string → HandoffToken (for fast lookup during auth)
	byToken map[string]*HandoffToken
}

var (
	globalTokenRegistry     *HandoffTokenRegistry
	globalTokenRegistryOnce sync.Once
)

// GetHandoffTokenRegistry returns the singleton token registry.
func GetHandoffTokenRegistry() *HandoffTokenRegistry {
	globalTokenRegistryOnce.Do(func() {
		globalTokenRegistry = &HandoffTokenRegistry{
			byContainer: make(map[string]*HandoffToken),
			byToken:     make(map[string]*HandoffToken),
		}
	})
	return globalTokenRegistry
}

// Register creates a new handoff token for the given container,
// replacing any existing token for that container. Returns the
// token string to include in the iframe URL.
func (r *HandoffTokenRegistry) Register(container string) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove previous token for this container if any
	if old, ok := r.byContainer[container]; ok {
		delete(r.byToken, old.Token)
	}

	ht := &HandoffToken{
		Token:     token,
		Container: container,
		CreatedAt: time.Now(),
	}
	r.byContainer[container] = ht
	r.byToken[token] = ht

	return token, nil
}

// Revoke removes the token for a container. Called when the handoff
// completes or the container is destroyed.
func (r *HandoffTokenRegistry) Revoke(container string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ht, ok := r.byContainer[container]; ok {
		delete(r.byToken, ht.Token)
		delete(r.byContainer, container)
	}
}

// ValidateContainer checks if the given container has an active handoff
// token. Used by the auth middleware for sub-resource requests that
// don't carry the token in the URL (CSS, JS, images loaded by the
// KasmVNC page).
func (r *HandoffTokenRegistry) ValidateContainer(container string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ht, ok := r.byContainer[container]
	if !ok {
		return false
	}

	// Tokens expire after 30 minutes as a safety net. Normal cleanup
	// happens via Revoke when the handoff ends.
	if time.Since(ht.CreatedAt) > 30*time.Minute {
		return false
	}

	return true
}

// ValidateToken checks if the given token string is valid and returns
// the associated container name. Used for the initial page load where
// the token is in the query string.
func (r *HandoffTokenRegistry) ValidateToken(token string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ht, ok := r.byToken[token]
	if !ok {
		return "", false
	}

	if time.Since(ht.CreatedAt) > 30*time.Minute {
		return "", false
	}

	return ht.Container, true
}
