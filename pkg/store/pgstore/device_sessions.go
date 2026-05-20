package pgstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceSession represents a transient SSO device flow session stored in PG.
type DeviceSession struct {
	DeviceCode   string    `json:"device_code"`
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	ProviderID   string    `json:"provider_id"`
	ClientType   string    `json:"client_type"`
	Status       string    `json:"status"` // "pending", "complete", "failed"
	ErrorMessage string    `json:"error_message,omitempty"`
	ResultData   []byte    `json:"result_data,omitempty"` // JSONB
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// PGDeviceSessionStore manages device sessions in the platform database.
type PGDeviceSessionStore struct {
	pool *pgxpool.Pool
}

// NewPGDeviceSessionStore creates a new PG-backed device session store.
func NewPGDeviceSessionStore(pool *pgxpool.Pool) *PGDeviceSessionStore {
	return &PGDeviceSessionStore{pool: pool}
}

// Create stores a new device session.
func (s *PGDeviceSessionStore) Create(ctx context.Context, sess *DeviceSession) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO device_sessions (device_code, state, nonce, provider_id, client_type, status, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sess.DeviceCode, sess.State, sess.Nonce, sess.ProviderID, sess.ClientType,
		sess.Status, sess.CreatedAt, sess.ExpiresAt,
	)
	return err
}

// GetByCode retrieves a device session by its device code.
// Returns nil if not found or expired.
func (s *PGDeviceSessionStore) GetByCode(ctx context.Context, code string) (*DeviceSession, error) {
	sess := &DeviceSession{}
	var errorMsg *string
	var resultData []byte
	err := s.pool.QueryRow(ctx,
		`SELECT device_code, state, nonce, provider_id, client_type, status, error_message, result_data, created_at, expires_at
		 FROM device_sessions WHERE device_code = $1 AND expires_at > now()`, code,
	).Scan(&sess.DeviceCode, &sess.State, &sess.Nonce, &sess.ProviderID, &sess.ClientType,
		&sess.Status, &errorMsg, &resultData, &sess.CreatedAt, &sess.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if errorMsg != nil {
		sess.ErrorMessage = *errorMsg
	}
	sess.ResultData = resultData
	return sess, nil
}

// GetByState retrieves a device session by its OIDC state parameter.
// Returns nil if not found or expired.
func (s *PGDeviceSessionStore) GetByState(ctx context.Context, state string) (*DeviceSession, error) {
	sess := &DeviceSession{}
	var errorMsg *string
	var resultData []byte
	err := s.pool.QueryRow(ctx,
		`SELECT device_code, state, nonce, provider_id, client_type, status, error_message, result_data, created_at, expires_at
		 FROM device_sessions WHERE state = $1 AND expires_at > now()`, state,
	).Scan(&sess.DeviceCode, &sess.State, &sess.Nonce, &sess.ProviderID, &sess.ClientType,
		&sess.Status, &errorMsg, &resultData, &sess.CreatedAt, &sess.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if errorMsg != nil {
		sess.ErrorMessage = *errorMsg
	}
	sess.ResultData = resultData
	return sess, nil
}

// Complete marks a device session as complete with result data.
func (s *PGDeviceSessionStore) Complete(ctx context.Context, code string, status string, errorMsg string, resultData any) error {
	var resultJSON []byte
	if resultData != nil {
		var err error
		resultJSON, err = json.Marshal(resultData)
		if err != nil {
			return err
		}
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE device_sessions SET status = $1, error_message = $2, result_data = $3 WHERE device_code = $4`,
		status, errorMsg, resultJSON, code,
	)
	return err
}

// Cleanup removes expired device sessions.
func (s *PGDeviceSessionStore) Cleanup(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM device_sessions WHERE expires_at < now()`)
	return err
}
