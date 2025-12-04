package astonish

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider/openrouter"
	"github.com/schardosin/astonish/pkg/provider/sap"
)

func handleSetupCommand() error {
	// Load config
	cfg, err := config.LoadAppConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		// Initialize empty config if load fails
		cfg = &config.AppConfig{
			Providers: make(map[string]config.ProviderConfig),
			General:   config.GeneralConfig{},
		}
	}

	// --- STEP 1: Provider Selection ---
	var selectedProviderID string
	
	// Define options
	options := []huh.Option[string]{
		huh.NewOption("Anthropic", "anthropic"),
		huh.NewOption("Google GenAI", "gemini"),
		huh.NewOption("Groq", "groq"),
		huh.NewOption("LM Studio", "lm_studio"),
		huh.NewOption("Ollama", "ollama"),
		huh.NewOption("OpenAI", "openai"),
		huh.NewOption("Openrouter", "openrouter"),
		huh.NewOption("SAP AI Core", "sap_ai_core"),
	}

	// Run selection form
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a provider to configure").
				Options(options...).
				Value(&selectedProviderID),
		),
	).Run()
	if err != nil {
		return err
	}

	// Initialize provider config if nil
	if cfg.Providers[selectedProviderID] == nil {
		cfg.Providers[selectedProviderID] = make(config.ProviderConfig)
	}
	
	pCfg := cfg.Providers[selectedProviderID]

	// --- STEP 2: Configuration Form ---
	switch selectedProviderID {
	case "anthropic":
		runAPIKeyForm("Anthropic API Key", "api_key", pCfg)
	case "gemini":
		runAPIKeyForm("Google API Key", "api_key", pCfg)
	case "groq":
		runAPIKeyForm("Groq API Key", "api_key", pCfg)
	case "lm_studio":
		runBaseURLForm("LM Studio Base URL", "http://localhost:1234/v1", pCfg)
	case "ollama":
		runOllamaForm(pCfg)
	case "openai":
		runAPIKeyForm("OpenAI API Key", "api_key", pCfg)
	case "openrouter":
		runAPIKeyForm("OpenRouter API Key", "api_key", pCfg)
		if err := fetchAndSelectOpenRouterModel(pCfg, cfg); err != nil {
			fmt.Printf("Warning: Failed to fetch/select OpenRouter models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "sap_ai_core":
		runSAPAICoreForm(pCfg)
		// Special handling for SAP AI Core model selection
		if err := fetchAndSelectSAPModel(pCfg, cfg); err != nil {
			fmt.Printf("Warning: Failed to fetch/select SAP models: %v\n", err)
		} else {
			// Skip generic model selection if we did it specifically for SAP
			goto SaveConfig
		}
	}

	// --- STEP 3: Default Model Selection (Generic) ---
	// Only ask if not already handled (like in SAP AI Core)
	{
		var defaultModel string = cfg.General.DefaultModel
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Default Model").
					Description("Leave empty to keep current").
					Value(&defaultModel),
			),
		).Run()
		if err == nil && defaultModel != "" {
			cfg.General.DefaultModel = defaultModel
		}
	}

SaveConfig:
	// Set as default provider
	cfg.General.DefaultProvider = selectedProviderID

	// Save config
	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("error saving config: %w", err)
	}

	printSuccess(fmt.Sprintf("%s configured successfully!", selectedProviderID))
	return nil
}

// Helper functions for forms

func runAPIKeyForm(title string, key string, pCfg config.ProviderConfig) {
	val := pCfg[key]
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				EchoMode(huh.EchoModePassword).
				Value(&val),
		),
	).Run()
	if err != nil {
		log.Fatal(err)
	}
	pCfg[key] = val
}

func runBaseURLForm(title string, defaultVal string, pCfg config.ProviderConfig) {
	val := pCfg["base_url"]
	if val == "" {
		val = defaultVal
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Value(&val),
		),
	).Run()
	if err != nil {
		log.Fatal(err)
	}
	pCfg["base_url"] = val
}

