//go:build e2e

package e2eboot

// Multi-tenant layout for E2E tests.
//
// This file defines a deterministic "world" of orgs, teams, users, memories,
// skills, and MCP servers. Any test that needs multi-tenant isolation can call
// Seed(t, h) which materializes this entire layout on top of a bootstrapped DB.
//
// Layout shape:
//
//	Org "acme" (multi-team)
//	├── Team "red"
//	│   ├── alice (admin)
//	│   └── bob (member)
//	└── Team "blue"
//	    └── carol (member)
//
//	Org "globex" (single-team)
//	└── Team "engineering"
//	    ├── dave (admin)
//	    └── eve (member) — adversarial user for cross-org boundary tests
//
//	Org "default" (auto-created by Bootstrap)
//	└── Team "general"
//	    └── e2e@test.local (admin, superadmin)

// --- Org slugs ---

const (
	OrgAcmeSlug   = "acme"
	OrgGlobexSlug = "globex"
	OrgDefaultSlug = "default" // auto-created by Bootstrap
)

// --- Team slugs ---

const (
	TeamAcmeRed    = "red"
	TeamAcmeBlue   = "blue"
	TeamGlobexEng  = "engineering"
	TeamDefaultGen = "general" // auto-created by Bootstrap
)

// --- User emails (deterministic) ---

const (
	UserAliceEmail = "alice@acme.test"
	UserBobEmail   = "bob@acme.test"
	UserCarolEmail = "carol@acme.test"
	UserDaveEmail  = "dave@globex.test"
	UserEveEmail   = "eve@globex.test"
)

// SeededUserPassword is defined in inspector_state.go (no build tag) so
// the standalone tools/e2e-inspector binary can also reference it.

// --- Seed data labels ---
// Each label uniquely identifies a piece of seeded data.
// Tests assert on these labels to verify isolation.

// Memory labels
const (
	MemAlicePersonal = "alice-personal-memory"
	MemBobPersonal   = "bob-personal-memory"
	MemCarolPersonal = "carol-personal-memory"
	MemDavePersonal  = "dave-personal-memory"
	MemEvePersonal   = "eve-personal-memory"

	MemAcmeRedTeam  = "acme-red-team-memory"
	MemAcmeBlueTeam = "acme-blue-team-memory"
	MemGlobexTeam   = "globex-eng-team-memory"

	MemAcmeOrg   = "acme-org-memory"
	MemGlobexOrg = "globex-org-memory"
)

// Skill labels
const (
	SkillAcmeRedTeam  = "acme-red-team-skill"
	SkillAcmeBlueTeam = "acme-blue-team-skill"
	SkillGlobexTeam   = "globex-eng-team-skill"

	SkillAcmeOrg   = "acme-org-skill"
	SkillGlobexOrg = "globex-org-skill"
)

// MCP Server labels
const (
	MCPAcmeRedTeam  = "acme-red-mcp-server"
	MCPAcmeBlueTeam = "acme-blue-mcp-server"
	MCPGlobexTeam   = "globex-eng-mcp-server"

	MCPAcmeOrg   = "acme-org-mcp-server"
	MCPGlobexOrg = "globex-org-mcp-server"
)

// Credential labels
const (
	CredAlicePersonal = "alice-personal-cred"
	CredBobPersonal   = "bob-personal-cred"
	CredDavePersonal  = "dave-personal-cred"
	CredEvePersonal   = "eve-personal-cred"

	CredAcmeRedTeam = "acme-red-team-cred"
	CredGlobexTeam  = "globex-eng-team-cred"
)

// SeededUser holds a provisioned user's identity and JWT.
type SeededUser struct {
	ID       string
	Email    string
	Name     string
	OrgSlug  string
	TeamSlug string
	OrgRole  string // "owner", "admin", "member"
	TeamRole string // "admin", "member", "viewer"
	Token    string // JWT access token
	baseURL  string // server base URL (internal)
}

// SeededMemory holds a memory record with its label for test assertions.
type SeededMemory struct {
	Label   string
	Scope   string // "personal", "team", "org"
	OrgSlug string
	Team    string // team slug (if team scope)
	UserID  string // owner user ID (if personal scope)
	Content string
}

// SeedResult holds the result of calling Seed(). Tests use this to
// get per-user HTTP clients and look up seeded record identifiers.
type SeedResult struct {
	Users    map[string]*SeededUser  // email → user
	Memories []SeededMemory
}

// Client returns an authenticated HTTP client for the user with the given email.
func (s *SeedResult) Client(email string) *Client {
	u, ok := s.Users[email]
	if !ok {
		panic("e2eboot: unknown user " + email)
	}
	return &Client{
		baseURL:  u.baseURL,
		token:    u.Token,
		teamSlug: u.TeamSlug,
	}
}

// ClientInTeam returns an authenticated HTTP client for the user but
// with a different X-Astonish-Team header (for users in multiple teams).
func (s *SeedResult) ClientInTeam(email, teamSlug string) *Client {
	u, ok := s.Users[email]
	if !ok {
		panic("e2eboot: unknown user " + email)
	}
	return &Client{
		baseURL:  u.baseURL,
		token:    u.Token,
		teamSlug: teamSlug,
	}
}
