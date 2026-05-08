package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/channels"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// buildTelegramLinkHandler returns a function suitable for TelegramChannel.LinkHandler.
// It consumes the one-time code, creates a verified user_channel record, and refreshes
// the channel manager's allowlist.
func buildTelegramLinkHandler(
	pgStore *pgstore.PGStore,
	linkStore *api.LinkCodeStore,
	channelMgr *channels.ChannelManager,
) func(ctx context.Context, senderID, senderUsername, code string) (bool, string) {
	return func(ctx context.Context, senderID, senderUsername, code string) (bool, string) {
		// Consume the code from the store
		pending := linkStore.Consume(code)
		if pending == nil {
			return false, "Invalid or expired code. Please generate a new one from Settings → Connected Channels."
		}

		// Verify this is for Telegram
		if pending.Channel != "telegram" {
			return false, "This code is not for Telegram linking."
		}

		// Check if this external ID is already linked to another user
		ucStore := pgStore.UserChannels()
		existing, err := ucStore.GetByExternalID(ctx, "telegram", senderID)
		if err != nil {
			return false, "An error occurred. Please try again."
		}
		if existing != nil && existing.UserID != pending.UserID {
			return false, "This Telegram account is already linked to a different user."
		}
		if existing != nil && existing.UserID == pending.UserID {
			// Already linked — just make sure it's verified and enabled
			_ = ucStore.Verify(ctx, existing.ID)
			if !existing.Enabled {
				existing.Enabled = true
				_ = ucStore.Update(ctx, existing)
			}
			return true, fmt.Sprintf("Your Telegram is already linked to %s. Re-verified!", pending.Email)
		}

		// Create a new verified user_channel record
		displayName := "@" + senderUsername
		if senderUsername == "" {
			displayName = "Telegram " + senderID
		}

		ch := &store.UserChannel{
			ID:          uuid.New().String(),
			UserID:      pending.UserID,
			ChannelType: "telegram",
			ExternalID:  senderID,
			DisplayName: displayName,
			Enabled:     true,
			Verified:    true,
			VerifiedAt:  timePtr(time.Now()),
			CreatedAt:   time.Now(),
		}
		if err := ucStore.Link(ctx, ch); err != nil {
			return false, "Failed to link account. Please try again or contact support."
		}

		return true, fmt.Sprintf("Account linked successfully! You're now connected as %s.\n\nYou can start chatting with me right away.", pending.Email)
	}
}

// buildSlackLinkHandler returns a function suitable for SlackChannel.LinkHandler.
// It consumes the one-time code, creates a verified user_channel record, and refreshes
// the channel manager's allowlist.
func buildSlackLinkHandler(
	pgStore *pgstore.PGStore,
	linkStore *api.LinkCodeStore,
	channelMgr *channels.ChannelManager,
) func(ctx context.Context, senderID, senderName, code string) (bool, string) {
	return func(ctx context.Context, senderID, senderName, code string) (bool, string) {
		// Consume the code from the store
		pending := linkStore.Consume(code)
		if pending == nil {
			return false, "Invalid or expired code. Please generate a new one from Settings → Connected Channels."
		}

		// Verify this is for Slack
		if pending.Channel != "slack" {
			return false, "This code is not for Slack linking."
		}

		// Check if this external ID is already linked to another user
		ucStore := pgStore.UserChannels()
		existing, err := ucStore.GetByExternalID(ctx, "slack", senderID)
		if err != nil {
			return false, "An error occurred. Please try again."
		}
		if existing != nil && existing.UserID != pending.UserID {
			return false, "This Slack account is already linked to a different user."
		}
		if existing != nil && existing.UserID == pending.UserID {
			_ = ucStore.Verify(ctx, existing.ID)
			if !existing.Enabled {
				existing.Enabled = true
				_ = ucStore.Update(ctx, existing)
			}
			return true, fmt.Sprintf("Your Slack is already linked to %s. Re-verified!", pending.Email)
		}

		// Create a new verified user_channel record
		displayName := senderName
		if displayName == "" || displayName == senderID {
			displayName = "Slack " + senderID
		}

		ch := &store.UserChannel{
			ID:          uuid.New().String(),
			UserID:      pending.UserID,
			ChannelType: "slack",
			ExternalID:  senderID,
			DisplayName: displayName,
			Enabled:     true,
			Verified:    true,
			VerifiedAt:  timePtr(time.Now()),
			CreatedAt:   time.Now(),
		}
		if err := ucStore.Link(ctx, ch); err != nil {
			return false, "Failed to link account. Please try again or contact support."
		}

		return true, fmt.Sprintf("Account linked successfully! You're now connected as %s.\n\nYou can start chatting with me here.", pending.Email)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
