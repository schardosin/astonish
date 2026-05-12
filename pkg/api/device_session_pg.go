package api

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// pgDeviceSessionBackend implements DeviceSessionBackend using PostgreSQL.
// Enables stateless horizontal scaling by storing SSO device flow state in PG
// instead of process memory.
type pgDeviceSessionBackend struct {
	store *pgstore.PGDeviceSessionStore
}

// NewPGDeviceSessionBackend creates a PG-backed device session backend.
func NewPGDeviceSessionBackend(store *pgstore.PGDeviceSessionStore) DeviceSessionBackend {
	return &pgDeviceSessionBackend{store: store}
}

func (b *pgDeviceSessionBackend) Create(ctx context.Context, sess *deviceSession) error {
	return b.store.Create(ctx, &pgstore.DeviceSession{
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

func (b *pgDeviceSessionBackend) GetByCode(ctx context.Context, code string) *deviceSession {
	row, err := b.store.GetByCode(ctx, code)
	if err != nil || row == nil {
		return nil
	}
	return b.toDeviceSession(row)
}

func (b *pgDeviceSessionBackend) GetByState(ctx context.Context, state string) *deviceSession {
	row, err := b.store.GetByState(ctx, state)
	if err != nil || row == nil {
		return nil
	}
	return b.toDeviceSession(row)
}

func (b *pgDeviceSessionBackend) Complete(ctx context.Context, code string, sess *deviceSession) error {
	// Serialize the full session data as result_data JSONB
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

// toDeviceSession converts a PG row to a deviceSession.
func (b *pgDeviceSessionBackend) toDeviceSession(row *pgstore.DeviceSession) *deviceSession {
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
	if len(row.ResultData) > 0 && sess.Status != deviceStatusPending {
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
		if err := json.Unmarshal(row.ResultData, &result); err != nil {
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
var _ DeviceSessionBackend = (*pgDeviceSessionBackend)(nil)
