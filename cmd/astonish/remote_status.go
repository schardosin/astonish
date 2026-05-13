package astonish

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/client"
)

func handleStatusCommand(_ []string) error {
	cfg, err := client.LoadRemoteConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg == nil || cfg.URL == "" {
		fmt.Println("Mode: personal (local)")
		fmt.Println("No remote server configured.")
		fmt.Println("")
		fmt.Println("To connect to a remote server:")
		fmt.Println("  astonish login <server-url>")
		return nil
	}

	fmt.Println("Mode: remote (platform)")
	fmt.Printf("Server: %s\n", cfg.URL)
	fmt.Printf("Org:    %s\n", cfg.Org)
	if cfg.Team != "" {
		fmt.Printf("Team:   %s\n", cfg.Team)
	}
	if cfg.UserEmail != "" {
		fmt.Printf("User:   %s\n", cfg.UserEmail)
	}

	// Check connectivity
	c, err := client.New()
	if err != nil {
		fmt.Printf("\nStatus: disconnected (%s)\n", err)
		return nil
	}

	if err := c.Ping(); err != nil {
		fmt.Printf("\nStatus: unreachable (%s)\n", err)
	} else {
		fmt.Println("\nStatus: connected")
	}

	return nil
}
