package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/store"
)

// --- Test: resolveCredentialVarsInRawContext ---

func TestResolveCredentialVarsInRawContext_BasicResolution(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	state.Data["credential_name"] = "openstack"

	raw := `APP_CRED_ID="{{CREDENTIAL:{credential_name}:username}}"
APP_CRED_SECRET="{{CREDENTIAL:{credential_name}:password}}"`

	result := a.resolveCredentialVarsInRawContext(raw, state)

	if !strings.Contains(result, `{{CREDENTIAL:openstack:username}}`) {
		t.Errorf("expected {{CREDENTIAL:openstack:username}} in result, got:\n%s", result)
	}
	if !strings.Contains(result, `{{CREDENTIAL:openstack:password}}`) {
		t.Errorf("expected {{CREDENTIAL:openstack:password}} in result, got:\n%s", result)
	}
	// Should NOT contain the unresolved {credential_name}
	if strings.Contains(result, `{credential_name}`) {
		t.Errorf("should not contain unresolved {credential_name}, got:\n%s", result)
	}
}

func TestResolveCredentialVarsInRawContext_PreservesShellSyntax(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	state.Data["credential_name"] = "openstack"

	raw := `APP_CRED_ID="{{CREDENTIAL:{credential_name}:username}}"
TOKEN=$(grep -i "x-subject-token" /tmp/os_headers | awk '{print $2}' | tr -d '\r\n')
NOVA_URL=$(python3 -c "import json; [print(ep['url']) for svc in json.load(open('/tmp/os_auth_response.json'))['token']['catalog']")
curl -s -H "X-Auth-Token: $TOKEN" "$NOVA_URL/servers/detail"`

	result := a.resolveCredentialVarsInRawContext(raw, state)

	// Credential placeholder should be resolved
	if !strings.Contains(result, `{{CREDENTIAL:openstack:username}}`) {
		t.Errorf("credential placeholder not resolved, got:\n%s", result)
	}
	// Shell syntax should be preserved EXACTLY
	if !strings.Contains(result, `awk '{print $2}'`) {
		t.Errorf("awk syntax garbled, got:\n%s", result)
	}
	if !strings.Contains(result, `$TOKEN`) {
		t.Errorf("shell $TOKEN garbled, got:\n%s", result)
	}
	if !strings.Contains(result, `$NOVA_URL`) {
		t.Errorf("shell $NOVA_URL garbled, got:\n%s", result)
	}
	if !strings.Contains(result, `json.load(open('/tmp/os_auth_response.json'))['token']['catalog']`) {
		t.Errorf("python expression garbled, got:\n%s", result)
	}
}

func TestResolveCredentialVarsInRawContext_UnresolvableVarLeftAsIs(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	// credential_name NOT in state

	raw := `APP_CRED_ID="{{CREDENTIAL:{credential_name}:username}}"`

	result := a.resolveCredentialVarsInRawContext(raw, state)

	// Should be left as-is since variable can't be resolved
	if !strings.Contains(result, `{{CREDENTIAL:{credential_name}:username}}`) {
		t.Errorf("unresolvable placeholder should be left as-is, got:\n%s", result)
	}
}

func TestResolveCredentialVarsInRawContext_AlreadyResolved(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	state.Data["credential_name"] = "openstack"

	// Already has a literal credential name (no nested {var})
	raw := `APP_CRED_ID="{{CREDENTIAL:openstack:username}}"`

	result := a.resolveCredentialVarsInRawContext(raw, state)

	// Should remain unchanged
	if result != raw {
		t.Errorf("already-resolved placeholder should not be modified,\nexpected: %s\ngot: %s", raw, result)
	}
}

// --- Test: renderString preserves credential placeholders ---

func TestRenderString_PreservesCredentialPlaceholders(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	state.Data["credential_name"] = "openstack"

	// System instruction containing credential placeholders
	tmpl := `Use {{CREDENTIAL:openstack:username}} and {{CREDENTIAL:openstack:password}} for auth.`

	result := a.renderString(tmpl, state)

	if !strings.Contains(result, `{{CREDENTIAL:openstack:username}}`) {
		t.Errorf("renderString garbled credential placeholder 'username', got:\n%s", result)
	}
	if !strings.Contains(result, `{{CREDENTIAL:openstack:password}}`) {
		t.Errorf("renderString garbled credential placeholder 'password', got:\n%s", result)
	}
}

