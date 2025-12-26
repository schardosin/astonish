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

	// Handle --version flag
	if os.Args[1] == "--version" || os.Args[1] == "-v" {
		printVersion()
		return nil
	}

	command := os.Args[1]
	switch command {
	case "flows", "agents": // "agents" is a hidden alias for backwards compatibility
		return handleFlowsCommand(os.Args[2:])
	case "tap":
		return handleTapCommand(os.Args[2:])
	case "studio":
		return handleStudioCommand(os.Args[2:])
	case "setup":
		return handleSetupCommand()
	case "config":
		return handleConfigCommand(os.Args[2:])
	case "tools":
		return handleToolsCommand(os.Args[2:])
	case "demo":
		return handleDemoCommand(os.Args[2:])
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printUsage() {
	fmt.Println("usage: astonish [-h] [-v] {flows,tap,studio,config,setup,tools} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {flows,tap,studio,config,setup,tools}")
	fmt.Println("                        Astonish CLI commands")
	fmt.Println("    flows               Design and run AI flows")
	fmt.Println("    tap                 Manage extension repositories")
	fmt.Println("    studio              Launch the visual editor")
	fmt.Println("    config              Manage configuration")
	fmt.Println("    setup               Run interactive setup")
	fmt.Println("    tools               Manage MCP tools")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
	fmt.Println("  -v, --version         show version information and exit")
}

