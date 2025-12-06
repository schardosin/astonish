package astonish

import (
	"flag"
	"fmt"

	"github.com/schardosin/astonish/pkg/launcher"
)

func handleStudioCommand(args []string) error {
	studioCmd := flag.NewFlagSet("studio", flag.ExitOnError)
	port := studioCmd.Int("port", 9393, "Port to run the studio server on")

	if err := studioCmd.Parse(args); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	return launcher.RunStudio(*port)
}

func printStudioUsage() {
	fmt.Println("usage: astonish studio [-h] [--port PORT]")
	fmt.Println("")
	fmt.Println("Launch the Astonish Studio visual editor")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
	fmt.Println("  --port PORT           Port to run the studio server on (default: 9393)")
}
