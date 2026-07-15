package tools

import (
	"fmt"

	"github.com/SAP/astonish/pkg/store"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// --- list_team_members tool ---

// ListTeamMembersArgs is the input for the list_team_members tool.
type ListTeamMembersArgs struct {
	// No required args — uses team from context
}

// TeamMemberInfo represents a team member with their linked channel types.
type TeamMemberInfo struct {
	UserID          string   `json:"user_id"`
	DisplayName     string   `json:"display_name"`
	Email           string   `json:"email"`
	Role            string   `json:"role"`
	LinkedChannels  []string `json:"linked_channels"`  // channel types with active links (e.g., ["telegram", "email"])
}

// ListTeamMembersResult is the output of the list_team_members tool.
type ListTeamMembersResult struct {
	Members []TeamMemberInfo `json:"members"`
	Count   int             `json:"count"`
	Message string          `json:"message,omitempty"`
}

func listTeamMembers(ctx tool.Context, args ListTeamMembersArgs) (ListTeamMembersResult, error) {
	// Get org/team slugs from context
	orgSlug := store.OrgSlugFromContext(ctx)
	teamSlug := store.TeamSlugFromContext(ctx)

	if orgSlug == "" || teamSlug == "" {
		return ListTeamMembersResult{
			Message: "Team members are only available in platform mode. In personal mode, you are the only user.",
		}, nil
	}

	// Get the Services instance to access platform stores
	svc := store.FromContext(ctx)
	if svc == nil || svc.Platform == nil || svc.TenantRouter == nil {
		return ListTeamMembersResult{
			Message: "Platform services not available",
		}, nil
	}

	// Resolve team members via org data store
	orgStore, err := svc.TenantRouter.ForOrg(orgSlug)
	if err != nil {
		return ListTeamMembersResult{
			Message: fmt.Sprintf("Failed to access organization: %v", err),
		}, nil
	}

	team, err := orgStore.Teams().GetTeamBySlug(ctx, teamSlug)
	if err != nil || team == nil {
		return ListTeamMembersResult{
			Message: fmt.Sprintf("Team %q not found", teamSlug),
		}, nil
	}

	members, err := orgStore.Teams().ListMembers(ctx, team.ID)
	if err != nil {
		return ListTeamMembersResult{
			Message: fmt.Sprintf("Failed to list team members: %v", err),
		}, nil
	}

	// Get user details and linked channels for each member
	userStore := svc.Platform.Users()
	channelStore := svc.Platform.UserChannels()

	result := make([]TeamMemberInfo, 0, len(members))
	for _, m := range members {
		info := TeamMemberInfo{
			UserID: m.UserID,
			Role:   m.Role,
			Email:  m.Email,
		}

		// Get display name from platform users table
		if u, err := userStore.GetByID(ctx, m.UserID); err == nil && u != nil {
			info.DisplayName = u.DisplayName
			if info.Email == "" {
				info.Email = u.Email
			}
		}

		// Get linked channels (only enabled+verified)
		if links, err := channelStore.ListByUser(ctx, m.UserID); err == nil {
			for _, link := range links {
				if link.Enabled && link.Verified {
					info.LinkedChannels = append(info.LinkedChannels, link.ChannelType)
				}
			}
		}

		result = append(result, info)
	}

	msg := fmt.Sprintf("%d team member(s) in %s/%s", len(result), orgSlug, teamSlug)

	return ListTeamMembersResult{
		Members: result,
		Count:   len(result),
		Message: msg,
	}, nil
}

// NewListTeamMembersTool creates the list_team_members tool.
func NewListTeamMembersTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "list_team_members",
		Description: `List all members of the current team with their display names, emails, roles, and linked delivery channels.
Use this tool before scheduling a job with delivery_mode "members" to find out which team members exist and what channels they have linked (telegram, email, slack).
This helps you suggest appropriate delivery targets when the user wants to schedule something for specific people.`,
	}, listTeamMembers)
}
