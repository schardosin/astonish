package astonish

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
)

func handleCredentialCommand(args []string) error {
	if len(args) == 0 {
		printCredentialUsage()
		return fmt.Errorf("no credential subcommand provided")
	}

	switch args[0] {
	case "list", "ls":
		return handleCredentialList()
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish credential add <name>")
		}
		return handleCredentialAdd(strings.Join(args[1:], " "))
	case "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish credential remove <name>")
		}
		return handleCredentialRemove(strings.Join(args[1:], " "))
	case "test":
		if len(args) < 2 {
			return fmt.Errorf("usage: astonish credential test <name>")
		}
		return handleCredentialTest(strings.Join(args[1:], " "))
	default:
		printCredentialUsage()
		return fmt.Errorf("unknown credential subcommand: %s", args[0])
	}
}

func printCredentialUsage() {
	fmt.Println("usage: astonish credential {list,add,remove,test}")
	fmt.Println("")
	fmt.Println("Manage the encrypted credential store.")
	fmt.Println("")
	fmt.Println("subcommands:")
	fmt.Println("  list (ls)          List stored credentials and secrets (no values shown)")
	fmt.Println("  add <name>         Add a credential interactively")
	fmt.Println("  remove (rm) <name> Remove a credential")
	fmt.Println("  test <name>        Test a credential (OAuth: token flow, others: config check)")
}

func handleCredentialList() error {
	store, err := openCredentialStore()
	if err != nil {
		return err
	}

	creds := store.List()
	secrets := store.ListSecrets()
	sort.Strings(secrets)

	if len(creds) == 0 && len(secrets) == 0 {
		fmt.Println("No stored credentials or secrets.")
		fmt.Println("\nAdd credentials via CLI or chat:")
		fmt.Println("  astonish credential add <name>")
		fmt.Println("  Chat: \"Save my API key sk-... as 'my-api'\"")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	if len(creds) > 0 {
		fmt.Println("HTTP Credentials:")
		fmt.Fprintf(w, "  NAME\tTYPE\n")
		fmt.Fprintf(w, "  ----\t----\n")
		for name, credType := range creds {
			fmt.Fprintf(w, "  %s\t%s\n", name, credType)
		}
		w.Flush()
		fmt.Println()
	}

	if len(secrets) > 0 {
		fmt.Println("Secrets (provider keys, tokens, etc.):")
		for _, key := range secrets {
			fmt.Printf("  %s\n", key)
		}
		fmt.Println()
	}

	fmt.Printf("%d HTTP credential(s), %d secret(s)\n", len(creds), len(secrets))
	return nil
}

func handleCredentialAdd(name string) error {
	store, err := openCredentialStore()
	if err != nil {
		return err
	}

	// Check if credential already exists
	if existing := store.Get(name); existing != nil {
		var confirm bool
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Credential %q already exists. Overwrite?", name)).
					Value(&confirm),
			),
		).Run()
		if err != nil {
			return err
		}
		if !confirm {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Select credential type
	var credType string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Credential type").
				Options(
					huh.NewOption("API Key (custom header + value)", "api_key"),
					huh.NewOption("Bearer Token (Authorization: Bearer ...)", "bearer"),
					huh.NewOption("Basic Auth (username + password)", "basic"),
					huh.NewOption("OAuth Client Credentials (auto token refresh)", "oauth_client_credentials"),
				).
				Value(&credType),
		),
	).Run()
	if err != nil {
		return err
	}

	var cred *credentials.Credential

	switch credentials.CredentialType(credType) {
	case credentials.CredAPIKey:
		cred, err = collectAPIKeyCred()
	case credentials.CredBearer:
		cred, err = collectBearerCred()
	case credentials.CredBasic:
		cred, err = collectBasicCred()
	case credentials.CredOAuthClientCreds:
		cred, err = collectOAuthCred()
	default:
		return fmt.Errorf("unknown credential type: %s", credType)
	}
	if err != nil {
		return err
	}

	if err := store.Set(name, cred); err != nil {
		return fmt.Errorf("failed to save credential: %w", err)
	}

	fmt.Printf("Credential %q saved (%s)\n", name, credType)
	return nil
}