func TestRenderString_ResolvesStateVarsAndPreservesCredentials(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	state.Data["credential_name"] = "openstack"

	// Mix of state vars and credential placeholders
	tmpl := `Auth with '{credential_name}' using {{CREDENTIAL:openstack:password}}.`

	result := a.renderString(tmpl, state)

	// State var should be resolved
	if !strings.Contains(result, `'openstack'`) {
		t.Errorf("state var not resolved, got:\n%s", result)
	}
	// Credential placeholder should be preserved
	if !strings.Contains(result, `{{CREDENTIAL:openstack:password}}`) {
		t.Errorf("credential placeholder garbled after state var resolution, got:\n%s", result)
	}
}

func TestRenderString_MultipleCredentialPlaceholders(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()

	tmpl := `ID={{CREDENTIAL:myapp:username}} SECRET={{CREDENTIAL:myapp:password}} TOKEN={{CREDENTIAL:other:token}}`

	result := a.renderString(tmpl, state)

	for _, placeholder := range []string{
		"{{CREDENTIAL:myapp:username}}",
		"{{CREDENTIAL:myapp:password}}",
		"{{CREDENTIAL:other:token}}",
	} {
		if !strings.Contains(result, placeholder) {
			t.Errorf("missing placeholder %q in result:\n%s", placeholder, result)
		}
	}
}

// --- Test: Full instruction construction pipeline (simulates executeLLMNodeAttempt) ---

