package astonish

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/schardosin/astonish/pkg/client"
	"github.com/schardosin/astonish/pkg/skills"
)

func handleSkillsCommand(args []string) error {
	if len(args) == 0 {
		return handleSkillsList()
	}

	switch args[0] {
	case "list":
		return handleSkillsList()
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish skills show <name>")
		}
		return handleSkillsShow(args[1])
	case "check":
		return handleSkillsCheck()
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish skills create <name>")
		}
		return handleSkillsCreate(args[1])
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish skills install <slug|url|clawhub:slug>")
		}
		return handleSkillsInstall(args[1])
	default:
		fmt.Println("usage: astonish skills {list,show,check,create,install}")
		fmt.Println("")
		fmt.Println("  list              List all available skills (from platform)")
		fmt.Println("  show <name>       Show full skill content from platform")
		fmt.Println("  check             Check eligibility of platform skills")
		fmt.Println("  create <name>     Create a new custom skill in your team/org")
		fmt.Println("  install <input>   Install a skill from ClawHub (server-side)")
		fmt.Println("                    Accepts: slug, clawhub:slug, or full URL")
		return fmt.Errorf("unknown skills command: %s", args[0])
	}
}

func handleSkillsList() error {
	c, err := client.New()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	resp, err := c.ListSkills()
	if err != nil {
		return fmt.Errorf("failed to list skills from platform: %w", err)
	}

	if len(resp.Skills) == 0 {
		fmt.Println("No skills found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tDESCRIPTION\tSOURCE\tSTATUS")

	eligible := 0
	for _, s := range resp.Skills {
		status := "eligible"
		if !s.Eligible {
			if len(s.Missing) > 0 {
				status = fmt.Sprintf("missing: %s", strings.Join(s.Missing, ", "))
			} else {
				status = "ineligible"
			}
		} else {
			eligible++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, truncate(s.Description, 55), s.Source, status)
	}
	w.Flush()

	fmt.Printf("\n%d eligible, %d total\n", eligible, len(resp.Skills))
	return nil
}

func handleSkillsShow(name string) error {
	c, err := client.New()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	content, err := c.GetSkillContent(name)
	if err != nil {
		return fmt.Errorf("failed to fetch skill %q: %w", name, err)
	}

	fmt.Printf("# %s\n\n", content.Name)
	fmt.Printf("Description: %s\n", content.Description)
	fmt.Printf("Source: %s\n", content.Source)
	fmt.Printf("Editable: %v\n", content.Editable)
	fmt.Printf("\n---\n\n%s\n", content.Content)
	return nil
}

func handleSkillsCheck() error {
	c, err := client.New()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	resp, err := c.ListSkills()
	if err != nil {
		return fmt.Errorf("failed to list skills: %w", err)
	}

	if len(resp.Skills) == 0 {
		fmt.Println("No skills found.")
		return nil
	}

	eligible := 0
	ineligible := 0
	for _, s := range resp.Skills {
		if s.Eligible {
			fmt.Printf("  ✓ %s\n", s.Name)
			eligible++
		} else {
			missing := s.Missing
			if len(missing) == 0 {
				missing = []string{"unknown"}
			}
			fmt.Printf("  ✗ %s (missing: %s)\n", s.Name, strings.Join(missing, ", "))
			ineligible++
		}
	}

	fmt.Printf("\n%d eligible, %d ineligible\n", eligible, ineligible)
	return nil
}

func handleSkillsCreate(name string) error {
	c, err := client.New()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Default to team scope on the server
	if err := c.CreateSkill(name, "team"); err != nil {
		return fmt.Errorf("failed to create skill on platform: %w", err)
	}

	fmt.Printf("Created skill '%s' in your team (platform).\n", name)
	fmt.Println("Use the Studio UI or PUT /api/skills/{name}/content to edit it.")
	return nil
}

func handleSkillsInstall(input string) error {
	// Optional early validation on client side (nice error messages)
	if _, err := skills.ParseClawHubInput(input); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}

	c, err := client.New()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	fmt.Printf("Requesting server to install %q from ClawHub...\n", input)

	resp, err := c.InstallSkill(input)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("\nInstalled to platform (%s scope): %s\n", resp.Scope, resp.Name)
	if resp.Version != "" {
		fmt.Printf("Version: %s\n", resp.Version)
	}
	fmt.Printf("Main skill + %d auxiliary files saved to database.\n", resp.FilesSaved)
	if resp.Description != "" {
		fmt.Printf("Description: %s\n", resp.Description)
	}
	fmt.Println("Status: eligible (server-side check not re-run here)")

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
