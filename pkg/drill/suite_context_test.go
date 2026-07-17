package drill

import (
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
)

func TestIsTutorialSuite(t *testing.T) {
	regular := &LoadedSuite{
		Name: "juicytrade",
		Config: &config.AgentConfig{Description: "regular"},
		Tests: []LoadedTest{
			{Name: "health", Config: &config.AgentConfig{
				DrillConfig: &config.DrillConfig{Tags: []string{"smoke"}},
			}},
		},
	}
	if IsTutorialSuite(regular) {
		t.Fatal("regular suite should not be tutorial")
	}

	byMode := &LoadedSuite{
		Name: "juicytrade-tutorial",
		Config: &config.AgentConfig{},
		Tests: []LoadedTest{
			{Name: "overview", Config: &config.AgentConfig{
				DrillConfig: &config.DrillConfig{Mode: "tutorial", Tags: []string{"tutorial"}},
			}},
		},
	}
	if !IsTutorialSuite(byMode) {
		t.Fatal("mode=tutorial should classify as tutorial suite")
	}

	byTag := &LoadedSuite{
		Name: "legacy-tut",
		Config: &config.AgentConfig{},
		Tests: []LoadedTest{
			{Name: "walk", Config: &config.AgentConfig{
				DrillConfig: &config.DrillConfig{Tags: []string{"tutorial"}},
			}},
		},
	}
	if !IsTutorialSuite(byTag) {
		t.Fatal("tutorial tag should classify as tutorial suite")
	}
}

func TestBuildSuiteContext_InfraAndTutorialFlag(t *testing.T) {
	suite := &LoadedSuite{
		Name: "juicytrade-tutorial",
		File: "juicytrade-tutorial.yaml",
		Config: &config.AgentConfig{
			Description: "Tutorial videos",
			SuiteConfig: &config.DrillSuiteConfig{
				Template:  "juicytrade",
				Workspace: "/root/juicytrade",
				Branch:    "main",
				BaseURL:   "http://localhost:5173",
				Setup:     []string{"bash /root/juicytrade/.astonish/start-services.sh"},
				Configure: []string{"echo prep"},
				ReadyCheck: &config.ReadyCheck{
					Type: "http",
					URL:  "http://localhost:5173/health",
				},
				Credentials: map[string]string{"tradier": "tradier-paper"},
				CredentialInjection: &config.SuiteCredentialInjection{
					Env: []config.SuiteEnvInjection{
						{Credential: "tradier", Var: "TRADIER_TOKEN", Field: "token"},
					},
					Files: []config.SuiteFileInjection{
						{Credential: "tradier", Path: "/root/.config/tradier.json", Field: "json"},
					},
				},
				Services: []config.ServiceConfig{
					{Name: "frontend", Setup: "npx vite"},
				},
			},
		},
		Tests: []LoadedTest{
			{Name: "overview", Config: &config.AgentConfig{
				Description: "Product overview",
				DrillConfig: &config.DrillConfig{Mode: "tutorial", Tags: []string{"tutorial"}},
				Nodes: []config.Node{
					{Name: "dash", Args: map[string]any{"tool": "browser_click"}},
				},
			}},
		},
	}

	got := BuildSuiteContext(suite)
	for _, want := range []string{
		"TutorialSuite: yes",
		"Template: juicytrade",
		"Workspace: /root/juicytrade",
		"Branch: main",
		"Setup:",
		"start-services.sh",
		"Configure:",
		"ReadyCheck: http http://localhost:5173/health",
		"tradier → tradier-paper",
		"TRADIER_TOKEN",
		"/root/.config/tradier.json",
		"Mode: tutorial",
		"REUSE: Do not change template/setup/credentials",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuildSuiteContext_RegularSuite(t *testing.T) {
	suite := &LoadedSuite{
		Name: "juicytrade",
		File: "juicytrade.yaml",
		Config: &config.AgentConfig{
			Description: "Smoke",
			SuiteConfig: &config.DrillSuiteConfig{Template: "juicytrade"},
		},
		Tests: []LoadedTest{
			{Name: "health", Config: &config.AgentConfig{
				Description: "ping",
				DrillConfig: &config.DrillConfig{Tags: []string{"smoke"}},
			}},
		},
	}
	got := BuildSuiteContext(suite)
	if !strings.Contains(got, "TutorialSuite: no") {
		t.Fatalf("want TutorialSuite: no, got:\n%s", got)
	}
	if strings.Contains(got, "REUSE:") {
		t.Fatal("REUSE banner should only appear for tutorial suites")
	}
}
