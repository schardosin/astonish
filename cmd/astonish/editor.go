package astonish

import (
	"fmt"
	"os"
	"os/exec"
)

func openInEditor(path string) error {
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
