package tools

import (
	"strings"
	"testing"
)

func TestNewInjectDrillCredentialsTool(t *testing.T) {
	tool, err := NewInjectDrillCredentialsTool(nil, nil, nil)
	if err != nil {
		t.Fatalf("NewInjectDrillCredentialsTool: %v", err)
	}
	if tool.Name() != "inject_drill_credentials" {
		t.Fatalf("name = %q", tool.Name())
	}
	desc := tool.Description()
	for _, want := range []string{"BEFORE", "start-services", "run_drill"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q: %s", want, desc)
		}
	}
}

func TestExecuteInjectDrillCredentials_RequiresSuiteName(t *testing.T) {
	got, err := executeInjectDrillCredentials(nil, &runDrillDeps{}, InjectDrillCredentialsArgs{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Status != "error" || !strings.Contains(got.Message, "suite_name") {
		t.Fatalf("got %+v", got)
	}
}

func TestExecuteInjectDrillCredentials_RequiresPlatformStore(t *testing.T) {
	got, err := executeInjectDrillCredentials(nil, &runDrillDeps{}, InjectDrillCredentialsArgs{SuiteName: "juicytrade"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Status != "error" || !strings.Contains(got.Message, "platform mode") {
		t.Fatalf("got %+v", got)
	}
}
