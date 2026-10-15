package daemon

import (
	"context"
	"fmt"

	"github.com/SAP/astonish/pkg/channels"
	"github.com/SAP/astonish/pkg/scheduler"
	"github.com/SAP/astonish/pkg/store"
)

// slackPlatformAllowlist rejects legacy DB identities before adapter broadcasts.
// Personal configured allowlists do not use this path.
func slackPlatformAllowlist(links []*store.UserChannel, logger interface{ Printf(string, ...any) }) []string {
	var ids []string
	for _, link := range links {
		if !link.Enabled || !link.Verified {
			continue
		}
		if _, err := channels.ParseLinkPrincipal(channels.LinkSlack, link.ExternalID); err != nil {
			if logger != nil {
				logger.Printf("[channels] Ignoring legacy Slack link for user %s; unlink and relink Slack in Settings → Channels with workspace and member IDs", link.UserID)
			}
			continue
		}
		ids = append(ids, link.ExternalID)
	}
	return ids
}

// deliveryResolver implements scheduler.DeliveryResolver using the platform
// store to look up user channel links and team membership.
type deliveryResolver struct {
	backend store.PlatformBackend
}

// Ensure deliveryResolver implements scheduler.DeliveryResolver.
var _ scheduler.DeliveryResolver = (*deliveryResolver)(nil)

// ResolveUserChannels returns all active (enabled+verified) channel targets
// for a given platform user ID.
func (r *deliveryResolver) ResolveUserChannels(ctx context.Context, userID string) ([]scheduler.DeliveryTarget, error) {
	links, err := r.backend.UserChannels().ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list channels for user %s: %w", userID, err)
	}

	var targets []scheduler.DeliveryTarget
	for _, link := range links {
		if !link.Enabled || !link.Verified {
			continue
		}
		if link.ChannelType == string(channels.LinkSlack) {
			if _, err := channels.ParseLinkPrincipal(channels.LinkSlack, link.ExternalID); err != nil {
				return nil, fmt.Errorf("user %s has a legacy or invalid Slack link: unlink and relink Slack in Settings → Channels with your workspace and member IDs: %w", userID, err)
			}
		}
		targets = append(targets, scheduler.DeliveryTarget{
			ChannelID: link.ChannelType,
			ChatID:    link.ExternalID,
		})
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("user %s has no active channel links", userID)
	}
	return targets, nil
}

// ResolveTeamMembers returns all user IDs that are members of the given org/team.
func (r *deliveryResolver) ResolveTeamMembers(ctx context.Context, orgSlug, teamSlug string) ([]string, error) {
	orgStore, err := r.backend.ForOrg(orgSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve org %s: %w", orgSlug, err)
	}

	// Resolve team slug → team ID
	team, err := orgStore.Teams().GetTeamBySlug(ctx, teamSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve team %s/%s: %w", orgSlug, teamSlug, err)
	}
	if team == nil {
		return nil, fmt.Errorf("team %s/%s not found", orgSlug, teamSlug)
	}

	members, err := orgStore.Teams().ListMembers(ctx, team.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list members of team %s/%s: %w", orgSlug, teamSlug, err)
	}

	userIDs := make([]string, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}
	return userIDs, nil
}
