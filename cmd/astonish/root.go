package astonish

import (
	"fmt"
	"os"
)

// Execute is the main entry point for the CLI
func Execute() error {
	if len(os.Args) < 2 {
		printUsage()
		return fmt.Errorf("no command provided")
	}

	command := os.Args[1]
	switch command {
	case "agents":
		return handleAgentsCommand(os.Args[2:])
	case "setup":
		return handleSetupCommand()
	case "config":
		return handleConfigCommand(os.Args[2:])
	case "tools":
		return handleToolsCommand(os.Args[2:])
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printUsage() {
	fmt.Println("Usage: astonish <command> [args]")
	fmt.Println("Commands: agents, config, setup, tools")
}