func TestFullInstructionPipeline_OpenStackFlow(t *testing.T) {
	a := &AstonishAgent{}
	state := NewMockState()
	state.Data["credential_name"] = "openstack"

	// Simulates the list_vms node from the OpenStack flow
	node := &config.Node{
		Name:   "list_vms",
		Type:   "llm",
		System: "You are an infrastructure automation assistant.",
		Prompt: "Authenticate with the OpenStack credential '{credential_name}' and list all VMs in the project.",
		RawContext: `Execute EXACTLY this proven script. Do NOT modify the approach or use alternatives.

APP_CRED_ID="{{CREDENTIAL:{credential_name}:username}}"
APP_CRED_SECRET="{{CREDENTIAL:{credential_name}:password}}"

cat > /tmp/os_auth_payload.json <<EOF
{
  "auth": {
    "identity": {
      "methods": ["application_credential"],
      "application_credential": {
        "id": "${APP_CRED_ID}",
        "secret": "${APP_CRED_SECRET}"
      }
    }
  }
}
EOF

curl -s -X POST "https://identity-3.qa-de-1.cloud.sap/v3/auth/tokens" \
  -H "Content-Type: application/json" \
  -d @/tmp/os_auth_payload.json \
  -D /tmp/os_headers -o /tmp/os_auth_response.json

TOKEN=$(grep -i "x-subject-token" /tmp/os_headers | awk '{print $2}' | tr -d '\r\n')

NOVA_URL=$(python3 -c "import json; [print(ep['url']) for svc in json.load(open('/tmp/os_auth_response.json'))['token']['catalog'] if svc['type']=='compute' for ep in svc['endpoints'] if ep['region']=='qa-de-1' and ep['interface']=='public']")

echo "Token: $(echo $TOKEN | cut -c1-20)..."
echo "Nova URL: $NOVA_URL"

curl -s -H "X-Auth-Token: $TOKEN" "$NOVA_URL/servers/detail" | jq '[.servers[] | {
  id,
  name,
  status,
  flavor: .flavor.original_name,
  image: (.image.id? // "(boot-from-volume)"),
  created,
  updated
}]'
`,
	}

	// Step 1: Render system instruction (same as node_llm.go:367-368)
	userPrompt := a.renderString(node.Prompt, state)
	systemInstruction := a.renderString(node.System, state)

	// Step 2: Append raw_context with credential var resolution (same as node_llm.go:373-383)
	if node.RawContext != "" {
		if systemInstruction != "" {
			systemInstruction += "\n\n"
		}
		rawCtx := a.resolveCredentialVarsInRawContext(node.RawContext, state)
		systemInstruction += rawCtx
	}

	instruction := systemInstruction

	// --- Assertions ---

	// User prompt should have credential_name resolved
	if !strings.Contains(userPrompt, "'openstack'") {
		t.Errorf("userPrompt should contain resolved credential_name, got:\n%s", userPrompt)
	}

	// Instruction should contain resolved credential placeholders
	if !strings.Contains(instruction, `{{CREDENTIAL:openstack:username}}`) {
		t.Errorf("instruction missing {{CREDENTIAL:openstack:username}}, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, `{{CREDENTIAL:openstack:password}}`) {
		t.Errorf("instruction missing {{CREDENTIAL:openstack:password}}, got:\n%s", instruction)
	}

	// Instruction should NOT contain unresolved {credential_name}
	if strings.Contains(instruction, `{credential_name}`) {
		t.Errorf("instruction should not contain {credential_name}, got:\n%s", instruction)
	}

	// Shell syntax should be preserved
	if !strings.Contains(instruction, `${APP_CRED_ID}`) {
		t.Errorf("shell ${APP_CRED_ID} missing from instruction, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, `${APP_CRED_SECRET}`) {
		t.Errorf("shell ${APP_CRED_SECRET} missing from instruction, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, `awk '{print $2}'`) {
		t.Errorf("awk syntax garbled in instruction, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, `$TOKEN`) {
		t.Errorf("$TOKEN garbled in instruction")
	}
	if !strings.Contains(instruction, `$NOVA_URL`) {
		t.Errorf("$NOVA_URL garbled in instruction")
	}

	// The system instruction header should be present
	if !strings.Contains(instruction, "You are an infrastructure automation assistant.") {
		t.Errorf("system instruction header missing, got:\n%s", instruction)
	}

	// The credential preservation directive should NOT be present in raw instruction
	// (it's added later in executeLLMNodeAttempt after the instruction is built)
	// But we can test that credentials.ContainsPlaceholder detects them
	if !credentials.ContainsPlaceholder(instruction) {
		t.Errorf("ContainsPlaceholder should detect credential placeholders in instruction")
	}
}

// --- Test: SubstituteShellCommand with mock credential resolver ---

// mockCredentialResolver implements credentials.CredentialResolver for testing.
type mockCredentialResolver struct {
	creds map[string]*credentials.Credential
}

func (m *mockCredentialResolver) Get(name string) *credentials.Credential {
	return m.creds[name]
}

func (m *mockCredentialResolver) Resolve(name string) (headerKey, headerValue string, err error) {
	return "", "", nil
}

func (m *mockCredentialResolver) Reload() error {
	return nil
}

func TestSubstituteShellCommand_ResolvesCredentials(t *testing.T) {
	resolver := &mockCredentialResolver{
		creds: map[string]*credentials.Credential{
			"openstack": {
				Type:     credentials.CredPassword,
				Username: "real-app-credential-id-12345",
				Password: "real-app-secret-67890",
			},
		},
	}

	// Simulates the shell command the LLM would generate from the instruction
	command := `APP_CRED_ID="{{CREDENTIAL:openstack:username}}"
APP_CRED_SECRET="{{CREDENTIAL:openstack:password}}"
cat > /tmp/auth.json <<EOF
{"auth":{"identity":{"methods":["application_credential"],"application_credential":{"id":"${APP_CRED_ID}","secret":"${APP_CRED_SECRET}"}}}}
EOF
curl -s -X POST "https://identity-3.qa-de-1.cloud.sap/v3/auth/tokens" -d @/tmp/auth.json`

	result := credentials.SubstituteShellCommand(command, resolver)

	// Should contain env var exports with the real values
	if !strings.Contains(result, "export __ASTONISH_CRED_") {
		t.Fatalf("expected env var exports, got:\n%s", result)
	}

	// Should NOT contain the literal credential placeholder anymore
	if strings.Contains(result, "{{CREDENTIAL:openstack:username}}") {
		t.Errorf("credential username placeholder should be resolved, got:\n%s", result)
	}
	if strings.Contains(result, "{{CREDENTIAL:openstack:password}}") {
		t.Errorf("credential password placeholder should be resolved, got:\n%s", result)
	}

	// The real values should be present (single-quoted for shell safety)
	if !strings.Contains(result, "real-app-credential-id-12345") {
		t.Errorf("real username value should be in the env var export, got:\n%s", result)
	}
	if !strings.Contains(result, "real-app-secret-67890") {
		t.Errorf("real password value should be in the env var export, got:\n%s", result)
	}
}

// --- Test: BeforeToolCallback credential resolution via context ---

// mockPGCredentialStore implements store.CredentialStore for testing.
type mockPGCredentialStore struct {
	creds map[string]*store.Credential
}

func (m *mockPGCredentialStore) Get(_ context.Context, name string) *store.Credential {
	return m.creds[name]
}
func (m *mockPGCredentialStore) Set(_ context.Context, _ string, _ *store.Credential) error {
	return nil
}
func (m *mockPGCredentialStore) Remove(_ context.Context, _ string) error { return nil }
func (m *mockPGCredentialStore) List(_ context.Context) map[string]store.CredentialType {
	result := make(map[string]store.CredentialType)
	for name, cred := range m.creds {
		result[name] = cred.Type
	}
	return result
}
func (m *mockPGCredentialStore) Count(_ context.Context) int { return len(m.creds) }
func (m *mockPGCredentialStore) Resolve(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}
func (m *mockPGCredentialStore) InvalidateToken(_ context.Context, _ string)        {}
func (m *mockPGCredentialStore) Reload(_ context.Context) error                     { return nil }
func (m *mockPGCredentialStore) GetSecret(_ context.Context, _ string) string        { return "" }
func (m *mockPGCredentialStore) SetSecret(_ context.Context, _, _ string) error      { return nil }
func (m *mockPGCredentialStore) SetSecretBatch(_ context.Context, _ map[string]string) error {
	return nil
}
func (m *mockPGCredentialStore) RemoveSecret(_ context.Context, _ string) error { return nil }
func (m *mockPGCredentialStore) HasSecrets(_ context.Context) bool               { return false }
func (m *mockPGCredentialStore) SecretCount(_ context.Context) int               { return 0 }
func (m *mockPGCredentialStore) ListSecrets(_ context.Context) []string          { return nil }

func TestBeforeToolCallback_CredentialResolutionFromContext(t *testing.T) {
	// Simulate the PG credential store in context (as FlowRunHandler does)
	pgStore := &mockPGCredentialStore{
		creds: map[string]*store.Credential{
			"openstack": {
				Type:     store.CredPassword,
				Username: "app-cred-id-from-pg",
				Password: "app-cred-secret-from-pg",
			},
		},
	}

	// Create context with credential store (same as flow_run_handler.go:222)
	ctx := store.WithCredentialStore(context.Background(), pgStore)

	// Verify the credential store is retrievable from context
	cs := store.CredentialStoreFromContext(ctx)
	if cs == nil {
		t.Fatal("CredentialStoreFromContext returned nil — context injection failed")
	}

	// Create the resolver (same as node_llm.go:729-731)
	resolver := credentials.NewStoreAdapter(cs)
	if resolver == nil {
		t.Fatal("NewStoreAdapter returned nil")
	}

	// Verify credential can be retrieved
	cred := resolver.Get("openstack")
	if cred == nil {
		t.Fatal("resolver.Get('openstack') returned nil")
	}
	if cred.Username != "app-cred-id-from-pg" {
		t.Errorf("expected username 'app-cred-id-from-pg', got %q", cred.Username)
	}
	if cred.Password != "app-cred-secret-from-pg" {
		t.Errorf("expected password 'app-cred-secret-from-pg', got %q", cred.Password)
	}

	// Now test the full SubstituteShellCommand path
	command := `APP_CRED_ID="{{CREDENTIAL:openstack:username}}"
APP_CRED_SECRET="{{CREDENTIAL:openstack:password}}"
curl -s -X POST "https://identity.example.com/v3/auth/tokens" -d "id=${APP_CRED_ID}&secret=${APP_CRED_SECRET}"`

	result := credentials.SubstituteShellCommand(command, resolver)

	if !strings.Contains(result, "app-cred-id-from-pg") {
		t.Errorf("PG credential username not resolved in shell command, got:\n%s", result)
	}
	if !strings.Contains(result, "app-cred-secret-from-pg") {
		t.Errorf("PG credential password not resolved in shell command, got:\n%s", result)
	}
}

// --- Test: End-to-end simulation of tool node credential resolution ---

func TestToolNodeCredentialResolution(t *testing.T) {
	// Simulate a tool-type node with credential placeholders in args
	pgStore := &mockPGCredentialStore{
		creds: map[string]*store.Credential{
			"mydb": {
				Type:     store.CredPassword,
				Username: "db-admin",
				Password: "s3cret!",
			},
		},
	}

	ctx := store.WithCredentialStore(context.Background(), pgStore)

	// Build resolver (same as node_tool.go credential resolution)
	var resolver credentials.CredentialResolver
	if cs := store.CredentialStoreFromContext(ctx); cs != nil {
		resolver = credentials.NewStoreAdapter(cs)
	}

	if resolver == nil {
		t.Fatal("resolver should not be nil")
	}

	// Simulate tool args after renderString (with credential placeholders)
	resolvedArgs := map[string]any{
		"command": `mysql -u "{{CREDENTIAL:mydb:username}}" -p"{{CREDENTIAL:mydb:password}}" production`,
		"timeout": 30,
	}

	// Run substitution (same as node_tool.go)
	shellFields := []string{"command"}
	credentials.SubstituteAndRestore(resolvedArgs, resolver, shellFields...)

	cmd := resolvedArgs["command"].(string)

	// Should contain env var exports
	if !strings.Contains(cmd, "export __ASTONISH_CRED_") {
		t.Fatalf("expected env var exports in command, got:\n%s", cmd)
	}
	// Should contain real values
	if !strings.Contains(cmd, "db-admin") {
		t.Errorf("username not resolved, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "s3cret!") {
		t.Errorf("password not resolved, got:\n%s", cmd)
	}
}
