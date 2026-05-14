package sandbox

import (
	"testing"
)

// ---------------------------------------------------------------------------
// 6.10a: OrgNetworkName tests
// ---------------------------------------------------------------------------

func TestOrgNetworkName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		orgSlug string
		want    string
	}{
		{"short slug", "acme", "org-acme-br0"},
		{"max length slug", "abcdefg", "org-abcdefg-br0"},
		{"slug truncated to fit 15 chars", "engineering", "org-enginee-br0"},
		{"uppercase normalized", "Acme", "org-acme-br0"},
		{"special chars sanitized", "my@org", "org-my-org-br0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OrgNetworkName(tt.orgSlug)
			if got != tt.want {
				t.Errorf("OrgNetworkName(%q) = %q, want %q", tt.orgSlug, got, tt.want)
			}
			if len(got) > 15 {
				t.Errorf("OrgNetworkName(%q) = %q exceeds 15 char limit (%d chars)", tt.orgSlug, got, len(got))
			}
		})
	}
}

func TestOrgNetworkName_LinuxBridgeLimit(t *testing.T) {
	t.Parallel()

	// Even very long slugs must produce names <= 15 chars
	longSlug := "very-long-organization-name-that-exceeds-everything"
	name := OrgNetworkName(longSlug)
	if len(name) > 15 {
		t.Errorf("OrgNetworkName(%q) = %q exceeds 15 char Linux bridge limit (%d chars)",
			longSlug, name, len(name))
	}
	// Must start with "org-" and end with "-br0"
	if name[:4] != "org-" {
		t.Errorf("OrgNetworkName should start with 'org-', got %q", name)
	}
	if name[len(name)-4:] != "-br0" {
		t.Errorf("OrgNetworkName should end with '-br0', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// 6.10b: OrgProfileName tests
// ---------------------------------------------------------------------------

func TestOrgProfileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		orgSlug string
		want    string
	}{
		{"simple slug", "acme", "org-acme"},
		{"uppercase", "Acme", "org-acme"},
		{"special chars", "my.org", "org-my-org"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OrgProfileName(tt.orgSlug)
			if got != tt.want {
				t.Errorf("OrgProfileName(%q) = %q, want %q", tt.orgSlug, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6.10c: OrgSessionContainerName tests
// ---------------------------------------------------------------------------

func TestOrgSessionContainerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		orgSlug   string
		teamSlug  string
		sessionID string
		wantPfx   string // check prefix
		maxLen    int
	}{
		{
			"basic org session",
			"acme", "eng", "sess-123",
			"astn-sess-acme-eng-sess-123", 63,
		},
		{
			"falls back to personal when org empty",
			"", "eng", "sess-123",
			"astn-sess-sess-123", 63,
		},
		{
			"long org slug truncated",
			"engineering-division", "platform-team", "a1b2c3d4",
			"astn-sess-engineer-platform-a1b2c3d4", 63,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OrgSessionContainerName(tt.orgSlug, tt.teamSlug, tt.sessionID)
			if got != tt.wantPfx {
				t.Errorf("OrgSessionContainerName(%q, %q, %q) = %q, want %q",
					tt.orgSlug, tt.teamSlug, tt.sessionID, got, tt.wantPfx)
			}
			if len(got) > tt.maxLen {
				t.Errorf("OrgSessionContainerName result %q exceeds %d char limit (%d chars)",
					got, tt.maxLen, len(got))
			}
			// No trailing hyphen
			if len(got) > 0 && got[len(got)-1] == '-' {
				t.Errorf("OrgSessionContainerName result %q ends with hyphen", got)
			}
		})
	}
}

func TestOrgSessionContainerName_DifferentOrgsDistinct(t *testing.T) {
	t.Parallel()

	name1 := OrgSessionContainerName("acme", "eng", "session-1")
	name2 := OrgSessionContainerName("globex", "eng", "session-1")
	if name1 == name2 {
		t.Errorf("different orgs should produce different container names: %q == %q", name1, name2)
	}
}

func TestOrgSessionContainerName_DifferentTeamsDistinct(t *testing.T) {
	t.Parallel()

	name1 := OrgSessionContainerName("acme", "eng", "session-1")
	name2 := OrgSessionContainerName("acme", "sales", "session-1")
	if name1 == name2 {
		t.Errorf("different teams should produce different container names: %q == %q", name1, name2)
	}
}

// ---------------------------------------------------------------------------
// 6.10d: OrgFleetContainerName tests
// ---------------------------------------------------------------------------

func TestOrgFleetContainerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		orgSlug  string
		planKey  string
		agentKey string
		taskSlug string
		wantPfx  string
	}{
		{
			"basic org fleet",
			"acme", "deploy", "builder", "",
			"astn-fleet-acme-deploy-builder",
		},
		{
			"with task",
			"acme", "deploy", "builder", "compile",
			"astn-fleet-acme-deploy-builder-compile",
		},
		{
			"falls back when org empty",
			"", "deploy", "builder", "",
			"astn-fleet-deploy-builder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OrgFleetContainerName(tt.orgSlug, tt.planKey, tt.agentKey, tt.taskSlug)
			if got != tt.wantPfx {
				t.Errorf("OrgFleetContainerName(%q, %q, %q, %q) = %q, want %q",
					tt.orgSlug, tt.planKey, tt.agentKey, tt.taskSlug, got, tt.wantPfx)
			}
			if len(got) > 63 {
				t.Errorf("OrgFleetContainerName result %q exceeds 63 char limit (%d chars)", got, len(got))
			}
		})
	}
}

// Note: TestOrgSubnet_* were moved to pkg/sandbox/incus/org_network_test.go
// as part of the Phase B.2 reorganization — they exercise an unexported
// helper that lives in that package.

// ---------------------------------------------------------------------------
// 6.10f: NodeClientPool.SetOrgContext propagation
// ---------------------------------------------------------------------------

func TestNodeClientPool_OrgContext(t *testing.T) {
	t.Parallel()

	pool := &NodeClientPool{
		clients: make(map[string]*LazyNodeClient),
	}

	pool.SetOrgContext("acme", "eng")

	if got := pool.OrgSlug(); got != "acme" {
		t.Errorf("OrgSlug() = %q, want %q", got, "acme")
	}

	// GetOrCreate should propagate org context (but will return nil without incusClient)
	// Just verify the pool fields are set
	pool.mu.Lock()
	if pool.orgSlug != "acme" {
		t.Errorf("pool.orgSlug = %q, want %q", pool.orgSlug, "acme")
	}
	if pool.teamSlug != "eng" {
		t.Errorf("pool.teamSlug = %q, want %q", pool.teamSlug, "eng")
	}
	pool.mu.Unlock()
}
