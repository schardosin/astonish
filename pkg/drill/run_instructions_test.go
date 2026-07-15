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
		Setup:     []string{`bash /root/juicytrade/.astonish/start-services.sh`},
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
}

func TestGenerateRunInstructions_NilConfig(t *testing.T) {
	got := GenerateRunInstructions("alone", nil)
	if !strings.Contains(got, `run_drill(suite_name: "alone")`) {
		t.Fatalf("got %q", got)
	}
}
