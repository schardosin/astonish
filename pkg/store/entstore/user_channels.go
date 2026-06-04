package entstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	platforment "github.com/schardosin/astonish/ent/platform"
	"github.com/schardosin/astonish/ent/platform/userchannel"
	"github.com/schardosin/astonish/pkg/store"
)

// userChannelStore implements store.UserChannelStore using the Ent platform client.
type userChannelStore struct {
	client *platforment.Client
}

func (s *Store) UserChannels() store.UserChannelStore {
	return &userChannelStore{client: s.platformClient}
}

func (cs *userChannelStore) Link(ctx context.Context, ch *store.UserChannel) error {
	uid, err := uuid.Parse(ch.UserID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	create := cs.client.UserChannel.Create().
		SetUserID(uid).
		SetChannelType(ch.ChannelType).
		SetExternalID(ch.ExternalID).
		SetDisplayName(ch.DisplayName).
		SetEnabled(ch.Enabled).
		SetVerified(ch.Verified)

	if ch.ID != "" {
		cid, err := uuid.Parse(ch.ID)
		if err != nil {
			return fmt.Errorf("invalid channel ID: %w", err)
		}
		create.SetID(cid)
	}
	if ch.VerifiedAt != nil {
		create.SetVerifiedAt(*ch.VerifiedAt)
	}
	if !ch.CreatedAt.IsZero() {
		create.SetCreatedAt(ch.CreatedAt)
	}

	saved, err := create.Save(ctx)
	if err != nil {
		return err
	}
	ch.ID = saved.ID.String()
	return nil
}

func (cs *userChannelStore) Unlink(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid channel ID: %w", err)
	}
	return cs.client.UserChannel.DeleteOneID(uid).Exec(ctx)
}

func (cs *userChannelStore) GetByID(ctx context.Context, id string) (*store.UserChannel, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid channel ID: %w", err)
	}
	ent, err := cs.client.UserChannel.Get(ctx, uid)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entUserChannelToStore(ent), nil
}

func (cs *userChannelStore) GetByExternalID(ctx context.Context, channelType, externalID string) (*store.UserChannel, error) {
	ent, err := cs.client.UserChannel.Query().
		Where(
			userchannel.ChannelTypeEQ(channelType),
			userchannel.ExternalIDEQ(externalID),
		).
		Only(ctx)
	if err != nil {
		if platforment.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return entUserChannelToStore(ent), nil
}

func (cs *userChannelStore) ListByUser(ctx context.Context, userID string) ([]*store.UserChannel, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	ents, err := cs.client.UserChannel.Query().
		Where(userchannel.UserIDEQ(uid)).
		Order(userchannel.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]*store.UserChannel, len(ents))
	for i, e := range ents {
		channels[i] = entUserChannelToStore(e)
	}
	return channels, nil
}

func (cs *userChannelStore) ListByChannelType(ctx context.Context, channelType string) ([]*store.UserChannel, error) {
	ents, err := cs.client.UserChannel.Query().
		Where(
			userchannel.ChannelTypeEQ(channelType),
			userchannel.VerifiedEQ(true),
			userchannel.EnabledEQ(true),
		).
		Order(userchannel.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]*store.UserChannel, len(ents))
	for i, e := range ents {
		channels[i] = entUserChannelToStore(e)
	}
	return channels, nil
}

func (cs *userChannelStore) ListByUsers(ctx context.Context, userIDs []string, channelType string) ([]*store.UserChannel, error) {
	uuids := make([]uuid.UUID, 0, len(userIDs))
	for _, id := range userIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID %q: %w", id, err)
		}
		uuids = append(uuids, uid)
	}

	ents, err := cs.client.UserChannel.Query().
		Where(
			userchannel.UserIDIn(uuids...),
			userchannel.ChannelTypeEQ(channelType),
		).
		Order(userchannel.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]*store.UserChannel, len(ents))
	for i, e := range ents {
		channels[i] = entUserChannelToStore(e)
	}
	return channels, nil
}

func (cs *userChannelStore) Update(ctx context.Context, ch *store.UserChannel) error {
	uid, err := uuid.Parse(ch.ID)
	if err != nil {
		return fmt.Errorf("invalid channel ID: %w", err)
	}

	return cs.client.UserChannel.UpdateOneID(uid).
		SetDisplayName(ch.DisplayName).
		SetEnabled(ch.Enabled).
		Exec(ctx)
}

func (cs *userChannelStore) Verify(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid channel ID: %w", err)
	}

	now := time.Now()
	return cs.client.UserChannel.UpdateOneID(uid).
		SetVerified(true).
		SetVerifiedAt(now).
		Exec(ctx)
}

// entUserChannelToStore converts an Ent UserChannel entity to the store.UserChannel DTO.
func entUserChannelToStore(e *platforment.UserChannel) *store.UserChannel {
	return &store.UserChannel{
		ID:          e.ID.String(),
		UserID:      e.UserID.String(),
		ChannelType: e.ChannelType,
		ExternalID:  e.ExternalID,
		DisplayName: e.DisplayName,
		Enabled:     e.Enabled,
		Verified:    e.Verified,
		VerifiedAt:  e.VerifiedAt,
		CreatedAt:   e.CreatedAt,
	}
}

// Compile-time assertion.
var _ store.UserChannelStore = (*userChannelStore)(nil)
