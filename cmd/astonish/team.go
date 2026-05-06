package astonish

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/client"
)

func handleTeamCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: astonish team <subcommand>")
		fmt.Println("")
		fmt.Println("Subcommands:")
		fmt.Println("  list          List available teams")
		fmt.Println("  use <slug>    Switch active team")
		return nil
	}

	if !client.IsRemoteMode() {
		return fmt.Errorf("'team' command is only available in remote mode. Run 'astonish login' first")
	}

	switch args[0] {
	case "list", "ls":
		return handleTeamList()
	case "use":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish team use <slug>")
		}
		return handleTeamUse(args[1])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func handleTeamList() error {
	c, err := client.New()
	if err != nil {
		return err
	}

	teams, err := c.ListTeams()
	if err != nil {
		return fmt.Errorf("list teams: %w", err)
	}

	cfg, _ := client.LoadRemoteConfig()
	currentTeam := ""
	if cfg != nil {
		currentTeam = cfg.Team
	}

	if len(teams) == 0 {
		fmt.Println("No teams found.")
		return nil
	}

	for _, team := range teams {
		marker := "  "
		if team.Slug == currentTeam {
			marker = "* "
		}
		fmt.Printf("%s%s (%s)\n", marker, team.Name, team.Slug)
	}

	return nil
}

func handleTeamUse(slug string) error {
	cfg, err := client.LoadRemoteConfig()
	if err != nil || cfg == nil {
		return fmt.Errorf("failed to load remote config")
	}

	cfg.Team = slug
	if err := client.SaveRemoteConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Switched to team: %s\n", slug)
	return nil
}
