//go:build e2e

package e2eboot

import "strings"

// actualOrgSlug returns the actual org slug to use in the DB for a logical
// slug. In isolated mode (no PerTestSuffix) this is the input unchanged; in
// shared mode it appends "-<perTestSuffix>" so 26 tests can coexist.
func (h *Harness) actualOrgSlug(logical string) string {
	if h.PerTestSuffix == "" {
		return logical
	}
	return logical + "-" + h.PerTestSuffix
}

// actualTeamSlug returns the actual team slug to use in the DB for a logical
// slug. Same logic as actualOrgSlug.
func (h *Harness) actualTeamSlug(logical string) string {
	if h.PerTestSuffix == "" {
		return logical
	}
	return logical + "-" + h.PerTestSuffix
}

// actualEmail returns the actual email to use in the DB for a logical email.
// In shared mode it adds a "+<perTestSuffix>" plus-tag (RFC 5233) before the
// "@" so the address is unique without changing the routable mailbox.
//
//	alice@acme.test  →  alice+e2eDEADBEEF@acme.test
func (h *Harness) actualEmail(logical string) string {
	if h.PerTestSuffix == "" {
		return logical
	}
	at := strings.LastIndex(logical, "@")
	if at < 0 {
		// No "@" — fall back to suffix-prefix.
		return logical + "-" + h.PerTestSuffix
	}
	return logical[:at] + "+" + h.PerTestSuffix + logical[at:]
}
