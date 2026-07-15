package drill

import (
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

func TestGenerateRunInstructions_Override(t *testing.T) {
	sc := &config.DrillSuiteConfig{
		RunInstructions: "Custom prep only.",
	}
	got := GenerateRunInstructions("myapp", sc)
	if !strings.Contains(got, "Custom prep only.") {
		t.Fatalf("override missing: %q", got)
	}
	if !strings.Contains(got, `run_drill(suite_name: "myapp")`) {
		t.Fatalf("should append run_drill reminder: %q", got)
	}
}

func TestGenerateRunInstructions_Auto(t *testing.T) {
	sc := &config.DrillSuiteConfig{
		Template:  "juicytrade",
		Workspace: "/root/juicytrade",
		Branch:    "main",
		Credentials: map[string]string{
			"providers": "juicytrade-providers",
		},
		CredentialInjection: &config.SuiteCredentialInjection{
			Files: []config.SuiteFileInjection{
				{Credential: "providers", Path: "/root/juicytrade/config/providers.yaml", Field: "value"},
			},
		},
		Setup: []string{`bash /root/juicytrade/.astonish/start-services.sh`},
		ReadyCheck: &config.ReadyCheck{
			Type:    "http",
			URL:     "http://localhost:8008/health",
			Timeout: 60,
		},
	}
	got := GenerateRunInstructions("juicytrade", sc)
	for _, want := range []string{
		`use_sandbox_template(template_name: "juicytrade")`,
		`cd /root/juicytrade && git fetch && git checkout main && git pull --ff-only`,
		`inject_drill_credentials(suite_name: "juicytrade")`,
		`bash /root/juicytrade/.astonish/start-services.sh`,
		"http://localhost:8008/health",
		`run_drill(suite_name: "juicytrade")`,
		"does not switch templates",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "force=true") || strings.Contains(got, "force: true") {
		t.Errorf("must not mention force flag: %s", got)
	}

	injectIdx := strings.Index(got, "inject_drill_credentials")
	startIdx := strings.Index(got, "start-services.sh")
	if injectIdx < 0 || startIdx < 0 || injectIdx > startIdx {
		t.Fatalf("inject_drill_credentials must appear before start-services; inject=%d start=%d\n%s", injectIdx, startIdx, got)
	}
}

func TestGenerateRunInstructions_NoCredentialsOmitsInject(t *testing.T) {
	sc := &config.DrillSuiteConfig{
		Template: "plain",
		Setup:    []string{`echo start`},
	}
	got := GenerateRunInstructions("plain", sc)
	if strings.Contains(got, "inject_drill_credentials") {
		t.Fatalf("should omit inject when no credentials: %s", got)
	}
}

func TestSuiteDeclaresCredentials(t *testing.T) {
	if SuiteDeclaresCredentials(nil) {
		t.Fatal("nil")
	}
	if SuiteDeclaresCredentials(&config.DrillSuiteConfig{}) {
		t.Fatal("empty")
	}
	if !SuiteDeclaresCredentials(&config.DrillSuiteConfig{Credentials: map[string]string{"a": "b"}}) {
		t.Fatal("credentials map")
	}
	if !SuiteDeclaresCredentials(&config.DrillSuiteConfig{
		CredentialInjection: &config.SuiteCredentialInjection{
			Env: []config.SuiteEnvInjection{{Credential: "a", Var: "B"}},
		},
	}) {
		t.Fatal("credential_injection env")
	}
}

func TestGenerateRunInstructions_NilConfig(t *testing.T) {
	got := GenerateRunInstructions("alone", nil)
	if !strings.Contains(got, `run_drill(suite_name: "alone")`) {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "inject_drill_credentials") {
		t.Fatalf("nil config should not inject: %q", got)
	}
}
