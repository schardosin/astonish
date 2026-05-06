package astonish

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/client"
)

func handleOrgCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println("Usage: astonish org <subcommand>")
		fmt.Println("")
		fmt.Println("Subcommands:")
		fmt.Println("  list          List available organizations")
		fmt.Println("  use <slug>    Switch active organization")
		return nil
	}

	if !client.IsRemoteMode() {
		return fmt.Errorf("'org' command is only available in remote mode. Run 'astonish login' first")
	}

	switch args[0] {
	case "list", "ls":
		return handleOrgList()
	case "use":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish org use <slug>")
		}
		return handleOrgUse(args[1])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func handleOrgList() error {
	c, err := client.New()
	if err != nil {
		return err
	}

	orgs, err := c.ListOrgs()
	if err != nil {
		return fmt.Errorf("list orgs: %w", err)
	}

	cfg, _ := client.LoadRemoteConfig()
	currentOrg := ""
	if cfg != nil {
		currentOrg = cfg.Org
	}

	if len(orgs) == 0 {
		fmt.Println("No organizations found.")
		return nil
	}

	for _, org := range orgs {
		marker := "  "
		if org.Slug == currentOrg {
			marker = "* "
		}
		fmt.Printf("%s%s (%s) [%s]\n", marker, org.Name, org.Slug, org.Role)
	}

	return nil
}

func handleOrgUse(slug string) error {
	cfg, err := client.LoadRemoteConfig()
	if err != nil || cfg == nil {
		return fmt.Errorf("failed to load remote config")
	}

	cfg.Org = slug
	if err := client.SaveRemoteConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Switched to organization: %s\n", slug)
	fmt.Println("Note: You may need to run 'astonish login' again if your token is scoped to a different org.")
	return nil
}
