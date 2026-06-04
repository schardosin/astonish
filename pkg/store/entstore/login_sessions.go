package entstore

import (
	"context"
	"time"

	"github.com/google/uuid"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/loginsession"
	"github.com/schardosin/astonish/pkg/store"
)

// loginSessionStore implements store.LoginSessionStore using the Ent platform client.
type loginSessionStore struct {
	client *platforment.Client
}

func (s *Store) LoginSessions() store.LoginSessionStore {
	return &loginSessionStore{client: s.platformClient}
}

func (ls *loginSessionStore) Create(ctx context.Context, session *store.LoginSession) error {
	uid, _ := uuid.Parse(session.UserID)
	oid, _ := uuid.Parse(session.OrgID)

	_, err := ls.client.LoginSession.Create().
		SetID(session.TokenHash).
		SetUserID(uid).
		SetOrgID(oid).
		SetCreatedAt(session.CreatedAt).
		SetExpiresAt(session.ExpiresAt).
		SetNillableUserAgent(nilStrPtr(session.UserAgent)).
		SetNillableIPAddress(nilStrPtr(session.IPAddress)).
		Save(ctx)
	return err
}

func (ls *loginSessionStore) Validate(ctx context.Context, tokenHash string) (*store.LoginSession, error) {
	ent, err := ls.client.LoginSession.Query().
		Where(
			loginsession.IDEQ(tokenHash),
			loginsession.ExpiresAtGT(time.Now()),
		).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entLoginSessionToStore(ent), nil
}

func (ls *loginSessionStore) Delete(ctx context.Context, tokenHash string) error {
	return ls.client.LoginSession.DeleteOneID(tokenHash).Exec(ctx)
}

func (ls *loginSessionStore) DeleteExpired(ctx context.Context) error {
	_, err := ls.client.LoginSession.Delete().
		Where(loginsession.ExpiresAtLT(time.Now())).
		Exec(ctx)
	return err
}

func entLoginSessionToStore(e *platforment.LoginSession) *store.LoginSession {
	s := &store.LoginSession{
		TokenHash: e.ID,
		UserID:    e.UserID.String(),
		OrgID:     e.OrgID.String(),
		CreatedAt: e.CreatedAt,
		ExpiresAt: e.ExpiresAt,
	}
	if e.UserAgent != nil {
		s.UserAgent = *e.UserAgent
	}
	if e.IPAddress != nil {
		s.IPAddress = *e.IPAddress
	}
	return s
}

// Compile-time assertion.
var _ store.LoginSessionStore = (*loginSessionStore)(nil)
