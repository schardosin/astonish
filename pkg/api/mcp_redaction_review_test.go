package api

import (
	"strings"
	"testing"
)

// TestRedactMCPCommand_SpaceSeparatedFlag covers the review gap: the redaction
// pattern previously only matched --flag=VALUE / --flag:VALUE and missed the
// space-separated form "--api-key VALUE", which is how many MCP servers receive
// tokens on the command line.
func TestRedactMCPCommand_SpaceSeparatedFlag(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak string // substring that must NOT survive
		keep string // substring that must survive (shape preserved)
	}{
		{"equals", "npx server --api-key=sk-secret123", "sk-secret123", "--api-key="},
		{"colon", "npx server --token:tok-secret123", "tok-secret123", "--token:"},
		{"space", "npx server --api-key sk-secret123", "sk-secret123", "--api-key"},
		{"space-password", "run --password hunter2logn", "hunter2logn", "--password"},
		{"no-secret", "npx weather-server --verbose", "", "--verbose"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactMCPCommand(tc.in)
			if tc.leak != "" && strings.Contains(got, tc.leak) {
				t.Fatalf("secret leaked: %q still in %q", tc.leak, got)
			}
			if !strings.Contains(got, tc.keep) {
				t.Fatalf("command shape lost: %q not in %q", tc.keep, got)
			}
		})
	}
}

// TestRedactMCPArgs covers Args redaction in both the single-token and
// split-across-two-elements forms, which the inspector/list responses now apply.
func TestRedactMCPArgs(t *testing.T) {
	got := redactMCPArgs([]string{"server", "--api-key", "sk-splitsecret", "--verbose"})
	for _, a := range got {
		if strings.Contains(a, "sk-splitsecret") {
			t.Fatalf("split secret leaked in args: %v", got)
		}
	}
	// Flag name and unrelated flags preserved.
	if got[1] != "--api-key" || got[3] != "--verbose" {
		t.Fatalf("args shape altered: %v", got)
	}

	inline := redactMCPArgs([]string{"server", "--token=tok-inlinesecret"})
	if strings.Contains(inline[1], "tok-inlinesecret") {
		t.Fatalf("inline secret leaked: %v", inline)
	}

	// Nil / empty is unchanged.
	if redactMCPArgs(nil) != nil {
		t.Fatalf("nil args should be returned unchanged")
	}
}

// TestProviderRedirectRetainsStoredSecret covers the key-redirect gap: a bare
// base_url change that omits api_key (or sends it masked) would send the stored
// key to a new server, and must be rejected; changing base_url with a fresh key
// is allowed; a URL change with no stored secret is allowed.
func TestProviderRedirectRetainsStoredSecret(t *testing.T) {
	stored := map[string]string{"api_key": "sk-stored", "base_url": "https://api.openai.com"}
	masked := maskValue("sk-stored")

	cases := []struct {
		name     string
		existing map[string]string
		incoming map[string]string
		want     bool
	}{
		{
			name:     "omit key, change url -> redirect",
			existing: stored,
			incoming: map[string]string{"base_url": "https://attacker.example"},
			want:     true,
		},
		{
			name:     "masked key, change url -> redirect",
			existing: stored,
			incoming: map[string]string{"api_key": masked, "base_url": "https://attacker.example"},
			want:     true,
		},
		{
			name:     "fresh key, change url -> allowed",
			existing: stored,
			incoming: map[string]string{"api_key": "sk-fresh", "base_url": "https://attacker.example"},
			want:     false,
		},
		{
			name:     "same url, omit key -> allowed",
			existing: stored,
			incoming: map[string]string{"base_url": "https://api.openai.com"},
			want:     false,
		},
		{
			name:     "no stored secret, change url -> allowed",
			existing: map[string]string{"base_url": "https://api.openai.com"},
			incoming: map[string]string{"base_url": "https://attacker.example"},
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerRedirectRetainsStoredSecret(tc.existing, tc.incoming); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMergeProvidersRejectsRedirect proves the shared mergeProviders helper
// (used by the provider GET/PATCH and org/platform paths) rejects a destination
// redirect that retains a stored secret.
func TestMergeProvidersRejectsRedirect(t *testing.T) {
	existing := map[string]map[string]string{
		"openai": {"api_key": "sk-stored", "base_url": "https://api.openai.com"},
	}
	incoming := map[string]map[string]string{
		"openai": {"base_url": "https://attacker.example"},
	}
	if _, err := mergeProviders(existing, incoming); err == nil {
		t.Fatal("expected mergeProviders to reject bare base_url redirect, got nil error")
	}

	// A fresh key alongside the new URL is accepted.
	okIncoming := map[string]map[string]string{
		"openai": {"api_key": "sk-fresh", "base_url": "https://attacker.example"},
	}
	if _, err := mergeProviders(existing, okIncoming); err != nil {
		t.Fatalf("redirect with fresh key should be allowed, got %v", err)
	}
}
