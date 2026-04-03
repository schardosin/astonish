package astonish

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/provider/anthropic"
	"github.com/schardosin/astonish/pkg/provider/google"
	"github.com/schardosin/astonish/pkg/provider/groq"
	"github.com/schardosin/astonish/pkg/provider/litellm"
	"github.com/schardosin/astonish/pkg/provider/lmstudio"
	"github.com/schardosin/astonish/pkg/provider/ollama"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"github.com/schardosin/astonish/pkg/provider/openai_compat"
	"github.com/schardosin/astonish/pkg/provider/openrouter"
	"github.com/schardosin/astonish/pkg/provider/poe"
	"github.com/schardosin/astonish/pkg/provider/sap"
	"github.com/schardosin/astonish/pkg/provider/xai"
	"github.com/schardosin/astonish/pkg/sandbox"
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

	// Build list of existing providers with their types
	var existingOptions []huh.Option[string]
	existingProviderTypes := make(map[string]string)

	for name, pCfg := range cfg.Providers {
		providerType := config.GetProviderType(name, pCfg)
		displayName := provider.GetProviderDisplayName(providerType)
		if displayName == "" {
			displayName = name
		}
		label := fmt.Sprintf("%s (%s)", name, displayName)
		existingOptions = append(existingOptions, huh.NewOption(label, name))
		existingProviderTypes[name] = providerType
	}

	// Sort existing options by name
	sort.Slice(existingOptions, func(i, j int) bool {
		return existingOptions[i].Value < existingOptions[j].Value
	})

	// Add "Add new provider" option at the end
	const addNewValue = "__add_new__"
	existingOptions = append(existingOptions, huh.NewOption("+ Add new provider", addNewValue))

	// --- STEP 1: Select existing or add new ---
	var selectedInstance string
	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a provider instance to configure").
				Description("Choose an existing provider or add a new one").
				Options(existingOptions...).
				Value(&selectedInstance),
		),
	).Run()
	if err != nil {
		return err
	}

	var isNewProvider bool
	var selectedProviderID string

	if selectedInstance == addNewValue {
		isNewProvider = true
		// --- STEP 2: Provider Type (for new provider) ---
		selectedProviderID = selectProviderType()
		// Initialize provider config for new provider
		cfg.Providers[addNewValue] = make(config.ProviderConfig)
	} else {
		// Editing existing provider
		selectedProviderID = existingProviderTypes[selectedInstance]
		if selectedProviderID == "" {
			// Fallback: try to infer from instance name
			selectedProviderID = selectedInstance
		}
		// Initialize provider config if nil
		if cfg.Providers[selectedInstance] == nil {
			cfg.Providers[selectedInstance] = make(config.ProviderConfig)
		}
	}

	pCfg := cfg.Providers[selectedInstance]

	// --- STEP 3: Configuration Form ---
	switch selectedProviderID {
	case "anthropic":
		runAPIKeyForm("Anthropic API Key", "api_key", pCfg)
		if err := fetchAndSelectAnthropicModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select Anthropic models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "gemini":
		runAPIKeyForm("Google API Key", "api_key", pCfg)
		if err := fetchAndSelectGoogleModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select Google models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "groq":
		runAPIKeyForm("Groq API Key", "api_key", pCfg)
		if err := fetchAndSelectGroqModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select Groq models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "lm_studio":
		runBaseURLForm("LM Studio Base URL", "http://localhost:1234/v1", pCfg)
		if err := fetchAndSelectLMStudioModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select LM Studio models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "litellm":
		runAPIKeyForm("LiteLLM API Key", "api_key", pCfg)
		runBaseURLForm("LiteLLM Base URL", "http://localhost:4000/v1", pCfg)
		if err := fetchAndSelectLiteLLMModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select LiteLLM models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "ollama":
		runOllamaForm(pCfg)
		if err := fetchAndSelectOllamaModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select Ollama models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "openai":
		runAPIKeyForm("OpenAI API Key", "api_key", pCfg)
		if err := fetchAndSelectOpenAIModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select OpenAI models %v\n", err)
		} else {
			goto SaveConfig
		}
	case "xai":
		runAPIKeyForm("xAI API Key", "api_key", pCfg)
		if err := fetchAndSelectXAIModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select xAI models %v\n", err)
		} else {
			goto SaveConfig
		}
	case "openai_compat":
		runAPIKeyForm("API Key", "api_key", pCfg)
		runBaseURLForm("Base URL", "https://api.openai.com/v1", pCfg)
		if err := fetchAndSelectOpenAICompatModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "openrouter":
		runAPIKeyForm("OpenRouter API Key", "api_key", pCfg)
		if err := fetchAndSelectOpenRouterModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select OpenRouter models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "poe":
		runAPIKeyForm("Poe API Key", "api_key", pCfg)
		if err := fetchAndSelectPoeModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select Poe models: %v\n", err)
		} else {
			goto SaveConfig
		}
	case "sap_ai_core":
		runSAPAICoreForm(pCfg)
		// Special handling for SAP AI Core model selection
		if err := fetchAndSelectSAPModel(pCfg, cfg); err != nil {
			if isUserAborted(err) {
				fmt.Println("Setup aborted by user; no changes were saved.")
				return nil
			}
			fmt.Printf("Warning: Failed to fetch/select SAP models: %v\n", err)
		} else {
			// Skip generic model selection if we did it specifically for SAP
			goto SaveConfig
		}
	default:
		// Unknown provider type - ask for instance name to continue
		selectedProviderID = selectProviderType()
	}

	// --- STEP 4: Default Model Selection (Generic) ---
	// Only ask if not already handled (like in SAP AI Core)
	{
		var defaultModel string = cfg.General.DefaultModel
		clearScreen()
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
	// For new providers, ask for instance name at the end
	if isNewProvider {
		var instanceName string
		clearScreen()
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Provider Instance Name").
					Description("Unique name for this provider instance").
					Value(&instanceName),
			),
		).Run()
		if err != nil {
			return err
		}
		instanceName = strings.TrimSpace(instanceName)
		if instanceName == "" {
			return fmt.Errorf("instance name is required")
		}

		// Migrate config from temp key to final name
		if instanceName != selectedInstance {
			cfg.Providers[instanceName] = pCfg
			cfg.Providers[instanceName]["type"] = selectedProviderID
			delete(cfg.Providers, selectedInstance)
			selectedInstance = instanceName
		} else {
			cfg.Providers[instanceName]["type"] = selectedProviderID
		}
	} else if cfg.Providers[selectedInstance] != nil {
		// Ensure type is set for existing providers
		if cfg.Providers[selectedInstance]["type"] == "" {
			cfg.Providers[selectedInstance]["type"] = selectedProviderID
		}
	}

	// Set as default provider
	cfg.General.DefaultProvider = selectedInstance

	// Save secrets to encrypted credential store (instead of plaintext config.yaml)
	if err := saveProviderSecretsToStore(selectedInstance, selectedProviderID, pCfg); err != nil {
		fmt.Printf("Warning: Failed to save secrets to credential store: %v\n", err)
		fmt.Println("Secrets will be saved in config.yaml as fallback.")
	}

	// Save config (secrets have been scrubbed from pCfg by saveProviderSecretsToStore)
	if err := config.SaveAppConfig(cfg); err != nil {
		return fmt.Errorf("error saving config: %w", err)
	}

	displayName := provider.GetProviderDisplayName(selectedProviderID)
	if displayName == "" {
		displayName = selectedProviderID
	}
	printSuccess(fmt.Sprintf("%s (%s) configured successfully!", selectedInstance, displayName))

	// --- Web Tool Setup ---
	if err := handleWebToolSetup(); err != nil {
		if !isUserAborted(err) {
			fmt.Printf("Warning: Web tool setup failed: %v\n", err)
		}
	}

	// --- Browser Setup ---
	if err := handleBrowserSetup(); err != nil {
		if !isUserAborted(err) {
			fmt.Printf("Warning: Browser setup failed: %v\n", err)
		}
	}

	// --- Sandbox Setup ---
	if err := handleSandboxSetup(); err != nil {
		if !isUserAborted(err) {
			fmt.Printf("Warning: Sandbox setup failed: %v\n", err)
		}
	}

	return nil
}

