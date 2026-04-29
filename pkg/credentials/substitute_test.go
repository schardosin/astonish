package credentials

import (
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestFormatPlaceholder(t *testing.T) {
	got := FormatPlaceholder("my-ssh", "password")
	want := "{{CREDENTIAL:my-ssh:password}}"
	if got != want {
		t.Errorf("FormatPlaceholder = %q, want %q", got, want)
	}
}

func TestContainsPlaceholder(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello world", false},
		{"{{CREDENTIAL:my-ssh:password}}", true},
		{"prefix {{CREDENTIAL:x:token}} suffix", true},
		{"{{CREDENTIAL:}}", false},   // missing field
		{"{{CREDENTIAL:a:}}", false}, // empty field
		{"multiple {{CREDENTIAL:a:password}} and {{CREDENTIAL:b:token}}", true},
	}

	for _, tt := range tests {
		if got := ContainsPlaceholder(tt.input); got != tt.want {
			t.Errorf("ContainsPlaceholder(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSubstitutePlaceholders_NilStore(t *testing.T) {
	input := "{{CREDENTIAL:my-ssh:password}}"
	got := SubstitutePlaceholders(input, nil)
	if got != input {
		t.Errorf("SubstitutePlaceholders with nil store should return input unchanged, got %q", got)
	}
}

func TestSubstitutePlaceholders_NoPlaceholders(t *testing.T) {
	// Create a minimal store (in-memory, no encryption)
	input := "hello world"
	got := SubstitutePlaceholders(input, nil)
	if got != input {
		t.Errorf("SubstitutePlaceholders with no placeholders should return input unchanged, got %q", got)
	}
}

func TestSubstituteMap_NilInputs(t *testing.T) {
	// Nil map
	got := SubstituteMap(nil, nil)
	if got != nil {
		t.Errorf("SubstituteMap(nil, nil) should return nil")
	}

	// Nil store
	m := map[string]any{"key": "{{CREDENTIAL:x:password}}"}
	got = SubstituteMap(m, nil)
	if got["key"] != "{{CREDENTIAL:x:password}}" {
		t.Errorf("SubstituteMap with nil store should return unchanged, got %v", got)
	}
}

func TestSubstituteMap_NoPlaceholders(t *testing.T) {
	m := map[string]any{"key": "hello", "nested": map[string]any{"k": "world"}}
	got := SubstituteMap(m, nil)
	// Should return the same map reference (no copy)
	if got["key"] != "hello" {
		t.Errorf("SubstituteMap with no placeholders should return input unchanged")
	}
}

func TestResolveField_UnknownCredential(t *testing.T) {
	// resolveField with unknown credential should return fallback
	fallback := "{{CREDENTIAL:nonexistent:password}}"
	got := resolveField(nil, "nonexistent", "password", fallback)
	if got != fallback {
		t.Errorf("resolveField with nil store = %q, want %q", got, fallback)
	}
}

func TestPlaceholderRegex(t *testing.T) {
	tests := []struct {
		input   string
		matches [][]string // each match: [full, name, field]
	}{
		{
			"{{CREDENTIAL:my-ssh:password}}",
			[][]string{{"{{CREDENTIAL:my-ssh:password}}", "my-ssh", "password"}},
		},
		{
			"prefix {{CREDENTIAL:api:token}} middle {{CREDENTIAL:db:password}} suffix",
			[][]string{
				{"{{CREDENTIAL:api:token}}", "api", "token"},
				{"{{CREDENTIAL:db:password}}", "db", "password"},
			},
		},
		{
			"{{CREDENTIAL:oauth-app:client_secret}}",
			[][]string{{"{{CREDENTIAL:oauth-app:client_secret}}", "oauth-app", "client_secret"}},
		},
	}

	for _, tt := range tests {
		matches := credentialPlaceholderRe.FindAllStringSubmatch(tt.input, -1)
		if len(matches) != len(tt.matches) {
			t.Errorf("regex on %q: got %d matches, want %d", tt.input, len(matches), len(tt.matches))
			continue
		}
		for i, m := range matches {
			if m[0] != tt.matches[i][0] || m[1] != tt.matches[i][1] || m[2] != tt.matches[i][2] {
				t.Errorf("regex on %q match %d: got %v, want %v", tt.input, i, m, tt.matches[i])
			}
		}
	}
}

func TestSubstituteAndRestore_NilStore(t *testing.T) {
	m := map[string]any{"input": "{{CREDENTIAL:x:password}}"}
	restore := SubstituteAndRestore(m, nil)
	// Should be a no-op, map unchanged
	if m["input"] != "{{CREDENTIAL:x:password}}" {
		t.Errorf("nil store should leave map unchanged, got %q", m["input"])
	}
	restore() // no-op, should not panic
}

func TestSubstituteAndRestore_NilMap(t *testing.T) {
	restore := SubstituteAndRestore(nil, nil)
	restore() // no-op, should not panic
}

func TestSubstituteAndRestore_NoPlaceholders(t *testing.T) {
	m := map[string]any{"key": "hello", "num": 42}
	restore := SubstituteAndRestore(m, nil)
	if m["key"] != "hello" {
		t.Errorf("no placeholders should leave map unchanged")
	}
	restore() // no-op
}

func TestSubstituteAndRestore_WithStore(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store.Set("my-ssh", &Credential{
		Type:     CredPassword,
		Username: "root",
		Password: "secret123",
	})

	// Build args map simulating what ADK would pass (shared by reference
	// with the session event).
	args := map[string]any{
		"input":      "{{CREDENTIAL:my-ssh:password}}\n",
		"session_id": "abc123",
	}

	// Step 1: BeforeToolCallback — substitute placeholders.
	restore := SubstituteAndRestore(args, store)

	// After substitution, the tool should see the real value.
	if args["input"] != "secret123\n" {
		t.Errorf("after substitute, input = %q, want %q", args["input"], "secret123\n")
	}
	// Non-placeholder keys should be unchanged.
	if args["session_id"] != "abc123" {
		t.Errorf("session_id should be unchanged, got %q", args["session_id"])
	}

	// Step 2: AfterToolCallback — restore placeholders.
	restore()

	// After restore, the args map should have the original placeholders.
	if args["input"] != "{{CREDENTIAL:my-ssh:password}}\n" {
		t.Errorf("after restore, input = %q, want %q", args["input"], "{{CREDENTIAL:my-ssh:password}}\n")
	}
	if args["session_id"] != "abc123" {
		t.Errorf("session_id should still be unchanged, got %q", args["session_id"])
	}
}

func TestSubstituteAndRestore_MultipleKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store.Set("db", &Credential{
		Type:     CredPassword,
		Username: "admin",
		Password: "dbpass",
	})
	store.Set("api", &Credential{
		Type:  CredBearer,
		Token: "tok-abc",
	})

	args := map[string]any{
		"password": "{{CREDENTIAL:db:password}}",
		"token":    "{{CREDENTIAL:api:token}}",
		"host":     "localhost",
	}

	restore := SubstituteAndRestore(args, store)

	if args["password"] != "dbpass" {
		t.Errorf("password = %q, want %q", args["password"], "dbpass")
	}
	if args["token"] != "tok-abc" {
		t.Errorf("token = %q, want %q", args["token"], "tok-abc")
	}
	if args["host"] != "localhost" {
		t.Errorf("host should be unchanged")
	}

	restore()

	if args["password"] != "{{CREDENTIAL:db:password}}" {
		t.Errorf("after restore, password = %q, want placeholder", args["password"])
	}
	if args["token"] != "{{CREDENTIAL:api:token}}" {
		t.Errorf("after restore, token = %q, want placeholder", args["token"])
	}
}

func TestSubstituteAndRestore_RestoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store.Set("x", &Credential{Type: CredPassword, Username: "u", Password: "p"})

	args := map[string]any{"input": "{{CREDENTIAL:x:password}}"}
	restore := SubstituteAndRestore(args, store)
	restore()
	restore() // calling twice should be safe

	if args["input"] != "{{CREDENTIAL:x:password}}" {
		t.Errorf("double restore should still have placeholder, got %q", args["input"])
	}
}

// TestSubstituteAndRestore_ParallelCalls verifies that using per-call restores
// (as opposed to a shared variable) correctly handles concurrent tool calls.
// This test reproduces the race condition that occurred when ADK dispatched
// parallel tool calls — a shared credentialRestore variable could be overwritten
// by a concurrent goroutine, causing placeholders to leak into tool execution.
func TestSubstituteAndRestore_ParallelCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store.Set("proxmox", &Credential{Type: CredAPIKey, Header: "Authorization", Value: "PVEAPIToken=root@pam!astonish=secret123"})

	// Simulate two parallel tool calls — one with a credential placeholder,
	// one without. This is the exact pattern from session 1b919508 where
	// search_flows + shell_command ran in parallel.
	const iterations = 100
	for i := 0; i < iterations; i++ {
		var restoreFuncs sync.Map

		argsWithCred := map[string]any{
			"command": `curl -sk -H "Authorization: {{CREDENTIAL:proxmox:value}}" "https://proxmox.local/api2/json/status"`,
		}
		argsNoCred := map[string]any{
			"query": "proxmox memory",
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// Goroutine 1: tool with credential placeholder (like shell_command)
		go func() {
			defer wg.Done()
			callID := "call-with-cred"
			restore := SubstituteAndRestore(argsWithCred, store)
			restoreFuncs.Store(callID, restore)

			// Verify substitution happened (real secret visible)
			cmd := argsWithCred["command"].(string)
			if ContainsPlaceholder(cmd) {
				t.Errorf("iteration %d: credential not substituted in goroutine 1, got %q", i, cmd)
			}
			if !strings.Contains(cmd, "PVEAPIToken=root@pam!astonish=secret123") {
				t.Errorf("iteration %d: expected real token in command, got %q", i, cmd)
			}

			// Simulate tool execution delay
			runtime.Gosched()

			// Restore
			if fn, ok := restoreFuncs.LoadAndDelete(callID); ok {
				fn.(func())()
			}
		}()

		// Goroutine 2: tool without credential placeholder (like search_flows)
		go func() {
			defer wg.Done()
			callID := "call-no-cred"
			restore := SubstituteAndRestore(argsNoCred, store)
			restoreFuncs.Store(callID, restore)

			// This should be a no-op restore
			runtime.Gosched()

			if fn, ok := restoreFuncs.LoadAndDelete(callID); ok {
				fn.(func())()
			}
		}()

		wg.Wait()

		// After both goroutines complete and restore, the credential args should have
		// the placeholder back, not the real secret.
		cmd := argsWithCred["command"].(string)
		if !ContainsPlaceholder(cmd) {
			t.Fatalf("iteration %d: placeholder not restored after parallel calls, got %q", i, cmd)
		}
		if strings.Contains(cmd, "secret123") {
			t.Fatalf("iteration %d: real secret leaked into restored args", i)
		}
	}
}
