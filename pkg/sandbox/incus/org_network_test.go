package incus

import "testing"

// These tests exercise the unexported orgSubnet helper which lives in this
// package. They were moved here from pkg/sandbox/org_network_test.go as
// part of the Phase B.2 reorganization so they can reference the helper
// directly rather than via a shim.

func TestOrgSubnet_Deterministic(t *testing.T) {
	t.Parallel()

	s1 := orgSubnet("acme")
	s2 := orgSubnet("acme")
	if s1 != s2 {
		t.Errorf("orgSubnet should be deterministic: %q != %q", s1, s2)
	}
}

func TestOrgSubnet_DifferentOrgs(t *testing.T) {
	t.Parallel()

	s1 := orgSubnet("acme")
	s2 := orgSubnet("globex")
	// Different orgs should almost always get different subnets
	// (collision is possible with the simple hash but unlikely for short names)
	if s1 == s2 {
		t.Logf("warning: org subnet collision between 'acme' and 'globex': %s", s1)
	}
}

func TestOrgSubnet_ValidRange(t *testing.T) {
	t.Parallel()

	slugs := []string{"acme", "globex", "initech", "hooli", "piedpiper", "a", "zzzz", "test-org-123"}
	for _, slug := range slugs {
		subnet := orgSubnet(slug)
		// Should match pattern 10.{100-199}.{0-255}.1/24
		if len(subnet) == 0 {
			t.Errorf("orgSubnet(%q) returned empty string", slug)
			continue
		}
		// Quick validation: starts with "10." and ends with ".1/24"
		if subnet[:3] != "10." {
			t.Errorf("orgSubnet(%q) = %q, expected to start with '10.'", slug, subnet)
		}
		if subnet[len(subnet)-5:] != ".1/24" {
			t.Errorf("orgSubnet(%q) = %q, expected to end with '.1/24'", slug, subnet)
		}
	}
}