// selectProviderType shows the provider type selection and returns the selected type
func selectProviderType() string {
	var selectedProviderID string

	providerTypeOptions := []huh.Option[string]{
		huh.NewOption(provider.GetProviderDisplayName("anthropic"), "anthropic"),
		huh.NewOption(provider.GetProviderDisplayName("gemini"), "gemini"),
		huh.NewOption(provider.GetProviderDisplayName("groq"), "groq"),
		huh.NewOption(provider.GetProviderDisplayName("litellm"), "litellm"),
		huh.NewOption(provider.GetProviderDisplayName("lm_studio"), "lm_studio"),
		huh.NewOption(provider.GetProviderDisplayName("ollama"), "ollama"),
		huh.NewOption(provider.GetProviderDisplayName("openai"), "openai"),
		huh.NewOption(provider.GetProviderDisplayName("openai_compat"), "openai_compat"),
		huh.NewOption(provider.GetProviderDisplayName("openrouter"), "openrouter"),
		huh.NewOption(provider.GetProviderDisplayName("poe"), "poe"),
		huh.NewOption(provider.GetProviderDisplayName("sap_ai_core"), "sap_ai_core"),
		huh.NewOption(provider.GetProviderDisplayName("xai"), "xai"),
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select provider type").
				Options(providerTypeOptions...).
				Value(&selectedProviderID),
		),
	).Run()
	if err != nil {
		return ""
	}

	return selectedProviderID
}

