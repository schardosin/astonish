package drill

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/fleet"
	"github.com/SAP/astonish/pkg/store"
	"gopkg.in/yaml.v3"
)

func TestSuiteConfigParsesCredentialsConfigureInjection(t *testing.T) {
	t.Parallel()
	input := `
description: "Ready pipeline suite"
type: drill_suite
suite_config:
  template: "@myapp"
  credentials:
    providers: myapp-providers
  credential_injection:
    files:
      - credential: providers
        path: /root/myapp/config/providers.yaml
        field: value
        mode: "0600"
    env:
      - credential: providers
        var: PROVIDER_TOKEN
        field: token
  configure:
    - "test -f /root/myapp/config/providers.yaml"
  setup:
    - "bash /root/myapp/.astonish/start-services.sh"
`
	var cfg config.AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sc := cfg.SuiteConfig
	if sc == nil {
		t.Fatal("SuiteConfig nil")
	}
	if sc.Credentials["providers"] != "myapp-providers" {
		t.Fatalf("credentials = %#v", sc.Credentials)
	}
	if sc.CredentialInjection == nil || len(sc.CredentialInjection.Files) != 1 {
		t.Fatalf("injection = %#v", sc.CredentialInjection)
	}
	if sc.CredentialInjection.Files[0].Path != "/root/myapp/config/providers.yaml" {
		t.Fatalf("file path = %q", sc.CredentialInjection.Files[0].Path)
	}
	if len(sc.Configure) != 1 || sc.Configure[0] != "test -f /root/myapp/config/providers.yaml" {
		t.Fatalf("configure = %#v", sc.Configure)
	}
}

func TestSpecFromSuiteAndFleetFallback(t *testing.T) {
	t.Parallel()
	sc := &config.DrillSuiteConfig{
		Template: "@app",
		Credentials: map[string]string{
			"providers": "app-providers",
		},
		CredentialInjection: &config.SuiteCredentialInjection{
			Files: []config.SuiteFileInjection{{
				Credential: "providers",
				Path:       "/root/app/creds.yaml",
				Field:      "value",
			}},
		},
	}
	spec, err := SpecFromSuite("app", sc)
	if err != nil {
		t.Fatal(err)
	}
	if spec == nil || !spec.HasWork() || spec.Injection.Files[0].Path != "/root/app/creds.yaml" {
		t.Fatalf("spec = %#v", spec)
	}

	bad := &config.DrillSuiteConfig{
		Credentials: map[string]string{"a": "store-a"},
		CredentialInjection: &config.SuiteCredentialInjection{
			Env: []config.SuiteEnvInjection{{Credential: "missing", Var: "X", Field: "token"}},
		},
	}
	if _, err := SpecFromSuite("bad", bad); err == nil {
		t.Fatal("expected validation error for missing logical credential")
	}

	plan := &fleet.FleetPlan{
		Key: "juicytrade",
		Credentials: map[string]string{
			"providers": "jt-providers",
		},
		CredentialInjection: &fleet.CredentialInjection{
			Files: []fleet.FileInjection{{
				Credential: "providers",
				Path:       "/root/jt/providers.yaml",
				Field:      "value",
			}},
		},
	}
	fromPlan := SpecFromFleetPlan(plan)
	if fromPlan == nil || fromPlan.OwnerKey != "juicytrade" {
		t.Fatalf("fromPlan = %#v", fromPlan)
	}

	ps := &fakePlanStore{plans: map[string]*fleet.FleetPlan{"juicytrade": plan}}
	emptySuite := &config.DrillSuiteConfig{Template: "@juicytrade"}
	resolved, err := ResolveInjectionSpec(context.Background(), "juicytrade", emptySuite, ps)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.Injection.Files[0].Path != "/root/jt/providers.yaml" {
		t.Fatalf("fallback = %#v", resolved)
	}
}

