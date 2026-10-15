package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/channels"
	"github.com/SAP/astonish/pkg/store"
	"github.com/google/uuid"
)

func buildTelegramLinkHandler(backend store.PlatformBackend, linkStore api.LinkCodeBackend, _ *channels.ChannelManager) func(context.Context, string, string, string) (bool, string) {
	return buildChannelLinkHandler(backend, linkStore, channels.LinkTelegram)
}

// Slack adapters supply a validated T…:U… principal, never a bare member ID.
func buildSlackLinkHandler(backend store.PlatformBackend, linkStore api.LinkCodeBackend, _ *channels.ChannelManager) func(context.Context, string, string, string) (bool, string) {
	return buildChannelLinkHandler(backend, linkStore, channels.LinkSlack)
}

func buildChannelLinkHandler(backend store.PlatformBackend, linkStore api.LinkCodeBackend, channel channels.LinkChannel) func(context.Context, string, string, string) (bool, string) {
	return func(ctx context.Context, senderID, senderName, code string) (bool, string) {
		pending := api.ConsumeChannelLinkCode(ctx, linkStore, channel, senderID, code)
		if pending == nil {
			return false, "Invalid or expired code, wrong account, or too many attempts. Check the expected account in Settings → Connected Channels and try again later."
		}
		return completeChannelLink(ctx, backend, channel, pending, senderID, senderName)
	}
}

func completeChannelLink(ctx context.Context, backend store.PlatformBackend, channel channels.LinkChannel, pending *api.PendingLink, senderID, senderName string) (bool, string) {
	ucStore := backend.UserChannels()
	existing, err := ucStore.GetByExternalID(ctx, string(channel), senderID)
	if err != nil {
		return false, "An error occurred. Please generate a new code and try again."
	}
	if existing != nil && existing.UserID != pending.UserID {
		return false, "This account is already linked to a different user."
	}
	if existing != nil {
		if err := ucStore.Verify(ctx, existing.ID); err != nil {
			return false, "Failed to verify account."
		}
		if !existing.Enabled {
			existing.Enabled = true
			if err := ucStore.Update(ctx, existing); err != nil {
				return false, "Failed to enable account."
			}
		}
		return true, fmt.Sprintf("Account linked to %s. Re-verified!", pending.Email)
	}
	displayName := senderName
	if channel == channels.LinkTelegram && displayName != "" {
		displayName = "@" + displayName
	}
	if displayName == "" {
		displayName = string(channel) + " " + senderID
	}
	ch := &store.UserChannel{
		ID: uuid.New().String(), UserID: pending.UserID, ChannelType: string(channel),
		ExternalID: senderID, DisplayName: displayName, Enabled: true, Verified: true,
		VerifiedAt: timePtr(time.Now()), CreatedAt: time.Now(),
	}
	if err := ucStore.Link(ctx, ch); err != nil {
		return false, "Failed to link account. Please generate a new code and try again."
	}
	return true, fmt.Sprintf("Account linked successfully! You're now connected as %s.\n\nYou can start chatting with me here.", pending.Email)
}

func timePtr(t time.Time) *time.Time { return &t }
