package daemon

import (
	"context"
	"fmt"

	"github.com/SAP/astonish/pkg/scheduler"
	"github.com/SAP/astonish/pkg/store"
)

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
