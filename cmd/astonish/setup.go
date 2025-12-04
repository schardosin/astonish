package astonish

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider/sap"
)

func handleSetupCommand() error {
	var modelInput string
	reader := bufio.NewReader(os.Stdin)
	cfg, err := config.LoadAppConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return err
	}

	fmt.Println("Select a provider to configure:")
	
	// Define providers with display names and internal IDs
	type providerOption struct {
		DisplayName string
		ID          string
	}
	
	options := []providerOption{
		{"Anthropic", "anthropic"},
		{"Google GenAI", "gemini"},
		{"Groq", "groq"},
		{"LM Studio", "lm_studio"},
		{"Ollama", "ollama"},
		{"OpenAI", "openai"},
		{"Openrouter", "openrouter"},
		{"SAP AI Core", "sap_ai_core"},
	}

	for i, opt := range options {
		fmt.Printf("%d. %s\n", i+1, opt.DisplayName)
	}

	fmt.Print("Enter the number of your choice: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var selectedProvider string
	var selectedDisplayName string
	
	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err == nil && idx > 0 && idx <= len(options) {
		selectedProvider = options[idx-1].ID
		selectedDisplayName = options[idx-1].DisplayName
	} else {
		return fmt.Errorf("invalid selection")
	}

	fmt.Printf("Configuring %s...\n", selectedDisplayName)

	if cfg.Providers[selectedProvider] == nil {
		cfg.Providers[selectedProvider] = make(config.ProviderConfig)
	}

	switch selectedProvider {
	case "anthropic":
		promptAndSet(reader, cfg.Providers[selectedProvider], "api_key", "Enter Anthropic API Key")
	case "gemini":
		promptAndSet(reader, cfg.Providers[selectedProvider], "api_key", "Enter Google API Key")
	case "groq":
		promptAndSet(reader, cfg.Providers[selectedProvider], "api_key", "Enter Groq API Key")
	case "lm_studio":
		promptAndSet(reader, cfg.Providers[selectedProvider], "base_url", "Enter LM Studio Base URL [http://localhost:1234/v1]")
	case "ollama":
		promptAndSet(reader, cfg.Providers[selectedProvider], "base_url", "Enter Ollama Base URL [http://localhost:11434/v1]")
		promptAndSet(reader, cfg.Providers[selectedProvider], "model", "Enter Default Model (e.g. llama3)")
	case "openai":
		promptAndSet(reader, cfg.Providers[selectedProvider], "api_key", "Enter OpenAI API Key")
	case "openrouter":
		promptAndSet(reader, cfg.Providers[selectedProvider], "api_key", "Enter OpenRouter API Key")
	case "sap_ai_core":
		promptAndSet(reader, cfg.Providers[selectedProvider], "client_id", "Enter Client ID")
		promptAndSet(reader, cfg.Providers[selectedProvider], "client_secret", "Enter Client Secret")
		promptAndSet(reader, cfg.Providers[selectedProvider], "auth_url", "Enter Auth URL")
		promptAndSet(reader, cfg.Providers[selectedProvider], "base_url", "Enter Base URL")
		promptAndSet(reader, cfg.Providers[selectedProvider], "resource_group", "Enter Resource Group")

		// Fetch and list models
		fmt.Println("Fetching available models from SAP AI Core...")
		pCfg := cfg.Providers[selectedProvider]
		models, err := sap.ListModels(context.Background(),
			pCfg["client_id"],
			pCfg["client_secret"],
			pCfg["auth_url"],
			pCfg["base_url"],
			pCfg["resource_group"])

		if err != nil {
			fmt.Printf("Warning: Failed to fetch models: %v\n", err)
		} else if len(models) > 0 {
			fmt.Println("Available models:")
			for i, m := range models {
				fmt.Printf("%d. %s\n", i+1, m)
			}
			fmt.Print("Select a model number (or press Enter to skip): ")
			modelChoice, _ := reader.ReadString('\n')
			modelChoice = strings.TrimSpace(modelChoice)
			if modelChoice != "" {
				var idx int
				if _, err := fmt.Sscanf(modelChoice, "%d", &idx); err == nil && idx > 0 && idx <= len(models) {
					cfg.General.DefaultModel = models[idx-1]
					fmt.Printf("Selected model: %s\n", cfg.General.DefaultModel)
					// Skip the generic model prompt below
					goto SaveConfig
				} else {
					fmt.Println("Invalid selection, skipping model selection.")
				}
			}
		} else {
			fmt.Println("No running models found.")
		}
	}

	// Set as default
	cfg.General.DefaultProvider = selectedProvider
	fmt.Printf("Set %s as default provider.\n", selectedDisplayName)

	// Ask for default model
	fmt.Print("Enter default model (leave empty to keep current/default): ")
	modelInput, _ = reader.ReadString('\n')
	modelInput = strings.TrimSpace(modelInput)
	if modelInput != "" {
		cfg.General.DefaultModel = modelInput
	}

SaveConfig:
	if err := config.SaveAppConfig(cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return err
	}

	fmt.Println("Configuration saved successfully!")
	return nil
}

func promptAndSet(reader *bufio.Reader, providerConfig config.ProviderConfig, key string, prompt string) {
	current := providerConfig[key]
	fmt.Printf("%s [%s]: ", prompt, current)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" {
		providerConfig[key] = input
	}
}
