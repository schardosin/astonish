package astonish

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/config"
)

func handleConfigCommand(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: astonish config <subcommand>")
		fmt.Println("Subcommands: directory")
		return fmt.Errorf("no config subcommand provided")
	}

	switch args[0] {
	case "directory":
		return handleConfigDirectory()
	default:
		return fmt.Errorf("unknown config subcommand: %s", args[0])
	}
}

func handleConfigDirectory() error {
	dir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}
	fmt.Println(dir)
	return nil
}
