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
		fmt.Println("")
		fmt.Println("To switch organizations, run 'astonish logout' then 'astonish login'")
		return nil
	}

	if !client.IsRemoteMode() {
		return fmt.Errorf("'org' command is only available in remote mode. Run 'astonish login' first")
	}

	switch args[0] {
	case "list", "ls":
		return handleOrgList()
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
