package sandbox

import (
	"encoding/json"
	"testing"
)

// TestParseNodeResponse_CleanOutput verifies normal NDJSON protocol parsing.
func TestParseNodeResponse_CleanOutput(t *testing.T) {
	stdout := []byte("{\"ready\":true}\n{\"id\":\"1\",\"result\":{\"output\":\"hello\"}}\n")
	result, err := parseNodeResponse(stdout, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed.Output != "hello" {
		t.Errorf("result.output = %q, want %q", parsed.Output, "hello")
	}
}

// TestParseNodeResponse_ANSIBannerPrefix is the regression test for the
// "new version available" ANSI banner that corrupted the NDJSON stream.
// The decoder must skip the non-JSON lines and find the response.
func TestParseNodeResponse_ANSIBannerPrefix(t *testing.T) {
	// Exact reproduction of what the sandbox-base binary emitted.
	stdout := []byte(
		"\x1b[93mA new version of Astonish is available: v2.9.1\x1b[0m\n" +
			"\x1b[93mRun \x1b[1mbrew upgrade schardosin/astonish/astonish\x1b[0m\x1b[93m to update.\x1b[0m\n" +
			"\n" +
			"{\"ready\":true}\n" +
			"{\"id\":\"1\",\"result\":{\"content\":\"file contents here\"}}\n",
	)
	result, err := parseNodeResponse(stdout, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed.Content != "file contents here" {
		t.Errorf("result.content = %q, want %q", parsed.Content, "file contents here")
	}
}

// TestParseNodeResponse_ErrorFromNode verifies that node-side errors are
// propagated correctly.
func TestParseNodeResponse_ErrorFromNode(t *testing.T) {
	stdout := []byte("{\"ready\":true}\n{\"id\":\"1\",\"error\":\"file not found\"}\n")
	_, err := parseNodeResponse(stdout, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "node: file not found" {
		t.Errorf("error = %q, want %q", got, "node: file not found")
	}
}

// TestParseNodeResponse_NoResponse verifies proper error when stdout has no
// valid NDJSON response (e.g. the node process crashed before emitting one).
func TestParseNodeResponse_NoResponse(t *testing.T) {
	stdout := []byte("some random output\nnothing useful\n")
	_, err := parseNodeResponse(stdout, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !contains(got, "no node response") {
		t.Errorf("error = %q, want contains %q", got, "no node response")
	}
	if got := err.Error(); !contains(got, "exit=1") {
		t.Errorf("error = %q, want contains %q", got, "exit=1")
	}
}

// TestParseNodeResponse_GarbageBetweenLines verifies robustness when garbage
// lines are interspersed between valid NDJSON lines.
func TestParseNodeResponse_GarbageBetweenLines(t *testing.T) {
	stdout := []byte(
		"locale: cannot set LC_ALL\n" +
			"{\"ready\":true}\n" +
			"WARNING: something something\n" +
			"{\"id\":\"42\",\"result\":{\"ok\":true}}\n" +
			"trailing garbage\n",
	)
	result, err := parseNodeResponse(stdout, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !parsed.OK {
		t.Error("result.ok = false, want true")
	}
}

// TestParseNodeResponse_EmptyStdout verifies error on empty output.
func TestParseNodeResponse_EmptyStdout(t *testing.T) {
	_, err := parseNodeResponse(nil, 137)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !contains(got, "exit=137") {
		t.Errorf("error = %q, want contains %q", got, "exit=137")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
