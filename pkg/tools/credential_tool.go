package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/filestore"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// credentialStoreVar holds the file-based credential store reference.
// Set by the daemon or console launcher via SetCredentialStore.
// Used as fallback when no platform credential store is available in the context.
var credentialStoreVar *credentials.Store

// SetCredentialStore registers the credential store for LLM tool access.
func SetCredentialStore(store *credentials.Store) {
	credentialStoreVar = store
}

// GetCredentialStore returns the current credential store (for redaction wiring).
func GetCredentialStore() *credentials.Store {
	return credentialStoreVar
}

// getEffectiveCredStore returns the tenant-scoped credential store from the
// context (platform mode) or wraps the file-based global (personal mode).
// Returns nil if neither is available.
func getEffectiveCredStore(ctx context.Context) store.CredentialStore {
	// Platform mode: credential store injected into context by ChatRunner
	if ctx != nil {
		if cs := store.CredentialStoreFromContext(ctx); cs != nil {
			return cs
		}
	}
	// Personal mode: wrap the global file-based store
	if credentialStoreVar != nil {
		return filestore.NewCredentialStore(credentialStoreVar)
	}
	return nil
}

// --- save_credential tool ---

type SaveCredentialArgs struct {
	Name         string `json:"name" jsonschema:"Short identifier for this credential (e.g., 'my-api', 'proxmox', 'google-calendar')"`
	Type         string `json:"type" jsonschema:"Credential type: 'api_key' (custom header+value), 'bearer' (Authorization: Bearer token), 'basic' (HTTP Basic Auth), 'password' (plain username+password for SSH/FTP/SMTP/databases), 'oauth_client_credentials' (auto-refreshing OAuth2 server-to-server), 'oauth_authorization_code' (user-authorized OAuth2 with refresh token — Google, GitHub, etc.)"`
	Header       string `json:"header,omitempty" jsonschema:"Header name for api_key type (e.g., 'X-API-Key', 'Authorization'). Required for api_key type."`
	Value        string `json:"value,omitempty" jsonschema:"The API key value. Required for api_key type."`
	Token        string `json:"token,omitempty" jsonschema:"The bearer token. Required for bearer type."`
	Username     string `json:"username,omitempty" jsonschema:"Username for basic or password type. Required for both."`
	Password     string `json:"password,omitempty" jsonschema:"Password for basic or password type. Required for both."`
	AuthURL      string `json:"auth_url,omitempty" jsonschema:"OAuth token endpoint URL. Required for oauth_client_credentials type."`
	ClientID     string `json:"client_id,omitempty" jsonschema:"OAuth client ID. Required for oauth_client_credentials and oauth_authorization_code types."`
	ClientSecret string `json:"client_secret,omitempty" jsonschema:"OAuth client secret. Required for oauth_client_credentials and oauth_authorization_code types."`
	Scope        string `json:"scope,omitempty" jsonschema:"OAuth scope (optional, for oauth_client_credentials and oauth_authorization_code types)."`
	TokenURL     string `json:"token_url,omitempty" jsonschema:"Token endpoint URL for exchanging/refreshing tokens. Required for oauth_authorization_code type (e.g., https://oauth2.googleapis.com/token)."`
	AccessToken  string `json:"access_token,omitempty" jsonschema:"Current access token. Required for oauth_authorization_code type."`
	RefreshToken string `json:"refresh_token,omitempty" jsonschema:"Refresh token for obtaining new access tokens. Required for oauth_authorization_code type."`
}

type SaveCredentialResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func saveCredential(ctx tool.Context, args SaveCredentialArgs) (SaveCredentialResult, error) {
	cs := getEffectiveCredStore(ctx)
	if cs == nil {
		return SaveCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return SaveCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	var storeCred *store.Credential

	credType := store.CredentialType(args.Type)
	switch credType {
	case store.CredAPIKey:
		if args.Header == "" {
			return SaveCredentialResult{Status: "error", Message: "Header is required for api_key type"}, nil
		}
		if args.Value == "" {
			return SaveCredentialResult{Status: "error", Message: "Value is required for api_key type"}, nil
		}
		storeCred = &store.Credential{
			Type:   store.CredAPIKey,
			Header: args.Header,
			Value:  args.Value,
		}

	case store.CredBearer:
		if args.Token == "" {
			return SaveCredentialResult{Status: "error", Message: "Token is required for bearer type"}, nil
		}
		storeCred = &store.Credential{
			Type:  store.CredBearer,
			Token: args.Token,
		}

	case store.CredBasic:
		if args.Username == "" {
			return SaveCredentialResult{Status: "error", Message: "Username is required for basic type"}, nil
		}
		storeCred = &store.Credential{
			Type:     store.CredBasic,
			Username: args.Username,
			Password: args.Password,
		}

	case store.CredOAuthClientCreds:
		if args.AuthURL == "" {
			return SaveCredentialResult{Status: "error", Message: "auth_url is required for oauth_client_credentials type"}, nil
		}
		if args.ClientID == "" {
			return SaveCredentialResult{Status: "error", Message: "client_id is required for oauth_client_credentials type"}, nil
		}
		if args.ClientSecret == "" {
			return SaveCredentialResult{Status: "error", Message: "client_secret is required for oauth_client_credentials type"}, nil
		}
		storeCred = &store.Credential{
			Type:         store.CredOAuthClientCreds,
			AuthURL:      args.AuthURL,
			ClientID:     args.ClientID,
			ClientSecret: args.ClientSecret,
			Scope:        args.Scope,
		}

	case store.CredPassword:
		if args.Username == "" {
			return SaveCredentialResult{Status: "error", Message: "Username is required for password type"}, nil
		}
		storeCred = &store.Credential{
			Type:     store.CredPassword,
			Username: args.Username,
			Password: args.Password,
		}

	case store.CredOAuthAuthCode:
		if args.TokenURL == "" {
			return SaveCredentialResult{Status: "error", Message: "token_url is required for oauth_authorization_code type (e.g., https://oauth2.googleapis.com/token)"}, nil
		}
		if args.ClientID == "" {
			return SaveCredentialResult{Status: "error", Message: "client_id is required for oauth_authorization_code type"}, nil
		}
		if args.ClientSecret == "" {
			return SaveCredentialResult{Status: "error", Message: "client_secret is required for oauth_authorization_code type"}, nil
		}
		if args.AccessToken == "" {
			return SaveCredentialResult{Status: "error", Message: "access_token is required for oauth_authorization_code type — exchange the authorization code first, then save the resulting tokens"}, nil
		}
		if args.RefreshToken == "" {
			return SaveCredentialResult{Status: "error", Message: "refresh_token is required for oauth_authorization_code type — ensure access_type=offline was used in the authorization URL"}, nil
		}
		storeCred = &store.Credential{
			Type:         store.CredOAuthAuthCode,
			TokenURL:     args.TokenURL,
			ClientID:     args.ClientID,
			ClientSecret: args.ClientSecret,
			AccessToken:  args.AccessToken,
			RefreshToken: args.RefreshToken,
			Scope:        args.Scope,
		}

	default:
		return SaveCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Unknown type %q. Use: api_key, bearer, basic, password, oauth_client_credentials, or oauth_authorization_code", args.Type),
		}, nil
	}

	if err := cs.Set(ctx, args.Name, storeCred); err != nil {
		return SaveCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to save: %v", err),
		}, nil
	}

	// Immediately register the new credential's secret values with the
	// Redactor so that tool outputs in the same session are protected.
	if r := credentials.RedactorFromContext(ctx); r != nil {
		r.HydrateFromStore(cs)
	}

	return SaveCredentialResult{
		Status: "saved",
		Message: fmt.Sprintf("Credential %q saved (%s). The value is encrypted and will not appear in conversation history. "+
			"IMPORTANT: Now save a memory note documenting: (1) credential name %q, (2) what format/prefix is already included in the stored value "+
			"(so you never double-prefix it), (3) the header name and how to use it with api_call, (4) the target API base URL. "+
			"Do NOT include the actual secret value in the memory note — reference it by name only.", args.Name, args.Type, args.Name),
	}, nil
}

// --- list_credentials tool ---

type ListCredentialsArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional filter to match by name/key (e.g., 'anthropic', 'telegram'). If empty, lists everything."`
}

type CredentialSummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Scope    string `json:"scope,omitempty"`    // "personal" or "team" — only set in platform mode
	Shadowed bool   `json:"shadowed,omitempty"` // true if a personal credential overrides this team credential
}

type SecretSummary struct {
	Key   string `json:"key"`
	Scope string `json:"scope,omitempty"` // "personal" or "team" — only set in platform mode
}

type ListCredentialsResult struct {
	Credentials []CredentialSummary `json:"credentials"`
	Secrets     []SecretSummary     `json:"secrets"`
	Count       int                 `json:"count"`
	Message     string              `json:"message,omitempty"`
}

