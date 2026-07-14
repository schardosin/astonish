package drill

import (
	"strings"
	"testing"
)

func TestClassifyKnownFailure_BrowserStackTransient(t *testing.T) {
	cases := []string{
		"failed to resolve CDP URL at 10.0.0.1:9222 after 15s",
		"Browser CDP connection is dead (closed pipe), reconnecting...",
		"sandbox is not ready",
		"browser preflight failed (in-container Chromium/CDP not ready): boom",
		"CloakBrowser started but DevTools port 9223 is not listening after 5s",
	}
	for _, msg := range cases {
		v := ClassifyKnownFailure(StepResult{Error: msg, Tool: "browser_navigate"})
		if v == nil {
			t.Fatalf("expected classification for %q", msg)
		}
		if v.Classification != "transient" || !v.Retry {
			t.Fatalf("got %+v for %q", v, msg)
		}
	}
}

func TestClassifyKnownFailure_ServiceDeathEnvironment(t *testing.T) {
	msg := "navigation failed: net::ERR_CONNECTION_REFUSED (service not answering at http://127.0.0.1:3001/ — app/frontend likely died after ready_check; restore via start-services.sh. This is not a browser stack failure)"
	v := ClassifyKnownFailure(StepResult{Error: msg, Tool: "browser_navigate"})
	if v == nil {
		t.Fatal("expected classification")
	}
	if v.Classification != "environment" || v.Retry {
		t.Fatalf("got %+v", v)
	}
	if !strings.Contains(strings.ToLower(v.Recommendation), "start-services") {
		t.Fatalf("recommendation = %q", v.Recommendation)
	}
}

func TestClassifyKnownFailure_UnknownNil(t *testing.T) {
	if v := ClassifyKnownFailure(StepResult{Error: `expected "ok" but got "nope"`, Tool: "shell_command"}); v != nil {
		t.Fatalf("expected nil, got %+v", v)
	}
}
