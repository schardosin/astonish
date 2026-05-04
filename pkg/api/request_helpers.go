package api

import (
	"net/http"
	"time"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/filestore"
	"github.com/schardosin/astonish/pkg/store/pgstore"
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

// effectiveCredentialStore returns the credential store for the current request,
// optionally scoped by the "scope" query parameter.
//
// Scope values:
//   - "personal": returns the user's personal credential store
//   - "team": returns the team-scoped credential store
//   - "" (empty/omitted): returns a merged store (personal-first, team-fallback)
//
// In personal mode (no platform), always returns the file-based singleton.
func effectiveCredentialStore(r *http.Request) store.CredentialStore {
	scope := r.URL.Query().Get("scope")
	return effectiveCredentialStoreScoped(r, scope)
}

// effectiveCredentialStoreScoped returns the credential store for the given scope.
func effectiveCredentialStoreScoped(r *http.Request, scope string) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.Mode == store.ModePlatform {
		switch scope {
		case "personal":
			return svc.PersonalCredentials
		case "team":
			return svc.Credentials
		default:
			// Merged: personal-first, team-fallback
			if svc.PersonalCredentials != nil || svc.Credentials != nil {
				return store.NewMergedCredentialStore(svc.PersonalCredentials, svc.Credentials)
			}
			return svc.Credentials
		}
	}
	// Fall back to the personal-mode singleton.
	if cs := getAPICredentialStore(); cs != nil {
		return filestore.NewCredentialStore(cs)
	}
	return nil
}

// effectivePersonalCredentialStore returns just the personal credential store.
func effectivePersonalCredentialStore(r *http.Request) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.PersonalCredentials != nil {
		return svc.PersonalCredentials
	}
	// In personal mode, the single store IS the personal store.
	if cs := getAPICredentialStore(); cs != nil {
		return filestore.NewCredentialStore(cs)
	}
	return nil
}

// effectiveTeamCredentialStore returns just the team credential store.
func effectiveTeamCredentialStore(r *http.Request) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.Credentials != nil {
		return svc.Credentials
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

// effectiveTeamSlug returns the team slug for the current request.
// In platform mode, this is read from the TenantContext (set by auth middleware).
// In personal mode, this returns an empty string.
func effectiveTeamSlug(r *http.Request) string {
	if tc := pgstore.TenantContextFrom(r.Context()); tc != nil {
		return tc.TeamSlug
	}
	return ""
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