func handleCredentialRemove(name string) error {
	store, err := openCredentialStore()
	if err != nil {
		return err
	}

	if store.Get(name) == nil {
		return fmt.Errorf("credential %q not found", name)
	}

	if err := store.Remove(name); err != nil {
		return fmt.Errorf("failed to remove: %w", err)
	}

	fmt.Printf("Credential %q removed\n", name)
	return nil
}

func handleCredentialTest(name string) error {
	store, err := openCredentialStore()
	if err != nil {
		return err
	}

	cred := store.Get(name)
	if cred == nil {
		return fmt.Errorf("credential %q not found", name)
	}

	switch cred.Type {
	case credentials.CredOAuthClientCreds:
		fmt.Printf("Testing OAuth credential %q...\n", name)
		headerKey, headerValue, err := store.Resolve(name)
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			return nil
		}
		// Show success without revealing the token
		tokenLen := len(headerValue) - len("Bearer ")
		fmt.Printf("OK: acquired token (%d chars), header: %s\n", tokenLen, headerKey)

	case credentials.CredAPIKey:
		fmt.Printf("Credential %q configured (api_key, header: %s)\n", name, cred.Header)
		fmt.Println("Use http_request tool with credential parameter to test connectivity.")

	case credentials.CredBearer:
		fmt.Printf("Credential %q configured (bearer, token: %d chars)\n", name, len(cred.Token))
		fmt.Println("Use http_request tool with credential parameter to test connectivity.")

	case credentials.CredBasic:
		fmt.Printf("Credential %q configured (basic, user: %s)\n", name, cred.Username)
		fmt.Println("Use http_request tool with credential parameter to test connectivity.")

	default:
		fmt.Printf("Credential %q configured (type: %s)\n", name, cred.Type)
	}

	return nil
}

// --- Interactive form helpers ---

func collectAPIKeyCred() (*credentials.Credential, error) {
	header := "Authorization"
	value := ""

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Header name").
				Description("e.g., X-API-Key, Authorization").
				Value(&header),
			huh.NewInput().
				Title("Key value").
				EchoMode(huh.EchoModePassword).
				Value(&value),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	if value == "" {
		return nil, fmt.Errorf("key value is required")
	}

	return &credentials.Credential{
		Type:   credentials.CredAPIKey,
		Header: header,
		Value:  value,
	}, nil
}

func collectBearerCred() (*credentials.Credential, error) {
	token := ""

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Bearer token").
				EchoMode(huh.EchoModePassword).
				Value(&token),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	if token == "" {
		return nil, fmt.Errorf("token is required")
	}

	return &credentials.Credential{
		Type:  credentials.CredBearer,
		Token: token,
	}, nil
}

func collectBasicCred() (*credentials.Credential, error) {
	username := ""
	password := ""

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Username").
				Value(&username),
			huh.NewInput().
				Title("Password").
				EchoMode(huh.EchoModePassword).
				Value(&password),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	return &credentials.Credential{
		Type:     credentials.CredBasic,
		Username: username,
		Password: password,
	}, nil
}

func collectOAuthCred() (*credentials.Credential, error) {
	authURL := ""
	clientID := ""
	clientSecret := ""
	scope := ""

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Auth URL").
				Description("Token endpoint (e.g., https://auth.example.com/oauth/token)").
				Value(&authURL),
			huh.NewInput().
				Title("Client ID").
				Value(&clientID),
			huh.NewInput().
				Title("Client Secret").
				EchoMode(huh.EchoModePassword).
				Value(&clientSecret),
			huh.NewInput().
				Title("Scope (optional)").
				Value(&scope),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	if authURL == "" {
		return nil, fmt.Errorf("auth URL is required")
	}
	if clientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("client secret is required")
	}

	return &credentials.Credential{
		Type:         credentials.CredOAuthClientCreds,
		AuthURL:      authURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scope:        scope,
	}, nil
}

// openCredentialStore opens the credential store from the default config directory.
func openCredentialStore() (*credentials.Store, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}
	store, err := credentials.Open(configDir)
	if err != nil {
		return nil, fmt.Errorf("credential store: %w", err)
	}
	return store, nil
}
