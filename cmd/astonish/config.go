package astonish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/schardosin/astonish/pkg/config"
)

func handleConfigCommand(args []string) error {
	if len(args) < 1 || args[0] == "-h" || args[0] == "--help" {
		printConfigUsage()
		return nil
	}

	switch args[0] {
	case "edit":
		return handleConfigEdit()
	case "show":
		return handleConfigShow()
	case "directory":
		return handleConfigDirectory()
	default:
		return fmt.Errorf("unknown config subcommand: %s", args[0])
	}
}

func printConfigUsage() {
	fmt.Println("usage: astonish config [-h] {edit,show,directory} ...")
	fmt.Println("")
	fmt.Println("positional arguments:")
	fmt.Println("  {edit,show,directory}")
	fmt.Println("                        Configuration management commands")
	fmt.Println("    edit                Open config.yaml in default editor")
	fmt.Println("    show                Print config.yaml contents")
	fmt.Println("    directory           Print the configuration directory path")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  -h, --help            show this help message and exit")
}

func handleConfigEdit() error {
	path, err := config.GetConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Try to find a suitable editor
		editors := []string{"nano", "vim", "vi", "emacs"}
		for _, e := range editors {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
		if editor == "" {
			// Fallback for Windows or if nothing found
			if _, err := exec.LookPath("notepad"); err == nil {
				editor = "notepad"
			} else {
				return fmt.Errorf("no editor found. Please set the EDITOR environment variable")
			}
		}
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func handleConfigShow() error {
	path, err := config.GetConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Config file does not exist.")
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func handleConfigDirectory() error {
	dir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}
	fmt.Println(dir)
	return nil
}
