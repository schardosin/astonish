package api

import (
	"net/http"
	"time"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/filestore"
)

// effectiveUserID returns the user ID for the current request.
//
// In platform mode, this is the authenticated user's ID from the JWT token.
// In personal mode, this falls back to the hardcoded "studio_user" constant.
//
// All handlers that create or query user-scoped data (sessions, apps, etc.)
// should call this instead of using studioChatUserID directly.
func effectiveUserID(r *http.Request) string {
	if pu := GetPlatformUser(r); pu != nil {
		return pu.ID
	}
	return studioChatUserID
}

// effectiveCredentialStore returns the credential store for the current request.
//
// In platform mode, this returns the tenant-scoped store from request context.
// In personal mode, this wraps the singleton getAPICredentialStore() behind
// the store.CredentialStore interface.
//
// Returns nil if no credential store is available.
func effectiveCredentialStore(r *http.Request) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.Credentials != nil {
		return svc.Credentials
	}
	// Fall back to the personal-mode singleton.
	if cs := getAPICredentialStore(); cs != nil {
		return filestore.NewCredentialStore(cs)
	}
	return nil
}

// isPlatformMode checks whether the current request is running in platform mode
// by inspecting the Services context. Returns false for personal mode or when
// Services is not available.
// Used by handlers in tasks 4.3-4.8 for platform-mode branching.
func isPlatformMode(r *http.Request) bool {
	svc := store.FromRequest(r)
	return svc != nil && svc.Mode == store.ModePlatform
}

// DefaultUserID returns the default user ID used in personal mode and for
// system-initiated operations (scheduled fleet sessions, recovery, etc.).
// External packages that need the default user ID should call this instead of
// hardcoding the string.
func DefaultUserID() string {
	return studioChatUserID
}

// storeMetaToResponse converts a store.SessionMeta to the API response type.
func storeMetaToResponse(m store.SessionMeta) StudioSessionResponse {
	return StudioSessionResponse{
		ID:           m.ID,
		Title:        m.Title,
		CreatedAt:    m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
		MessageCount: m.MessageCount,
		FleetKey:     m.FleetKey,
		FleetName:    m.FleetName,
		IssueNumber:  m.IssueNumber,
		Repo:         m.Repo,
	}
}
