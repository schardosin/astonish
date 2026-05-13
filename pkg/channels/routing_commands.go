package channels

import (
	"context"
	"fmt"
	"strings"
)

// RegisterRoutingCommands adds /org, /team, and /context commands to the registry.
// These commands let channel users switch their active org/team routing.
func RegisterRoutingCommands(r *CommandRegistry) {
	r.Register(orgCommand())
	r.Register(teamCommand())
	r.Register(contextCommand())
}

func orgCommand() *Command {
	return &Command{
		Name:        "org",
		Description: "Switch active organization (e.g. /org my-org)",
		Handler: func(ctx context.Context, cc CommandContext) (string, error) {
			resolver := cc.Manager.GetPlatformResolver()
			if resolver == nil {
				return "Organization routing is only available in platform mode.", nil
			}

			// Parse the org slug from the command arguments
			slug := strings.TrimSpace(strings.TrimPrefix(cc.RawText, "/org"))
			if slug == "" {
				// Show current routing
				pref := resolver.GetRoutingPref(cc.ChannelID, cc.SenderID)
				if pref != nil && pref.OrgSlug != "" {
					return fmt.Sprintf("Current org: %s\n\nUsage: /org <slug> — switch to a different organization", pref.OrgSlug), nil
				}
				return "No org preference set (using default).\n\nUsage: /org <slug> — switch to a different organization\nUse /context to see available orgs and teams.", nil
			}

			// Get current pref to preserve team if possible
			currentPref := resolver.GetRoutingPref(cc.ChannelID, cc.SenderID)
			teamSlug := ""
			if currentPref != nil && currentPref.OrgSlug == slug {
				teamSlug = currentPref.TeamSlug
			}
			// Setting org resets team to first-available in new org
			resolver.SetRoutingPref(cc.ChannelID, cc.SenderID, slug, teamSlug)

			// Validate by doing a test resolve
			_, _, _, err := resolver.ResolveChannelUserWithHint(ctx, cc.ChannelID, cc.SenderID, &RoutingHint{
				OrgSlug:  slug,
				TeamSlug: teamSlug,
			})
			if err != nil {
				// Revert the pref on failure
				if currentPref != nil {
					resolver.SetRoutingPref(cc.ChannelID, cc.SenderID, currentPref.OrgSlug, currentPref.TeamSlug)
				} else {
					resolver.SetRoutingPref(cc.ChannelID, cc.SenderID, "", "")
				}
				return fmt.Sprintf("Failed to switch org: %v", err), nil
			}

			// On success, confirm
			msg := fmt.Sprintf("Switched to org: %s", slug)
			if teamSlug == "" {
				msg += " (using default team)"
			}
			return msg, nil
		},
	}
}

func teamCommand() *Command {
	return &Command{
		Name:        "team",
		Description: "Switch active team (e.g. /team general)",
		Handler: func(ctx context.Context, cc CommandContext) (string, error) {
			resolver := cc.Manager.GetPlatformResolver()
			if resolver == nil {
				return "Team routing is only available in platform mode.", nil
			}

			// Parse the team slug from the command arguments
			slug := strings.TrimSpace(strings.TrimPrefix(cc.RawText, "/team"))
			if slug == "" {
				// Show current routing
				pref := resolver.GetRoutingPref(cc.ChannelID, cc.SenderID)
				if pref != nil && pref.TeamSlug != "" {
					return fmt.Sprintf("Current team: %s (org: %s)\n\nUsage: /team <slug> — switch to a different team", pref.TeamSlug, pref.OrgSlug), nil
				}
				return "No team preference set (using default).\n\nUsage: /team <slug> — switch to a different team\nUse /context to see available orgs and teams.", nil
			}

			// Get current pref — we need an org to set a team
			currentPref := resolver.GetRoutingPref(cc.ChannelID, cc.SenderID)
			orgSlug := ""
			if currentPref != nil {
				orgSlug = currentPref.OrgSlug
			}
			// If no org pref, the resolver will use the default org

			// Set the pref (resolver will validate on next message)
			resolver.SetRoutingPref(cc.ChannelID, cc.SenderID, orgSlug, slug)

			// Validate by doing a test resolve
			hint := &RoutingHint{OrgSlug: orgSlug, TeamSlug: slug}
			if orgSlug == "" {
				// No org hint — let resolver figure it out; we just set pref
				hint = nil
			}
			_, _, _, err := resolver.ResolveChannelUserWithHint(ctx, cc.ChannelID, cc.SenderID, hint)
			if err != nil {
				// Revert on failure
				if currentPref != nil {
					resolver.SetRoutingPref(cc.ChannelID, cc.SenderID, currentPref.OrgSlug, currentPref.TeamSlug)
				} else {
					resolver.SetRoutingPref(cc.ChannelID, cc.SenderID, "", "")
				}
				return fmt.Sprintf("Failed to switch team: %v", err), nil
			}

			// Success — read back what resolved
			newPref := resolver.GetRoutingPref(cc.ChannelID, cc.SenderID)
			if newPref != nil && newPref.OrgSlug != "" {
				return fmt.Sprintf("Switched to team: %s (org: %s)", slug, newPref.OrgSlug), nil
			}
			return fmt.Sprintf("Switched to team: %s", slug), nil
		},
	}
}

func contextCommand() *Command {
	return &Command{
		Name:        "context",
		Description: "Show current routing context and available orgs/teams",
		Handler: func(ctx context.Context, cc CommandContext) (string, error) {
			resolver := cc.Manager.GetPlatformResolver()
			if resolver == nil {
				return "Routing context is only available in platform mode.", nil
			}

			var b strings.Builder

			// Current routing
			pref := resolver.GetRoutingPref(cc.ChannelID, cc.SenderID)
			b.WriteString("Current Routing\n")
			if pref != nil && pref.OrgSlug != "" {
				b.WriteString(fmt.Sprintf("  Org:  %s\n", pref.OrgSlug))
				if pref.TeamSlug != "" {
					b.WriteString(fmt.Sprintf("  Team: %s\n", pref.TeamSlug))
				} else {
					b.WriteString("  Team: (default)\n")
				}
			} else {
				b.WriteString("  Using defaults (first org, first team)\n")
			}

			// Available routes
			routes, err := resolver.ListUserRoutes(ctx, cc.ChannelID, cc.SenderID)
			if err != nil {
				b.WriteString(fmt.Sprintf("\nCould not list available routes: %v\n", err))
				return b.String(), nil
			}

			if len(routes) == 0 {
				b.WriteString("\nNo org/team memberships found.\n")
				return b.String(), nil
			}

			b.WriteString("\nAvailable Routes\n")
			// Group by org
			currentOrg := ""
			for _, r := range routes {
				if r.OrgSlug != currentOrg {
					currentOrg = r.OrgSlug
					name := r.OrgName
					if name == "" || name == r.OrgSlug {
						b.WriteString(fmt.Sprintf("  %s\n", r.OrgSlug))
					} else {
						b.WriteString(fmt.Sprintf("  %s (%s)\n", name, r.OrgSlug))
					}
				}
				teamName := r.TeamName
				if teamName == "" || teamName == r.TeamSlug {
					b.WriteString(fmt.Sprintf("    - %s\n", r.TeamSlug))
				} else {
					b.WriteString(fmt.Sprintf("    - %s (%s)\n", teamName, r.TeamSlug))
				}
			}

			b.WriteString("\nCommands:\n")
			b.WriteString("  /org <slug>  — switch organization\n")
			b.WriteString("  /team <slug> — switch team\n")

			return b.String(), nil
		},
	}
}