// Helper functions for forms

func runAPIKeyForm(title string, key string, pCfg config.ProviderConfig) {
	clearScreen()
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
		slog.Error("form input failed", "error", err)
		os.Exit(1)
	}
	pCfg[key] = strings.TrimSpace(val)
}

func runBaseURLForm(title string, defaultVal string, pCfg config.ProviderConfig) {
	clearScreen()
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
		slog.Error("form input failed", "error", err)
		os.Exit(1)
	}
	pCfg["base_url"] = val
}

func runOllamaForm(pCfg config.ProviderConfig) {
	clearScreen()
	baseURL := pCfg["base_url"]
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Ollama Base URL").
				Value(&baseURL),
		),
	).Run()
	if err != nil {
		slog.Error("form input failed", "error", err)
		os.Exit(1)
	}
	pCfg["base_url"] = baseURL
}

func runSAPAICoreForm(pCfg config.ProviderConfig) {
	clearScreen()
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
		slog.Error("form input failed", "error", err)
		os.Exit(1)
	}

	pCfg["client_id"] = clientID
	pCfg["client_secret"] = clientSecret
	pCfg["auth_url"] = authURL
	pCfg["base_url"] = baseURL
	pCfg["resource_group"] = resourceGroup
}

// saveProviderSecretsToStore saves sensitive fields from a provider config
// into the encrypted credential store and scrubs them from the pCfg map.
// Non-secret fields (base_url, resource_group, type, model) are left in pCfg.
func saveProviderSecretsToStore(instanceName, providerType string, pCfg config.ProviderConfig) error {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("get config dir: %w", err)
	}

	store, err := credentials.Open(configDir)
	if err != nil {
		return fmt.Errorf("open credential store: %w", err)
	}

	// Determine which keys are secret for this provider type
	secretKeys := []string{"api_key"} // default
	switch providerType {
	case "sap_ai_core":
		secretKeys = []string{"client_id", "client_secret", "auth_url"}
	case "ollama", "lm_studio":
		secretKeys = nil // no secrets for local providers
	}

	secrets := make(map[string]string)
	for _, key := range secretKeys {
		val, ok := pCfg[key]
		if !ok || val == "" {
			continue
		}
		storeKey := "provider." + instanceName + "." + key
		secrets[storeKey] = val
	}

	if len(secrets) == 0 {
		return nil
	}

	if err := store.SetSecretBatch(secrets); err != nil {
		return fmt.Errorf("save secrets: %w", err)
	}

	// Scrub secrets from pCfg so they don't end up in config.yaml
	for _, key := range secretKeys {
		delete(pCfg, key)
	}

	return nil
}

func fetchAndSelectSAPModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	runSpinner("Connecting to SAP AI Core...")

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

	sort.Strings(models)

	var modelOptions []huh.Option[string]
	for _, m := range models {
		modelOptions = append(modelOptions, huh.NewOption(m, m))
	}

	var selectedModel string
	if appCfg.General.DefaultModel != "" {
		for _, m := range models {
			if m == appCfg.General.DefaultModel {
				selectedModel = m
				break
			}
		}
	}

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a model").
				Description("Type to filter list").
				Options(modelOptions...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

// fetchAndSelectGoogleModel fetches available models from Google GenAI and prompts user to select one
func fetchAndSelectGoogleModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	if apiKey == "" {
		return fmt.Errorf("API key is missing")
	}

	runSpinner("Fetching models from Google GenAI...")

	models, err := google.ListModels(context.Background(), apiKey)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	// Create options for huh.Select
	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Google GenAI Model").
				Options(options...).
				Value(&selectedModel),
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
		label := fmt.Sprintf("[%s] %s", m.Group, m.DisplayName)
		modelOptions = append(modelOptions, huh.NewOption(label, m.ID))
	}

	var selectedModel string
	if appCfg.General.DefaultModel != "" {
		for _, m := range models {
			if m.ID == appCfg.General.DefaultModel {
				selectedModel = m.ID
				break
			}
		}
	}

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a model").
				Description("Type to filter list").
				Options(modelOptions...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectOllamaModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	baseURL := pCfg["base_url"]
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	runSpinner("Fetching models from Ollama...")

	models, err := ollama.ListModels(context.Background(), baseURL)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an Ollama Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectLMStudioModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	baseURL := pCfg["base_url"]
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}

	runSpinner("Fetching models from LM Studio...")

	models, err := lmstudio.ListModels(context.Background(), baseURL)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an LM Studio Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectGroqModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	if apiKey == "" {
		return fmt.Errorf("API key required")
	}

	runSpinner("Fetching models from Groq...")

	models, err := groq.ListModels(context.Background(), apiKey)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Groq Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func isUserAborted(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "user aborted") || strings.Contains(msg, "aborted") || strings.Contains(msg, "interrupt")
}

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

// clearScreen resets the terminal so each wizard page starts fresh.
func clearScreen() {
	fmt.Print("\033[2J\033[H")
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

func fetchAndSelectOpenAIModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	if apiKey == "" {
		return fmt.Errorf("API key required")
	}

	runSpinner("Fetching models from OpenAI...")

	models, err := openai_provider.ListModels(context.Background(), apiKey)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an OpenAI Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectAnthropicModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	if apiKey == "" {
		return fmt.Errorf("API key required")
	}

	runSpinner("Fetching models from Anthropic...")

	models, err := anthropic.ListModels(context.Background(), apiKey)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	// Create options for huh.Select
	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an Anthropic Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectXAIModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	if apiKey == "" {
		return fmt.Errorf("API key required")
	}

	runSpinner("Fetching models from xAI...")

	models, err := xai.ListModels(context.Background(), apiKey)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select an xAI Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectPoeModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	runSpinner("Fetching models from Poe...")

	ctx := context.Background()
	models, err := poe.ListModels(ctx, pCfg["api_key"])
	if err != nil {
		return fmt.Errorf("failed to fetch models: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available from Poe")
	}

	options := make([]huh.Option[string], len(models))
	for i, m := range models {
		options[i] = huh.NewOption(m, m)
	}

	var selectedModel string
	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Poe Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectLiteLLMModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	baseURL := pCfg["base_url"]
	if baseURL == "" {
		baseURL = "http://localhost:4000/v1"
	}

	runSpinner("Fetching models from LiteLLM...")

	models, err := litellm.ListModels(context.Background(), apiKey, baseURL)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a LiteLLM Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

func fetchAndSelectOpenAICompatModel(pCfg config.ProviderConfig, appCfg *config.AppConfig) error {
	apiKey := pCfg["api_key"]
	baseURL := pCfg["base_url"]
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	runSpinner("Fetching models...")

	models, err := openai_compat.ListModels(context.Background(), apiKey, baseURL)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		return fmt.Errorf("no models found")
	}

	var options []huh.Option[string]
	for _, m := range models {
		options = append(options, huh.NewOption(m, m))
	}

	var selectedModel string

	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Model").
				Options(options...).
				Value(&selectedModel),
		),
	).Run()

	if err != nil {
		return err
	}

	appCfg.General.DefaultModel = selectedModel
	return nil
}

