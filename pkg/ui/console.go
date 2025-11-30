package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// ReadSelection prompts the user to select from a list of options using huh
func ReadSelection(options []string, title string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
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
