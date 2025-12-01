package astonish

import (
	"fmt"
	"os"
)

// Execute is the main entry point for the CLI
func Execute() error {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		printUsage()
		if len(os.Args) < 2 {
			return fmt.Errorf("no command provided")
		}
		return nil
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
	fmt.Println("usage: astonish [-h] {agents,config,setup,tools} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {agents,config,setup,tools}")
	fmt.Println("                        Astonish CLI commands")
	fmt.Println("    agents              Manage AI agents")
	fmt.Println("    config              Manage configuration")
	fmt.Println("    setup               Run interactive setup")
	fmt.Println("    tools               Manage MCP tools")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
}