func TestApplyCredentialInjectionMissingFailClosed(t *testing.T) {
	t.Parallel()
	spec := &InjectionSpec{
		OwnerKey: "s",
		Credentials: map[string]string{
			"providers": "missing-store-entry",
		},
		Injection: &fleet.CredentialInjection{
			Files: []fleet.FileInjection{{
				Credential: "providers",
				Path:       "/tmp/x.yaml",
				Field:      "value",
			}},
		},
	}
	cs := &memCredStore{creds: map[string]*store.Credential{}}
	_, err := ApplyCredentialInjection(context.Background(), spec, cs, nil, InjectionTarget{
		ExecIncus: func(command []string, env map[string]string) ([]byte, []byte, int, error) {
			t.Fatal("should not exec when credential missing")
			return nil, nil, 0, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
}

func TestApplyCredentialInjectionWritesFileViaIncusExec(t *testing.T) {
	t.Parallel()
	spec := &InjectionSpec{
		OwnerKey: "s",
		Credentials: map[string]string{
			"providers": "providers-store",
		},
		Injection: &fleet.CredentialInjection{
			Files: []fleet.FileInjection{{
				Credential: "providers",
				Path:       "/root/app/providers.yaml",
				Field:      "value",
			}},
		},
	}
	cs := &memCredStore{creds: map[string]*store.Credential{
		"providers-store": {Type: store.CredAPIKey, Value: "secret-yaml-body"},
	}}
	var sawCmd string
	_, err := ApplyCredentialInjection(context.Background(), spec, cs, nil, InjectionTarget{
		ExecIncus: func(command []string, env map[string]string) ([]byte, []byte, int, error) {
			if len(command) >= 3 {
				sawCmd = command[2]
			}
			return nil, nil, 0, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawCmd, "/root/app/providers.yaml") {
		t.Fatalf("cmd = %q", sawCmd)
	}
	if !strings.Contains(sawCmd, base64.StdEncoding.EncodeToString([]byte("secret-yaml-body"))) {
		t.Fatalf("expected base64 content in cmd: %q", sawCmd)
	}
}

func TestRunSuiteConfigureRunsBeforeSetup(t *testing.T) {
	t.Parallel()
	// Thin runner does not execute configure/setup — they are instruction sources only.
	executor := newMockExecutor()
	runner := NewSuiteRunner(executor, nil, false)
	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
				Configure: []string{"echo configure"},
				Setup:     []string{"echo setup"},
			},
		},
	}
	report, _ := runner.RunSuite(context.Background(), suite, nil)
	if report.Status != "passed" {
		t.Fatalf("got %q", report.Status)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("expected no configure/setup execution, got %+v", executor.calls)
	}
}

type memCredStore struct {
	creds map[string]*store.Credential
}

func (m *memCredStore) Get(_ context.Context, name string) *store.Credential { return m.creds[name] }
func (m *memCredStore) Set(_ context.Context, name string, cred *store.Credential) error {
	m.creds[name] = cred
	return nil
}
func (m *memCredStore) Remove(_ context.Context, name string) error {
	delete(m.creds, name)
	return nil
}
func (m *memCredStore) List(_ context.Context) map[string]store.CredentialType {
	out := make(map[string]store.CredentialType, len(m.creds))
	for k, v := range m.creds {
		out[k] = v.Type
	}
	return out
}
func (m *memCredStore) Count(_ context.Context) int { return len(m.creds) }
func (m *memCredStore) Resolve(_ context.Context, name string) (string, string, error) {
	return store.ResolveCredentialHeader(name, m.Get(context.Background(), name), nil)
}
func (m *memCredStore) InvalidateToken(_ context.Context, _ string)                {}
func (m *memCredStore) SetSecret(_ context.Context, _, _ string) error              { return nil }
func (m *memCredStore) SetSecretBatch(_ context.Context, _ map[string]string) error { return nil }
func (m *memCredStore) GetSecret(_ context.Context, _ string) string                { return "" }
func (m *memCredStore) RemoveSecret(_ context.Context, _ string) error              { return nil }
func (m *memCredStore) HasSecrets(_ context.Context) bool                           { return false }
func (m *memCredStore) SecretCount(_ context.Context) int                           { return 0 }
func (m *memCredStore) ListSecrets(_ context.Context) []string                      { return nil }
func (m *memCredStore) Reload(_ context.Context) error                              { return nil }

type fakePlanStore struct {
	plans map[string]*fleet.FleetPlan
}

func (f *fakePlanStore) GetPlan(_ context.Context, key string) (any, bool) {
	p, ok := f.plans[key]
	return p, ok
}
func (f *fakePlanStore) ListPlans(_ context.Context) []store.FleetPlanSummary {
	out := make([]store.FleetPlanSummary, 0, len(f.plans))
	for k, p := range f.plans {
		out = append(out, store.FleetPlanSummary{Key: k, Name: p.Name})
	}
	return out
}
func (f *fakePlanStore) Save(_ context.Context, _ any) error                     { return nil }
func (f *fakePlanStore) Delete(_ context.Context, _ string) error                 { return nil }
func (f *fakePlanStore) Count(_ context.Context) int                              { return len(f.plans) }
func (f *fakePlanStore) Reload(_ context.Context) error                           { return nil }
func (f *fakePlanStore) GetPlanYAML(_ context.Context, _ string) (string, error)  { return "", nil }
func (f *fakePlanStore) SavePlanYAML(_ context.Context, _, _ string) error        { return nil }
