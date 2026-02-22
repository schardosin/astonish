package tools

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/credentials"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// credentialStoreVar holds the credential store reference.
// Set by the daemon or console launcher via SetCredentialStore.
var credentialStoreVar *credentials.Store

// SetCredentialStore registers the credential store for LLM tool access.
func SetCredentialStore(store *credentials.Store) {
	credentialStoreVar = store
}

// GetCredentialStore returns the current credential store (for redaction wiring).
func GetCredentialStore() *credentials.Store {
	return credentialStoreVar
}

// --- save_credential tool ---

type SaveCredentialArgs struct {
	Name         string `json:"name" jsonschema:"Short identifier for this credential (e.g., 'my-api', 'proxmox', 'saas-prod')"`
	Type         string `json:"type" jsonschema:"Credential type: 'api_key' (custom header+value), 'bearer' (Authorization: Bearer token), 'basic' (username+password), 'oauth_client_credentials' (auto-refreshing OAuth2)"`
	Header       string `json:"header,omitempty" jsonschema:"Header name for api_key type (e.g., 'X-API-Key', 'Authorization'). Required for api_key type."`
	Value        string `json:"value,omitempty" jsonschema:"The API key value. Required for api_key type."`
	Token        string `json:"token,omitempty" jsonschema:"The bearer token. Required for bearer type."`
	Username     string `json:"username,omitempty" jsonschema:"Username for basic auth. Required for basic type."`
	Password     string `json:"password,omitempty" jsonschema:"Password for basic auth. Required for basic type."`
	AuthURL      string `json:"auth_url,omitempty" jsonschema:"OAuth token endpoint URL. Required for oauth_client_credentials type."`
	ClientID     string `json:"client_id,omitempty" jsonschema:"OAuth client ID. Required for oauth_client_credentials type."`
	ClientSecret string `json:"client_secret,omitempty" jsonschema:"OAuth client secret. Required for oauth_client_credentials type."`
	Scope        string `json:"scope,omitempty" jsonschema:"OAuth scope (optional, for oauth_client_credentials type)."`
}

type SaveCredentialResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func saveCredential(_ tool.Context, args SaveCredentialArgs) (SaveCredentialResult, error) {
	if credentialStoreVar == nil {
		return SaveCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return SaveCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	credType := credentials.CredentialType(args.Type)
	var cred *credentials.Credential

	switch credType {
	case credentials.CredAPIKey:
		if args.Header == "" {
			return SaveCredentialResult{Status: "error", Message: "Header is required for api_key type"}, nil
		}
		if args.Value == "" {
			return SaveCredentialResult{Status: "error", Message: "Value is required for api_key type"}, nil
		}
		cred = &credentials.Credential{
			Type:   credentials.CredAPIKey,
			Header: args.Header,
			Value:  args.Value,
		}

	case credentials.CredBearer:
		if args.Token == "" {
			return SaveCredentialResult{Status: "error", Message: "Token is required for bearer type"}, nil
		}
		cred = &credentials.Credential{
			Type:  credentials.CredBearer,
			Token: args.Token,
		}

	case credentials.CredBasic:
		if args.Username == "" {
			return SaveCredentialResult{Status: "error", Message: "Username is required for basic type"}, nil
		}
		cred = &credentials.Credential{
			Type:     credentials.CredBasic,
			Username: args.Username,
			Password: args.Password,
		}

	case credentials.CredOAuthClientCreds:
		if args.AuthURL == "" {
			return SaveCredentialResult{Status: "error", Message: "auth_url is required for oauth_client_credentials type"}, nil
		}
		if args.ClientID == "" {
			return SaveCredentialResult{Status: "error", Message: "client_id is required for oauth_client_credentials type"}, nil
		}
		if args.ClientSecret == "" {
			return SaveCredentialResult{Status: "error", Message: "client_secret is required for oauth_client_credentials type"}, nil
		}
		cred = &credentials.Credential{
			Type:         credentials.CredOAuthClientCreds,
			AuthURL:      args.AuthURL,
			ClientID:     args.ClientID,
			ClientSecret: args.ClientSecret,
			Scope:        args.Scope,
		}

	default:
		return SaveCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Unknown type %q. Use: api_key, bearer, basic, or oauth_client_credentials", args.Type),
		}, nil
	}

	if err := credentialStoreVar.Set(args.Name, cred); err != nil {
		return SaveCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to save: %v", err),
		}, nil
	}

	return SaveCredentialResult{
		Status:  "saved",
		Message: fmt.Sprintf("Credential %q saved (%s). The value is encrypted and will not appear in conversation history.", args.Name, args.Type),
	}, nil
}

// --- list_credentials tool ---

type ListCredentialsArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional filter to match by name/key (e.g., 'anthropic', 'telegram'). If empty, lists everything."`
}

type CredentialSummary struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type SecretSummary struct {
	Key string `json:"key"`
}

type ListCredentialsResult struct {
	Credentials []CredentialSummary `json:"credentials"`
	Secrets     []SecretSummary     `json:"secrets"`
	Count       int                 `json:"count"`
	Message     string              `json:"message,omitempty"`
}