func runOllamaForm(pCfg config.ProviderConfig) {
	baseURL := pCfg["base_url"]
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	model := pCfg["model"]

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Ollama Base URL").
				Value(&baseURL),
			huh.NewInput().
				Title("Default Model").
				Description("e.g. llama3").
				Value(&model),
		),
	).Run()
	if err != nil {
		log.Fatal(err)
	}
	pCfg["base_url"] = baseURL
	pCfg["model"] = model
}

func runSAPAICoreForm(pCfg config.ProviderConfig) {
	clientID := pCfg["client_id"]
	clientSecret := pCfg["client_secret"]
	authURL := pCfg["auth_url"]
	baseURL := pCfg["base_url"]
	resourceGroup := pCfg["resource_group"]
	if resourceGroup == "" {
		resourceGroup = "default"
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Client ID").
				Value(&clientID),
			huh.NewInput().
				Title("Client Secret").
				EchoMode(huh.EchoModePassword).
				Value(&clientSecret),
			huh.NewInput().
				Title("Auth URL").
				Value(&authURL),
			huh.NewInput().
				Title("Base URL").
				Value(&baseURL),
			huh.NewInput().
				Title("Resource Group").
				Value(&resourceGroup),
		),
	).Run()
	if err != nil {
		log.Fatal(err)
	}

	pCfg["client_id"] = clientID
	pCfg["client_secret"] = clientSecret
	pCfg["auth_url"] = authURL
	pCfg["base_url"] = baseURL
	pCfg["resource_group"] = resourceGroup
}

func fetchAndSelectSAPModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	runSpinner("Connecting to SAP AI Core...")

	// Fetch models
	models, err := sap.ListModels(context.Background(),
		pCfg["client_id"],
		pCfg["client_secret"],
		pCfg["auth_url"],
		pCfg["base_url"],
		pCfg["resource_group"])

	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no running models found")
	}
	
	// Sort models for better UX
	sort.Strings(models)

	// Create Options dynamically
	var modelOptions []huh.Option[string]
	for _, m := range models {
		modelOptions = append(modelOptions, huh.NewOption(m, m))
	}

	var selectedModel string
	// Pre-select current default if it exists in the list
	if appCfg.General.DefaultModel != "" {
		for _, m := range models {
			if m == appCfg.General.DefaultModel {
				selectedModel = m
				break
			}
		}
	}

	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a model").
				Description("Type to filter list").
				Options(modelOptions...).
				Value(&selectedModel).
				Height(len(models) + 2), 
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectOpenRouterModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	runSpinner("Fetching models from OpenRouter...")

	models, err := openrouter.ListModels(pCfg["api_key"])
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	// Create Options dynamically
	var modelOptions []huh.Option[string]
	for _, m := range models {
		// Format: [Group] Name
		// We use the ID as the value
		label := fmt.Sprintf("[%s] %s", m.Group, m.DisplayName)
		modelOptions = append(modelOptions, huh.NewOption(label, m.ID))
	}

	var selectedModel string
	// Pre-select current default if it exists
	if appCfg.General.DefaultModel != "" {
		for _, m := range models {
			if m.ID == appCfg.General.DefaultModel {
				selectedModel = m.ID
				break
			}
		}
	}

	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a model").
				Description("Type to filter list").
				Options(modelOptions...).
				Value(&selectedModel).
				Height(len(models) + 2), 
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

// UI Helpers

func runSpinner(msg string) {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	
	// We can't easily run the bubbletea program and block here without more complex setup
	// For now, let's just print a message that looks nice
	fmt.Printf("%s %s\n", s.Style.Render("•"), msg)
	
	// In a real CLI app we might want to use tea.NewProgram to run the spinner properly
	// but since we're about to make a blocking network call, we can't update the spinner 
	// unless we run the network call in a goroutine.
	// For simplicity in this setup script, we just print the message.
}

func printSuccess(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")). // Green
		Bold(true).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42"))
	
	fmt.Println(style.Render("✓ " + msg))
}
