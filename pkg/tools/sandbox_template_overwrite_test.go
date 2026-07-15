package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSaveSandboxTemplateArgs_OverwriteJSON(t *testing.T) {
	b, err := json.Marshal(SaveSandboxTemplateArgs{
		TemplateName: "juicytrade",
		Overwrite:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"overwrite":true`) {
		t.Fatalf("expected overwrite in JSON, got %s", s)
	}
	if !strings.Contains(s, `"template_name":"juicytrade"`) {
		t.Fatalf("expected template_name in JSON, got %s", s)
	}
}
