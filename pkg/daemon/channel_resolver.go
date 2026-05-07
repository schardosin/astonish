package daemon

import (
	"context"
	"fmt"

	"github.com/schardosin/astonish/pkg/channels"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// channelPlatformResolver implements channels.PlatformResolver using the
// platform store to look up user-channel links and inject team-scoped context.
type channelPlatformResolver struct {
	pgStore *pgstore.PGStore
}

// Ensure channelPlatformResolver implements channels.PlatformResolver.
var _ channels.PlatformResolver = (*channelPlatformResolver)(nil)

// ResolveChannelUser looks up the external channel identity in user_channels,
// finds the user's preferred org/team, and returns a context enriched with
// team-scoped stores (credentials, flows, skills, MCP servers, memories).
func (r *channelPlatformResolver) ResolveChannelUser(
	ctx context.Context,
	channelType, externalID string,
) (context.Context, string, string, error) {
	// Look up the user-channel link
	link, err := r.pgStore.UserChannels().GetByExternalID(ctx, channelType, externalID)
	if err != nil {
		return ctx, "", "", fmt.Errorf("lookup failed: %w", err)
	}
	if link == nil {
		return ctx, "", "", fmt.Errorf("no linked user for %s/%s", channelType, externalID)
	}
	if !link.Enabled || !link.Verified {
		return ctx, "", "", fmt.Errorf("channel link for %s/%s is not active", channelType, externalID)
	}

	// Get the user info for display name
	user, err := r.pgStore.Users().GetByID(ctx, link.UserID)
	if err != nil || user == nil {
		return ctx, "", "", fmt.Errorf("user %s not found", link.UserID)
	}

	// Resolve org and team
	orgSlug := link.DefaultOrgSlug
	teamSlug := link.DefaultTeamSlug

	if orgSlug == "" || teamSlug == "" {
		// Fall back to user's first org membership and first team
		orgs, err := r.pgStore.Organizations().GetUserOrgs(ctx, link.UserID)
		if err == nil && len(orgs) > 0 {
			if orgSlug == "" {
				orgSlug = orgs[0].OrgSlug
			}
		}
		if orgSlug != "" && teamSlug == "" {
			// Get first team the user belongs to in this org
			if orgDS, orgErr := r.pgStore.ForOrg(orgSlug); orgErr == nil {
				teams, teamErr := orgDS.Teams().ListTeamsForUser(ctx, link.UserID)
				if teamErr == nil && len(teams) > 0 {
					teamSlug = teams[0].Slug
				}
			}
		}
	}

	if orgSlug == "" || teamSlug == "" {
		return ctx, link.UserID, user.DisplayName, fmt.Errorf("user %s has no org/team configured for channel routing", link.UserID)
	}

	// Get the org data store
	orgStore, err := r.pgStore.ForOrg(orgSlug)
	if err != nil {
		return ctx, link.UserID, user.DisplayName, fmt.Errorf("failed to resolve org %s: %w", orgSlug, err)
	}

	// Get the team data store
	teamStore := orgStore.ForTeam(teamSlug)

	// Inject team-scoped stores into the context
	enrichedCtx := ctx
	enrichedCtx = store.WithCredentialStore(enrichedCtx, teamStore.Credentials())
	enrichedCtx = store.WithFlowStore(enrichedCtx, teamStore.Flows())
	enrichedCtx = store.WithSkillStores(enrichedCtx, &store.SkillStores{
		Team: teamStore.Skills(),
	})
	enrichedCtx = store.WithMCPServerStores(enrichedCtx, &store.MCPServerStores{
		Org:  orgStore.OrgMCPServers(),
		Team: teamStore.MCPServers(),
	})
	enrichedCtx = store.WithMemoryStore(enrichedCtx, teamStore.Memories())

	return enrichedCtx, link.UserID, user.DisplayName, nil
}
