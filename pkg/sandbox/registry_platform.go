package sandbox

import (
	"context"

	"github.com/SAP/astonish/pkg/store"
)

// BuildPGSessionRegistry constructs a PG-backed SessionRegistry for the
// given org+team using the platform backend. Returns nil if platform is nil,
// slugs are empty, or the platform doesn't support sandbox sessions.
//
// The returned nil is safe to pass directly to BackendFromAppConfigWithSessions
// (which falls back to the local file-based registry when nil is received).
//
// This helper is exported so that packages outside pkg/api (e.g. pkg/launcher)
// can construct a PG-backed registry without depending on the HTTP request
// context or the unexported getPlatformBackend() singleton.
func BuildPGSessionRegistry(platform store.PlatformBackend, orgSlug, teamSlug string) *SessionRegistry {
	if platform == nil || orgSlug == "" || teamSlug == "" {
		return nil
	}
	provider, ok := platform.(store.SandboxSessionProvider)
	if !ok {
		return nil
	}
	sessStore := provider.SandboxSessionsForTeam(context.Background(), orgSlug, teamSlug)
	if sessStore == nil {
		return nil
	}
	return NewSessionRegistryFromStore(sessStore)
}
