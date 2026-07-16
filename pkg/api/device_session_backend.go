package api

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
)

// DeviceSessionBackendFromStore returns a DeviceSessionBackend backed by the
// platform store, or nil if the backend does not support DB-backed device sessions.
// When nil is returned, SSO falls back to the in-memory implementation.
func DeviceSessionBackendFromStore(backend store.PlatformBackend) DeviceSessionBackend {
	// Type-assert to get the entstore.Store which exposes PlatformClient().
	es, ok := backend.(*entstore.Store)
	if !ok {
		return nil
	}
	client := es.PlatformClient()
	if client == nil {
		return nil
	}
	dsStore := entstore.NewDeviceSessionStore(client)
	return &entDeviceSessionBackend{store: dsStore}
}

// entDeviceSessionBackend implements DeviceSessionBackend using the entstore
// DeviceSessionStore. Works for both PostgreSQL and SQLite backends.
type entDeviceSessionBackend struct {
	store *entstore.DeviceSessionStore
}

func (b *entDeviceSessionBackend) Create(ctx context.Context, sess *deviceSession) error {
	return b.store.Create(ctx, &entstore.DeviceSessionRow{
		DeviceCode: sess.DeviceCode,
		State:      sess.State,
		Nonce:      sess.Nonce,
		ProviderID: sess.ProviderID,
		ClientType: sess.ClientType,
		Status:     string(sess.Status),
		CreatedAt:  sess.CreatedAt,
		ExpiresAt:  sess.CreatedAt.Add(deviceSessionTTL),
	})
}

func (b *entDeviceSessionBackend) GetByCode(ctx context.Context, code string) *deviceSession {
	row, err := b.store.GetByCode(ctx, code)
	if err != nil || row == nil {
		return nil
	}
	return b.toDeviceSession(row)
}

func (b *entDeviceSessionBackend) GetByState(ctx context.Context, state string) *deviceSession {
	row, err := b.store.GetByState(ctx, state)
	if err != nil || row == nil {
		return nil
	}
	return b.toDeviceSession(row)
}

func (b *entDeviceSessionBackend) Complete(ctx context.Context, code string, sess *deviceSession) error {
	resultData := map[string]any{
		"access_token":    sess.AccessToken,
		"refresh_token":   sess.RefreshToken,
		"expires_in":      sess.ExpiresIn,
		"user":            sess.User,
		"org":             sess.Org,
		"team_slug":       sess.TeamSlug,
		"available_orgs":  sess.AvailableOrgs,
		"available_teams": sess.AvailableTeams,
	}
	return b.store.Complete(ctx, code, string(sess.Status), sess.ErrorMessage, resultData)
}

// toDeviceSession converts an entstore.DeviceSessionRow to a deviceSession.
func (b *entDeviceSessionBackend) toDeviceSession(row *entstore.DeviceSessionRow) *deviceSession {
	sess := &deviceSession{
		DeviceCode:   row.DeviceCode,
		State:        row.State,
		Nonce:        row.Nonce,
		ProviderID:   row.ProviderID,
		ClientType:   row.ClientType,
		Status:       deviceSessionStatus(row.Status),
		ErrorMessage: row.ErrorMessage,
		CreatedAt:    row.CreatedAt,
	}

	// Deserialize result_data if session is complete
	if row.ResultData != nil && sess.Status != deviceStatusPending {
		// ResultData is already a map[string]any — re-marshal/unmarshal for typed fields
		data, err := json.Marshal(row.ResultData)
		if err != nil {
			slog.Warn("failed to marshal device session result_data", "code", row.DeviceCode, "error", err)
			return sess
		}
		var result struct {
			AccessToken    string           `json:"access_token"`
			RefreshToken   string           `json:"refresh_token"`
			ExpiresIn      int              `json:"expires_in"`
			User           authUserResponse `json:"user"`
			Org            authOrgResponse  `json:"org"`
			TeamSlug       string           `json:"team_slug"`
			AvailableOrgs  []authOrgOption  `json:"available_orgs"`
			AvailableTeams []authTeamOption `json:"available_teams"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			slog.Warn("failed to unmarshal device session result_data", "code", row.DeviceCode, "error", err)
		} else {
			sess.AccessToken = result.AccessToken
			sess.RefreshToken = result.RefreshToken
			sess.ExpiresIn = result.ExpiresIn
			sess.User = result.User
			sess.Org = result.Org
			sess.TeamSlug = result.TeamSlug
			sess.AvailableOrgs = result.AvailableOrgs
			sess.AvailableTeams = result.AvailableTeams
		}
	}

	return sess
}

// Compile-time interface check
var _ DeviceSessionBackend = (*entDeviceSessionBackend)(nil)
