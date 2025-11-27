package ui

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// ReadSelection prompts the user to select from a list of options using arrow keys
func ReadSelection(options []string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no options provided")
	}

	// Put terminal in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return -1, fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	selectedIndex := 0
	
	// ANSI codes
	const (
		cursorUp    = "\033[A"
		cursorDown  = "\033[B"
		clearLine   = "\033[2K\r"
		hideCursor  = "\033[?25l"
		showCursor  = "\033[?25h"
		colorCyan   = "\033[36m"
		colorReset  = "\033[0m"
	)

	fmt.Print(hideCursor)
	defer fmt.Print(showCursor)

	// Initial render
	render := func() {
		for i, option := range options {
			if i == selectedIndex {
				fmt.Printf("%s\r> %s%s\r\n", colorCyan, option, colorReset)
			} else {
				fmt.Printf("\r  %s\r\n", option)
			}
		}
	}

	render()

	for {
		b := make([]byte, 3)
		n, err := os.Stdin.Read(b)
		if err != nil {
			return -1, err
		}

		if n == 1 {
			if b[0] == '\r' || b[0] == '\n' { // Enter
				// Clear the rendered lines so we don't clutter output
				// Actually, let's keep the selected option printed but clear the rest?
				// For now, just return. The caller will print the selection if needed.
				// But we need to move cursor down past the options to avoid overwriting?
				// Or we can clear the options and print the selected one.
				// Let's clear the options from the screen to keep it clean.
				for i := 0; i < len(options); i++ {
					fmt.Print(cursorUp)
					fmt.Print(clearLine)
				}
				return selectedIndex, nil
			} else if b[0] == 3 { // Ctrl+C
				return -1, fmt.Errorf("interrupted")
			}
		} else if n == 3 && b[0] == 27 && b[1] == 91 { // Escape sequence
			if b[2] == 65 { // Up
				if selectedIndex > 0 {
					selectedIndex--
				}
			} else if b[2] == 66 { // Down
				if selectedIndex < len(options)-1 {
					selectedIndex++
				}
			}
			
			// Re-render: Move cursor up N lines and redraw
			for i := 0; i < len(options); i++ {
				fmt.Print(cursorUp)
			}
			render()
		}
	}
}
