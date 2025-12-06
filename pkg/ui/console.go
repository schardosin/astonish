package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
)

// isRunningUnderDebugger checks if the application is running under a debugger
// by checking if stdin is not a terminal (which happens with dlv dap)
func isRunningUnderDebugger() bool {
	// Check if /dev/tty is accessible
	_, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	return err != nil
}

// ReadSelection prompts the user to select from a list of options using huh
func ReadSelection(options []string, title string, description string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	// Fall back to simple input if running under debugger
	if isRunningUnderDebugger() {
		return readSelectionFallback(options, title, description)
	}

	var selected string
	
	// Create huh options
	huhOptions := make([]huh.Option[string], len(options))
	for i, opt := range options {
		huhOptions[i] = huh.NewOption(opt, opt)
	}

	// Create and run the form
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Description(description).
				Options(huhOptions...).
				Value(&selected),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return selected, nil
}

// readSelectionFallback provides a simple text-based selection when TTY is not available
func readSelectionFallback(options []string, title string, description string) (string, error) {
	fmt.Println("\n" + title)
	if description != "" {
		fmt.Println(description)
	}
	fmt.Println()
	
	for i, opt := range options {
		fmt.Printf("%d. %s\n", i+1, opt)
	}
	
	fmt.Print("\nEnter your choice (1-", len(options), "): ")
	
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	
	input = strings.TrimSpace(input)
	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(options) {
		return "", fmt.Errorf("invalid choice: %s", input)
	}
	
	return options[choice-1], nil
}

// ReadInput prompts the user for text input using huh
func ReadInput(title string, description string) (string, error) {
	// Fall back to simple input if running under debugger
	if isRunningUnderDebugger() {
		return readInputFallback(title, description)
	}

	var input string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				Value(&input),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return input, nil
}

// readInputFallback provides simple text-based input when TTY is not available
func readInputFallback(title string, description string) (string, error) {
	fmt.Println("\n" + title)
	if description != "" {
		fmt.Println(description)
	}
	fmt.Print("\nInput: ")
	
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(input), nil
}