func listCredentials(ctx tool.Context, args ListCredentialsArgs) (ListCredentialsResult, error) {
	cs := getEffectiveCredStore(ctx)
	if cs == nil {
		return ListCredentialsResult{Message: "Credential store is not available"}, nil
	}

	// Reload to pick up changes made outside this process
	cs.Reload(ctx) //nolint:errcheck

	// Check if we have a merged store for scope-aware listing
	merged, isMerged := cs.(*store.MergedCredentialStore)

	var credSummaries []CredentialSummary
	var secretSummaries []SecretSummary

	if isMerged {
		// Platform mode: list from both stores with scope labels
		personalCreds := make(map[string]store.CredentialType)
		teamCreds := make(map[string]store.CredentialType)
		personalSecrets := make(map[string]bool)

		if merged.Personal != nil {
			personalCreds = merged.Personal.List(ctx)
			for _, k := range merged.Personal.ListSecrets(ctx) {
				personalSecrets[k] = true
			}
		}
		if merged.Team != nil {
			teamCreds = merged.Team.List(ctx)
		}

		// Personal credentials first
		for name, credType := range personalCreds {
			if args.Filter != "" && !strings.Contains(name, args.Filter) {
				continue
			}
			credSummaries = append(credSummaries, CredentialSummary{
				Name:  name,
				Type:  string(credType),
				Scope: "personal",
			})
		}

		// Team credentials (mark shadowed if personal has same name)
		for name, credType := range teamCreds {
			if args.Filter != "" && !strings.Contains(name, args.Filter) {
				continue
			}
			_, shadowed := personalCreds[name]
			credSummaries = append(credSummaries, CredentialSummary{
				Name:     name,
				Type:     string(credType),
				Scope:    "team",
				Shadowed: shadowed,
			})
		}

		// Personal secrets
		if merged.Personal != nil {
			for _, key := range merged.Personal.ListSecrets(ctx) {
				if args.Filter != "" && !strings.Contains(key, args.Filter) {
					continue
				}
				secretSummaries = append(secretSummaries, SecretSummary{Key: key, Scope: "personal"})
			}
		}

		// Team secrets
		if merged.Team != nil {
			for _, key := range merged.Team.ListSecrets(ctx) {
				if args.Filter != "" && !strings.Contains(key, args.Filter) {
					continue
				}
				if !personalSecrets[key] {
					secretSummaries = append(secretSummaries, SecretSummary{Key: key, Scope: "team"})
				} else {
					secretSummaries = append(secretSummaries, SecretSummary{Key: key, Scope: "team (shadowed)"})
				}
			}
		}
	} else {
		// Personal mode: single store, no scope labels
		creds := cs.List(ctx)
		credSummaries = make([]CredentialSummary, 0, len(creds))
		for name, credType := range creds {
			if args.Filter != "" && !strings.Contains(name, args.Filter) {
				continue
			}
			credSummaries = append(credSummaries, CredentialSummary{
				Name: name,
				Type: string(credType),
			})
		}

		secretKeys := cs.ListSecrets(ctx)
		secretSummaries = make([]SecretSummary, 0, len(secretKeys))
		for _, key := range secretKeys {
			if args.Filter != "" && !strings.Contains(key, args.Filter) {
				continue
			}
			secretSummaries = append(secretSummaries, SecretSummary{Key: key})
		}
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

func removeCredential(ctx tool.Context, args RemoveCredentialArgs) (RemoveCredentialResult, error) {
	cs := getEffectiveCredStore(ctx)
	if cs == nil {
		return RemoveCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return RemoveCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	if err := cs.Remove(ctx, args.Name); err != nil {
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

func testCredential(ctx tool.Context, args TestCredentialArgs) (TestCredentialResult, error) {
	cs := getEffectiveCredStore(ctx)
	if cs == nil {
		return TestCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return TestCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	cred := cs.Get(ctx, args.Name)
	if cred == nil {
		return TestCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Credential %q not found", args.Name),
		}, nil
	}

	switch cred.Type {
	case store.CredOAuthClientCreds:
		// Actually test the OAuth flow
		_, _, err := cs.Resolve(ctx, args.Name)
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

	case store.CredOAuthAuthCode:
		// Test by resolving (which triggers refresh if expired)
		_, _, err := cs.Resolve(ctx, args.Name)
		if err != nil {
			return TestCredentialResult{
				Status:  "failed",
				Message: fmt.Sprintf("OAuth authorization code credential %q: token refresh failed: %v", args.Name, err),
			}, nil
		}
		return TestCredentialResult{
			Status:  "ok",
			Message: fmt.Sprintf("OAuth credential %q: access token is valid (auto-refreshes when expired)", args.Name),
		}, nil

	case store.CredPassword:
		return TestCredentialResult{
			Status:  "ok",
			Message: fmt.Sprintf("Credential %q configured (%s, user: %s). Use resolve_credential to retrieve the username and password for SSH/FTP/database connections.", args.Name, cred.Type, cred.Username),
		}, nil

	default:
		return TestCredentialResult{
			Status:  "ok",
			Message: fmt.Sprintf("Credential %q configured (%s). Use http_request with credential=%q to test connectivity.", args.Name, cred.Type, args.Name),
		}, nil
	}
}

// --- resolve_credential tool ---

type ResolveCredentialArgs struct {
	Name string `json:"name" jsonschema:"The credential name to resolve"`
}

type ResolveCredentialResult struct {
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	Type     string `json:"type,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	Header   string `json:"header,omitempty"`
	Value    string `json:"value,omitempty"`
	AuthURL  string `json:"auth_url,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

func resolveCredential(ctx tool.Context, args ResolveCredentialArgs) (ResolveCredentialResult, error) {
	cs := getEffectiveCredStore(ctx)
	if cs == nil {
		return ResolveCredentialResult{Status: "error", Message: "Credential store is not available"}, nil
	}

	if args.Name == "" {
		return ResolveCredentialResult{Status: "error", Message: "Name is required"}, nil
	}

	// Reload to pick up changes made outside this process
	cs.Reload(ctx) //nolint:errcheck

	cred := cs.Get(ctx, args.Name)
	if cred == nil {
		return ResolveCredentialResult{
			Status:  "error",
			Message: fmt.Sprintf("Credential %q not found", args.Name),
		}, nil
	}

	result := ResolveCredentialResult{
		Status: "ok",
		Type:   string(cred.Type),
	}

	switch cred.Type {
	case store.CredPassword:
		result.Username = cred.Username
		result.Password = credentials.FormatPlaceholder(args.Name, "password")
	case store.CredBasic:
		result.Username = cred.Username
		result.Password = credentials.FormatPlaceholder(args.Name, "password")
	case store.CredBearer:
		result.Token = credentials.FormatPlaceholder(args.Name, "token")
	case store.CredAPIKey:
		result.Header = cred.Header
		result.Value = credentials.FormatPlaceholder(args.Name, "value")
	case store.CredOAuthClientCreds:
		result.AuthURL = cred.AuthURL
		result.ClientID = cred.ClientID
		result.Message = "Use http_request with credential parameter for OAuth — the token is managed automatically."
	case store.CredOAuthAuthCode:
		result.Token = credentials.FormatPlaceholder(args.Name, "token")
		result.ClientID = cred.ClientID
		result.Message = "Access token placeholder returned (auto-refreshed when used). Prefer using http_request with credential parameter — it handles the Bearer header automatically."
	}

	return result, nil
}

// --- Tool constructors ---

func NewSaveCredentialTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "save_credential",
		Description: `Save a credential to the encrypted store. Credentials saved here are PERSONAL (only you can see/use them). To share with the team, use the Settings UI 'Publish to Team'. Use IMMEDIATELY when the user provides any secret. Types: api_key, bearer, basic, password, oauth_client_credentials, oauth_authorization_code. HTTP credentials work with http_request; password credentials with resolve_credential. After saving, ALWAYS use memory_save to document: credential name, what format/prefix is already in the stored value, the header name, and the target API base URL — but NEVER include the actual secret value in the memory note.`,
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

func NewResolveCredentialTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "resolve_credential",
		Description: `Retrieve fields of a stored credential by name. Use for non-HTTP auth (SSH, FTP, databases). Returns type-specific fields: non-secret fields (username, header) as plaintext, secret fields (password, token, value) as {{CREDENTIAL:name:field}} placeholders. Pass placeholders directly to process_write, shell_command, browser_type, etc. — the system substitutes real values at execution time. The real secrets never appear in your context.`,
	}, resolveCredential)
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

	resolveTool, err := NewResolveCredentialTool()
	if err != nil {
		return nil, fmt.Errorf("resolve_credential: %w", err)
	}
	tools = append(tools, resolveTool)

	return tools, nil
}