// handleWebToolSetup prompts the user to configure a standard web tool MCP server.
func handleWebToolSetup() error {
	// Check if any web tool is already configured
	appCfg, err := config.LoadAppConfig()
	if err == nil && appCfg.General.WebSearchTool != "" {
		var reconfigure bool
		clearScreen()
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Web search is already configured").
					Description("Would you like to reconfigure web search tools?").
					Value(&reconfigure),
			),
		).Run()
		if err != nil || !reconfigure {
			return err
		}
	}

	// Ask if user wants to set up web tools
	var setupWeb bool
	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Configure Web Search Tools?").
				Description("Web tools let the AI search the web and extract content from URLs").
				Affirmative("Yes").
				Negative("Skip").
				Value(&setupWeb),
		),
	).Run()
	if err != nil {
		return err
	}
	if !setupWeb {
		return nil
	}

	// Show available standard servers
	servers := config.GetStandardServers()
	var serverOptions []huh.Option[string]
	for _, srv := range servers {
		label := srv.DisplayName
		if srv.IsDefault {
			label += " (recommended)"
		}
		if config.IsStandardServerInstalled(srv.ID) {
			label += " [installed]"
		}
		serverOptions = append(serverOptions, huh.NewOption(label, srv.ID))
	}

	var selectedServer string
	clearScreen()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a web tool provider").
				Description("Choose which service to use for web search and content extraction").
				Options(serverOptions...).
				Value(&selectedServer),
		),
	).Run()
	if err != nil {
		return err
	}

	srv := config.GetStandardServer(selectedServer)
	if srv == nil {
		return fmt.Errorf("unknown server: %s", selectedServer)
	}

	// Collect env var values
	envValues := make(map[string]string)
	for _, ev := range srv.EnvVars {
		var val string
		clearScreen()
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(ev.Name).
					Description(ev.Description).
					EchoMode(huh.EchoModePassword).
					Value(&val),
			),
		).Run()
		if err != nil {
			return err
		}
		val = strings.TrimSpace(val)
		if val == "" && ev.Required {
			return nil
		}
		envValues[ev.Name] = val
	}

	// Install the server
	clearScreen()
	runSpinner(fmt.Sprintf("Configuring %s...", srv.DisplayName))

	// Save API key to credential store
	storeKeyInConfig := true // fallback to config if store fails
	configDir, err := config.GetConfigDir()
	if err != nil {
		slog.Warn("failed to get config directory", "error", err)
	}
	if configDir != "" {
		if store, storeErr := credentials.Open(configDir); storeErr == nil {
			for _, ev := range srv.EnvVars {
				if val, ok := envValues[ev.Name]; ok && val != "" {
					storeKey := "web_servers." + selectedServer + ".api_key"
					if setErr := store.SetSecret(storeKey, val); setErr == nil {
						storeKeyInConfig = false
					}
					break
				}
			}
		}
	}

	if err := config.InstallStandardServer(selectedServer, envValues, storeKeyInConfig); err != nil {
		return fmt.Errorf("failed to configure %s: %w", srv.DisplayName, err)
	}

	clearScreen()
	msg := fmt.Sprintf("%s configured! Web search is now available.", srv.DisplayName)
	if srv.WebExtractTool != "" {
		msg = fmt.Sprintf("%s configured! Web search and content extraction are now available.", srv.DisplayName)
	}
	printSuccess(msg)

	return nil
}

// handleBrowserSetup is defined in browser_setup.go — it runs the interactive
// browser engine configuration flow (default Chromium, CloakBrowser, or custom).

