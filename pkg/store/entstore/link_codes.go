package entstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/pendinglinkcode"
	"github.com/schardosin/astonish/pkg/store"
)

const defaultLinkCodeTTL = 10 * time.Minute

// linkCodeStore implements store.LinkCodeStore using the Ent platform client.
type linkCodeStore struct {
	client *platforment.Client
}

func (s *Store) LinkCodes() store.LinkCodeStore {
	return &linkCodeStore{client: s.platformClient}
}

func (lc *linkCodeStore) Generate(ctx context.Context, code, userID, email, channel string) error {
	return lc.GenerateWithTTL(ctx, code, userID, email, channel, defaultLinkCodeTTL)
}

func (lc *linkCodeStore) GenerateWithTTL(ctx context.Context, code, userID, email, channel string, ttl time.Duration) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	now := time.Now()
	create := lc.client.PendingLinkCode.Create().
		SetID(code).
		SetUserID(uid).
		SetChannel(channel).
		SetCreatedAt(now).
		SetExpiresAt(now.Add(ttl))

	if email != "" {
		create.SetEmail(email)
	}

	_, err = create.Save(ctx)
	return err
}

func (lc *linkCodeStore) Consume(ctx context.Context, code string) (*store.LinkCode, error) {
	ent, err := lc.client.PendingLinkCode.Query().
		Where(
			pendinglinkcode.IDEQ(code),
			pendinglinkcode.ExpiresAtGT(time.Now()),
		).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, fmt.Errorf("link code not found or expired")
		}
		return nil, err
	}

	// Delete the consumed code.
	if err := lc.client.PendingLinkCode.DeleteOneID(code).Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to delete consumed link code: %w", err)
	}

	return entLinkCodeToStore(ent), nil
}

func (lc *linkCodeStore) Cleanup(ctx context.Context) error {
	_, err := lc.client.PendingLinkCode.Delete().
		Where(pendinglinkcode.ExpiresAtLT(time.Now())).
		Exec(ctx)
	return err
}

func entLinkCodeToStore(e *platforment.PendingLinkCode) *store.LinkCode {
	lc := &store.LinkCode{
		Code:      e.ID,
		UserID:    e.UserID.String(),
		Channel:   e.Channel,
		CreatedAt: e.CreatedAt,
		ExpiresAt: e.ExpiresAt,
	}
	if e.Email != nil {
		lc.Email = *e.Email
	}
	return lc
}

// Compile-time assertion.
var _ store.LinkCodeStore = (*linkCodeStore)(nil)
