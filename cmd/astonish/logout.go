package astonish

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/client"
)

func handleLogoutCommand(_ []string) error {
	if !client.IsRemoteMode() {
		fmt.Println("Not connected to a remote server.")
		return nil
	}

	cfg, _ := client.LoadRemoteConfig()
	if err := client.Logout(); err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}

	if cfg != nil {
		fmt.Printf("Disconnected from %s\n", cfg.URL)
	} else {
		fmt.Println("Logged out.")
	}
	return nil
}