// handleSandboxSetup initializes the sandbox (container isolation) as part of
// the main setup wizard. If Incus is not available, it guides the user to
// install it and warns about the security risk of running without sandbox.
func handleSandboxSetup() error {
	for {
		platform, reason := sandbox.DetectPlatformReason()

		if platform == sandbox.PlatformUnsupported {
			// If Incus is installed but we lack socket permissions, auto-escalate
			// via sudo rather than showing the "not available" screen.
			lowReason := strings.ToLower(reason)
			if sandbox.NeedsEscalation() && (strings.Contains(lowReason, "permission denied") || strings.Contains(lowReason, "not accessible")) {
				return sandbox.Escalate()
			}

			// Build description with install instructions and reason
			desc := "Sandbox runs AI tools inside isolated Linux containers,\n" +
				"preventing them from accessing your host system directly.\n\n"
			if reason != "" {
				desc += reason + "\n\n"
			}
			desc += "To enable sandbox:\n" +
				"  Linux:         sudo apt install incus && sudo incus admin init\n" +
				"  macOS/Windows: Install Docker Desktop\n\n" +
				"Docs: https://linuxcontainers.org/incus/docs/main/installing/"

			var action string
			clearScreen()
			err := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Sandbox Setup — Container runtime not available").
						Description(desc).
						Options(
							huh.NewOption("Continue — I've installed the runtime", "retry"),
							huh.NewOption("Skip — proceed without sandbox", "skip"),
						).
						Value(&action),
				),
			).Run()
			if err != nil {
				return err
			}

			if action == "retry" {
				continue
			}

			// User chose to skip — show security warning
			var acceptRisk bool
			clearScreen()
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Continue without sandbox?").
						Description("Without sandbox, AI tools will execute directly on your host\n" +
							"system with full access to your files, network, and system resources.\n" +
							"This is a security risk.").
						Affirmative("Yes, I accept the risk").
						Negative("Go back").
						Value(&acceptRisk),
				),
			).Run()
			if err != nil {
				return err
			}

			if !acceptRisk {
				continue
			}

			return nil
		}

		// Container runtime is available — set up the sandbox

		// On Linux, sandbox operations (overlay mounts, UID shifting, Incus
		// socket access) require root. Re-exec via sudo if needed.
		if sandbox.NeedsEscalation() {
			return sandbox.Escalate()
		}

		// On Docker+Incus (macOS/Windows), ensure the Docker container is running first
		if platform == sandbox.PlatformDockerIncus {
			clearScreen()
			fmt.Println("Setting up Docker+Incus sandbox runtime...")
			fmt.Println("This will pull the Incus Docker image and create a container.")
			fmt.Println("(This may take a few minutes on first run.)")
			fmt.Println()

			if err := sandbox.EnsureIncusDockerContainer(); err != nil {
				return fmt.Errorf("failed to set up Docker+Incus: %w", err)
			}
			fmt.Println("Docker+Incus runtime ready.")
		}

		sandbox.SetActivePlatform(platform)
		if appCfg, cfgErr := config.LoadAppConfig(); cfgErr == nil && appCfg != nil {
			sandbox.SetSandboxConfig(&appCfg.Sandbox)
		}

		// Detect nested LXC: unprivileged containers cannot run inside
		// another LXC container (mounting /proc in double-nested user
		// namespaces is blocked by the outer host). Ask the user whether
		// to enable privileged mode, which the outer LXC still isolates.
		if sandbox.IsInsideLXC() && !sandbox.IsPrivileged() {
			var action string
			clearScreen()
			err := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Sandbox — Nested LXC environment detected").
						Description(
							"This host is itself an LXC container. Unprivileged sandbox\n"+
								"containers cannot run here because mounting /proc in a\n"+
								"double-nested user namespace is not permitted by the outer host.\n\n"+
								"Privileged mode runs containers as root inside the sandbox.\n"+
								"The outer LXC container still provides the isolation boundary,\n"+
								"but a container escape would give access to this LXC host.\n\n"+
								"You can change this later in your config:\n"+
								"  sandbox:\n"+
								"    privileged: true",
						).
						Options(
							huh.NewOption("Enable privileged mode", "enable"),
							huh.NewOption("Skip sandbox setup", "skip"),
						).
						Value(&action),
				),
			).Run()
			if err != nil {
				return err
			}

			if action == "skip" {
				return nil
			}

			// User accepted — persist privileged: true in config
			cfg, cfgErr := config.LoadAppConfig()
			if cfgErr != nil {
				return fmt.Errorf("failed to load config: %w", cfgErr)
			}
			priv := true
			cfg.Sandbox.Privileged = &priv
			if err := config.SaveAppConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			sandbox.SetSandboxConfig(&cfg.Sandbox)
			fmt.Println("Privileged mode enabled in config.")
		}

		client, err := sandbox.Connect(platform)
		if err != nil {
			return fmt.Errorf("failed to connect to Incus: %w", err)
		}

		// Check if base template already exists
		containerName := sandbox.TemplateName(sandbox.BaseTemplate)
		if client.InstanceExists(containerName) {
			return nil
		}

		registry, err := sandbox.NewTemplateRegistry()
		if err != nil {
			return err
		}

		opts := promptOptionalTools()

		if err := sandbox.InitBaseTemplate(client, registry, opts); err != nil {
			return fmt.Errorf("sandbox setup: %w", err)
		}

		clearScreen()
		printSuccess("Sandbox initialized! AI tools will run inside isolated containers.")
		return nil
	}
}