func listCredentials(_ tool.Context, args ListCredentialsArgs) (ListCredentialsResult, error) {
	if credentialStoreVar == nil {
		return ListCredentialsResult{Message: "Credential store is not available"}, nil
	}

	// HTTP credentials
	creds := credentialStoreVar.List()
	credSummaries := make([]CredentialSummary, 0, len(creds))
	for name, credType := range creds {
		if args.Filter != "" && !strings.Contains(name, args.Filter) {
			continue
		}
		credSummaries = append(credSummaries, CredentialSummary{
			Name: name,
			Type: string(credType),
		})
	}

	// Flat secrets (provider keys, channel tokens, MCP server keys)
	secretKeys := credentialStoreVar.ListSecrets()
	secretSummaries := make([]SecretSummary, 0, len(secretKeys))
	for _, key := range secretKeys {
		if args.Filter != "" && !strings.Contains(key, args.Filter) {
			continue
		}
		secretSummaries = append(secretSummaries, SecretSummary{Key: key})
	}

	total := len(credSummaries) + len(secretSummaries)
	var msg string
	if total == 0 {
		if args.Filter != "" {
			msg = fmt.Sprintf("Nothing matching %q. Use save_credential for HTTP credentials or the setup commands for provider keys.", args.Filter)
		} else {
			msg = "No stored credentials or secrets."
		}
	} else {
		parts := make([]string, 0, 2)
		if len(credSummaries) > 0 {
			parts = append(parts, fmt.Sprintf("%d HTTP credential(s)", len(credSummaries)))
		}
		if len(secretSummaries) > 0 {
			parts = append(parts, fmt.Sprintf("%d secret(s)", len(secretSummaries)))
		}
		msg = strings.Join(parts, ", ")
	}

	return ListCredentialsResult{
		Credentials: credSummaries,
		Secrets:     secretSummaries,
		Count:       total,
		Message:     msg,
	}, nil
}

// --- remove_credential tool ---

type RemoveCredentialArgs struct {
	Name string `json:"name" jsonschema:"The credential name to remove"`
}

type RemoveCredentialResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func removeCredential(_ tool.Context, args RemoveCredentialArgs) (RemoveCredentialResult, error) {
	if credentialStoreVar == nil {
		return RemoveCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return RemoveCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	if err := credentialStoreVar.Remove(args.Name); err != nil {
		return RemoveCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to remove: %v", err),
		}, nil
	}

	return RemoveCredentialResult{
		Status:  "removed",
		Message: fmt.Sprintf("Credential %q removed", args.Name),
	}, nil
}

// --- test_credential tool ---

type TestCredentialArgs struct {
	Name string `json:"name" jsonschema:"The credential name to test"`
}

type TestCredentialResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func testCredential(_ tool.Context, args TestCredentialArgs) (TestCredentialResult, error) {
	if credentialStoreVar == nil {
		return TestCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return TestCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	cred := credentialStoreVar.Get(args.Name)
	if cred == nil {
		return TestCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Credential %q not found", args.Name),
		}, nil
	}

	switch cred.Type {
	case credentials.CredOAuthClientCreds:
		// Actually test the OAuth flow
		_, _, err := credentialStoreVar.Resolve(args.Name)
		if err != nil {
			return TestCredentialResult{
				Status:  "failed",
				Message: fmt.Sprintf("OAuth token acquisition failed: %v", err),
			}, nil
		}
		return TestCredentialResult{
			Status:  "ok",
			Message: fmt.Sprintf("OAuth credential %q: token acquired successfully", args.Name),
		}, nil

	default:
		return TestCredentialResult{
			Status:  "ok",
			Message: fmt.Sprintf("Credential %q configured (%s). Use http_request with credential=%q to test connectivity.", args.Name, cred.Type, args.Name),
		}, nil
	}
}

// --- Tool constructors ---

func NewSaveCredentialTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "save_credential",
		Description: `Save a credential (API key, token, or OAuth config) to the encrypted credential store.

The credential is encrypted at rest and never exposed in conversation history. 
Use this IMMEDIATELY when the user provides any secret (API key, token, password, client secret).
Do NOT repeat the secret value in your response — just confirm it was saved.

Types:
- api_key: Custom header + value (e.g., X-API-Key: sk-...)
- bearer: Authorization: Bearer <token>
- basic: Authorization: Basic <base64(user:pass)>
- oauth_client_credentials: Auto-refreshing OAuth2 (provide auth_url, client_id, client_secret)

After saving, the credential can be referenced by name in http_request calls.`,
	}, saveCredential)
}

func NewListCredentialsTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_credentials",
		Description: "List all stored credentials and secrets. Shows HTTP credentials (name + type) and provider/channel/MCP secrets (dot-notation keys like 'provider.anthropic.api_key'). Secret values are never exposed. Optionally filter by name or key.",
	}, listCredentials)
}

func NewRemoveCredentialTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "remove_credential",
		Description: "Remove a credential from the encrypted store by name.",
	}, removeCredential)
}

func NewTestCredentialTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "test_credential",
		Description: "Test a stored credential. For OAuth: performs the token acquisition flow. For others: confirms configuration is valid.",
	}, testCredential)
}

// GetCredentialTools returns all credential management tools.
func GetCredentialTools() ([]tool.Tool, error) {
	var tools []tool.Tool

	saveTool, err := NewSaveCredentialTool()
	if err != nil {
		return nil, fmt.Errorf("save_credential: %w", err)
	}
	tools = append(tools, saveTool)

	listTool, err := NewListCredentialsTool()
	if err != nil {
		return nil, fmt.Errorf("list_credentials: %w", err)
	}
	tools = append(tools, listTool)

	removeTool, err := NewRemoveCredentialTool()
	if err != nil {
		return nil, fmt.Errorf("remove_credential: %w", err)
	}
	tools = append(tools, removeTool)

	testTool, err := NewTestCredentialTool()
	if err != nil {
		return nil, fmt.Errorf("test_credential: %w", err)
	}
	tools = append(tools, testTool)

	return tools, nil
}
