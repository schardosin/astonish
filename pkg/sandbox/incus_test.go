package sandbox

import (
	"testing"
)

func TestSanitizeInstanceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple alphanumeric", "abc123", "abc123"},
		{"uppercase converted", "ABC123", "abc123"},
		{"uuid passthrough", "a2a5b479-b03a-4662", "a2a5b479-b03a-4662"},
		{"colons replaced", "email:direct:user", "email-direct-user"},
		{"email address", "rafael.schardosin@sap.com", "rafael-schardosin-sap-com"},
		{"consecutive special chars collapsed", "a::b@@c", "a-b-c"},
		{"leading special chars stripped", "::abc", "abc"},
		{"trailing special chars stripped", "abc::", "abc"},
		{"mixed special chars", "email:direct:user@domain.com", "email-direct-user-domain-com"},
		{"empty string", "", ""},
		{"only special chars", ":::@@@...", ""},
		{"hyphens preserved", "my-session-id", "my-session-id"},
		{"spaces replaced", "hello world", "hello-world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeInstanceName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeInstanceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSessionContainerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		want      string
	}{
		{
			"uuid session (studio)",
			"a2a5b479-b03a-4662-96aa-40aa8bade469",
			// sanitized: a2a5b479-b03a-4662-96aa-40aa8bade469 (36 chars, fits in 40)
			"astn-sess-a2a5b479-b03a-4662-96aa-40aa8bade469",
		},
		{
			"email channel session",
			"email:direct:rafael.schardosin@sap.com",
			// sanitized: email-direct-rafael-schardosin-sap-com (38 chars, fits in 40)
			"astn-sess-email-direct-rafael-schardosin-sap-com",
		},
		{
			"telegram channel session",
			"telegram:direct:8484406081",
			// sanitized: telegram-direct-8484406081 (26 chars, fits in 40)
			"astn-sess-telegram-direct-8484406081",
		},
		{
			"short session ID",
			"test",
			"astn-sess-test",
		},
		{
			"only invalid chars",
			":::@@@",
			"astn-sess-unknown",
		},
		{
			"long email truncated",
			"email:direct:a-very-long-username-that-exceeds-the-maximum@example.com",
			// sanitized: email-direct-a-very-long-username-that-exceeds-the-maximum-example-com
			// [:40] = "email-direct-a-very-long-username-that-e" → no trailing hyphen
			"astn-sess-email-direct-a-very-long-username-that-e",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SessionContainerName(tt.sessionID)
			if got != tt.want {
				t.Errorf("SessionContainerName(%q) = %q, want %q", tt.sessionID, got, tt.want)
			}
			// Verify the name only contains valid characters
			for _, r := range got {
				if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
					t.Errorf("SessionContainerName(%q) contains invalid character %q in result %q", tt.sessionID, string(r), got)
				}
			}
			// Verify it doesn't end with a hyphen
			if len(got) > 0 && got[len(got)-1] == '-' {
				t.Errorf("SessionContainerName(%q) = %q ends with hyphen", tt.sessionID, got)
			}
		})
	}
}

func TestSessionContainerNameDeterministic(t *testing.T) {
	t.Parallel()
	id := "email:direct:rafael.schardosin@sap.com"
	name1 := SessionContainerName(id)
	name2 := SessionContainerName(id)
	if name1 != name2 {
		t.Errorf("SessionContainerName is not deterministic: %q != %q", name1, name2)
	}
}

func TestSessionContainerNameDistinct(t *testing.T) {
	t.Parallel()
	// Different email senders should get different container names
	name1 := SessionContainerName("email:direct:alice@example.com")
	name2 := SessionContainerName("email:direct:bob@example.com")
	if name1 == name2 {
		t.Errorf("different session IDs produced same container name: %q", name1)
	}
}

func TestMatchContainerToSession(t *testing.T) {
	t.Parallel()

	sessions := map[string]bool{
		"a2a5b479-b03a-4662-96aa-40aa8bade469":   true,
		"email:direct:rafael.schardosin@sap.com": true,
		"telegram:direct:8484406081":             true,
	}

	tests := []struct {
		name          string
		containerName string
		want          string
	}{
		{
			"matches uuid session",
			SessionContainerName("a2a5b479-b03a-4662-96aa-40aa8bade469"),
			"a2a5b479-b03a-4662-96aa-40aa8bade469",
		},
		{
			"matches email session",
			SessionContainerName("email:direct:rafael.schardosin@sap.com"),
			"email:direct:rafael.schardosin@sap.com",
		},
		{
			"matches telegram session",
			SessionContainerName("telegram:direct:8484406081"),
			"telegram:direct:8484406081",
		},
		{
			"no match for unknown container",
			"astn-sess-unknown-session-id",
			"",
		},
		{
			"no match for fleet container",
			"astn-fleet-plan-agent",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchContainerToSession(tt.containerName, sessions)
			if got != tt.want {
				t.Errorf("matchContainerToSession(%q) = %q, want %q", tt.containerName, got, tt.want)
			}
		})
	}
}

func TestFleetContainerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		planKey  string
		agentKey string
		taskSlug string
		wantPfx  string
	}{
		{"basic fleet name", "astonish", "researcher", "", "astn-fleet-astonish-researcher"},
		{"with task slug", "plan", "agent", "task1", "astn-fleet-plan-agent-task1"},
		{"special chars sanitized", "my:plan", "my@agent", "", "astn-fleet-my-plan-my-agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FleetContainerName(tt.planKey, tt.agentKey, tt.taskSlug)
			if got != tt.wantPfx {
				t.Errorf("FleetContainerName(%q, %q, %q) = %q, want %q",
					tt.planKey, tt.agentKey, tt.taskSlug, got, tt.wantPfx)
			}
			// Verify valid characters
			for _, r := range got {
				if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
					t.Errorf("FleetContainerName result %q contains invalid character %q", got, string(r))
				}
			}
		})
	}
}
