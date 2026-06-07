package entstore

import (
	"context"
	"time"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/devicesession"
)

// DeviceSessionStore provides CRUD operations for device sessions using the
// platform Ent client. This replaces pgstore.PGDeviceSessionStore for both
// PostgreSQL and SQLite backends.
type DeviceSessionStore struct {
	client *platforment.Client
}

// NewDeviceSessionStore creates a new device session store backed by the
// platform Ent client.
func NewDeviceSessionStore(client *platforment.Client) *DeviceSessionStore {
	return &DeviceSessionStore{client: client}
}

// DeviceSessionRow is the data transfer object for device sessions.
type DeviceSessionRow struct {
	DeviceCode   string
	State        string
	Nonce        string
	ProviderID   string
	ClientType   string
	Status       string
	ErrorMessage string
	ResultData   map[string]any
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// Create stores a new device session.
func (s *DeviceSessionStore) Create(ctx context.Context, sess *DeviceSessionRow) error {
	_, err := s.client.DeviceSession.Create().
		SetID(sess.DeviceCode).
		SetState(sess.State).
		SetNonce(sess.Nonce).
		SetProviderID(sess.ProviderID).
		SetClientType(sess.ClientType).
		SetStatus(devicesession.StatusPending).
		SetCreatedAt(sess.CreatedAt).
		SetExpiresAt(sess.ExpiresAt).
		Save(ctx)
	return err
}

// GetByCode retrieves a device session by its device code (ID).
// Returns nil if not found or expired.
func (s *DeviceSessionStore) GetByCode(ctx context.Context, code string) (*DeviceSessionRow, error) {
	ent, err := s.client.DeviceSession.Query().
		Where(
			devicesession.ID(code),
			devicesession.ExpiresAtGT(time.Now()),
		).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToRow(ent), nil
}

// GetByState retrieves a device session by its OIDC state parameter.
// Returns nil if not found or expired.
func (s *DeviceSessionStore) GetByState(ctx context.Context, state string) (*DeviceSessionRow, error) {
	ent, err := s.client.DeviceSession.Query().
		Where(
			devicesession.StateEQ(state),
			devicesession.ExpiresAtGT(time.Now()),
		).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToRow(ent), nil
}

// Complete marks a device session as complete with result data.
func (s *DeviceSessionStore) Complete(ctx context.Context, code string, status string, errorMsg string, resultData map[string]any) error {
	update := s.client.DeviceSession.UpdateOneID(code).
		SetStatus(devicesession.Status(status))

	if errorMsg != "" {
		update = update.SetErrorMessage(errorMsg)
	}
	if resultData != nil {
		update = update.SetResultData(resultData)
	}

	_, err := update.Save(ctx)
	return err
}

// Cleanup removes expired device sessions.
func (s *DeviceSessionStore) Cleanup(ctx context.Context) error {
	_, err := s.client.DeviceSession.Delete().
		Where(devicesession.ExpiresAtLT(time.Now())).
		Exec(ctx)
	return err
}

// entToRow converts an Ent DeviceSession entity to a DeviceSessionRow.
func entToRow(e *platforment.DeviceSession) *DeviceSessionRow {
	row := &DeviceSessionRow{
		DeviceCode: e.ID,
		State:      e.State,
		Nonce:      e.Nonce,
		ProviderID: e.ProviderID,
		ClientType: e.ClientType,
		Status:     string(e.Status),
		ResultData: e.ResultData,
		CreatedAt:  e.CreatedAt,
		ExpiresAt:  e.ExpiresAt,
	}
	if e.ErrorMessage != nil {
		row.ErrorMessage = *e.ErrorMessage
	}
	return row
}
