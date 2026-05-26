//go:build e2e

package e2eboot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
)

// Seed materializes the standard multi-tenant test layout on top of an
// already-bootstrapped harness. It creates additional orgs, teams, users,
// memories, skills, MCP servers, and credentials — then returns a SeedResult
// with per-user authenticated clients.
//
// Call this once per test (after Bootstrap). All provisioned databases are
// cleaned up automatically via t.Cleanup.
//
// In shared-instance mode (h.SharedMode=true), every slug and email is
// suffixed with h.PerTestSuffix (the package name, e.g. "chatauth"), and
// Seed becomes IDEMPOTENT: the first test in the package provisions the
// world, and subsequent tests in the same package detect existing
// orgs/users and reuse them. The SeedResult.Users map remains keyed by
// the original email constants so test code stays unchanged.
func Seed(t *testing.T, h *Harness) *SeedResult {
	t.Helper()
	ctx := context.Background()

	result := &SeedResult{
		Users: make(map[string]*SeededUser),
	}

	// Compute actual slugs/emails for this test.
	acmeSlug := h.actualOrgSlug(OrgAcmeSlug)
	globexSlug := h.actualOrgSlug(OrgGlobexSlug)
	redSlug := h.actualTeamSlug(TeamAcmeRed)
	blueSlug := h.actualTeamSlug(TeamAcmeBlue)
	engSlug := h.actualTeamSlug(TeamGlobexEng)
	aliceEmail := h.actualEmail(UserAliceEmail)
	bobEmail := h.actualEmail(UserBobEmail)
	carolEmail := h.actualEmail(UserCarolEmail)
	daveEmail := h.actualEmail(UserDaveEmail)
	eveEmail := h.actualEmail(UserEveEmail)

	// In shared mode, if the orgs already exist (a previous test in this
	// same Go test package provisioned them), the harness skips re-seeding
	// and just rebuilds the SeededUser map from the existing DB rows.
	if h.SharedMode {
		if existing := tryAttachToExistingSeed(t, ctx, h, acmeSlug, globexSlug, redSlug, blueSlug, engSlug, aliceEmail, bobEmail, carolEmail, daveEmail, eveEmail); existing != nil {
			t.Logf("[e2eboot] Seed reused (package=%s): %d users", h.PerTestSuffix, len(existing.Users))
			return existing
		}
	}

	// --- Provision orgs ---
	acmeOrgID := provisionOrg(t, ctx, h, acmeSlug, "Acme Corporation")
	globexOrgID := provisionOrg(t, ctx, h, globexSlug, "Globex Industries")

	// --- Provision users in platform DB ---
	aliceID := createUser(t, ctx, h, aliceEmail, "Alice Smith")
	bobID := createUser(t, ctx, h, bobEmail, "Bob Jones")
	carolID := createUser(t, ctx, h, carolEmail, "Carol White")
	daveID := createUser(t, ctx, h, daveEmail, "Dave Black")
	eveID := createUser(t, ctx, h, eveEmail, "Eve Mallory")

	// --- Org memberships ---
	addOrgMember(t, ctx, h, aliceID, acmeOrgID, "owner")
	addOrgMember(t, ctx, h, bobID, acmeOrgID, "member")
	addOrgMember(t, ctx, h, carolID, acmeOrgID, "member")
	addOrgMember(t, ctx, h, daveID, globexOrgID, "owner")
	addOrgMember(t, ctx, h, eveID, globexOrgID, "member")

	// --- Provision teams and schemas ---
	acmeOrg := getOrgDataStore(t, h, acmeSlug)
	globexOrg := getOrgDataStore(t, h, globexSlug)

	redTeamID := provisionTeam(t, ctx, acmeOrg, redSlug, "Team Red")
	blueTeamID := provisionTeam(t, ctx, acmeOrg, blueSlug, "Team Blue")
	engTeamID := provisionTeam(t, ctx, globexOrg, engSlug, "Engineering")

	// --- Team memberships ---
	addTeamMember(t, ctx, acmeOrg, aliceID, redTeamID, "admin")
	addTeamMember(t, ctx, acmeOrg, bobID, redTeamID, "member")
	addTeamMember(t, ctx, acmeOrg, carolID, blueTeamID, "member")
	addTeamMember(t, ctx, globexOrg, daveID, engTeamID, "admin")
	addTeamMember(t, ctx, globexOrg, eveID, engTeamID, "member")

	// --- Personal schemas ---
	provisionPersonalSchema(t, ctx, acmeOrg, aliceID)
	provisionPersonalSchema(t, ctx, acmeOrg, bobID)
	provisionPersonalSchema(t, ctx, acmeOrg, carolID)
	provisionPersonalSchema(t, ctx, globexOrg, daveID)
	provisionPersonalSchema(t, ctx, globexOrg, eveID)

	// --- Issue JWTs ---
	jwtIssuer := api.NewJWTIssuer(defaultJWTSecret, 15*time.Minute, 90*24*time.Hour)

	// SeedResult.Users is keyed by the ORIGINAL email constants (UserAliceEmail
	// etc.) so test code stays unchanged. The SeededUser stores the actual
	// org/team slug used in the DB.
	registerSeededUser(result, h.BaseURL, jwtIssuer, aliceID, UserAliceEmail, aliceEmail, "Alice Smith", acmeSlug, redSlug, "owner", "admin")
	registerSeededUser(result, h.BaseURL, jwtIssuer, bobID, UserBobEmail, bobEmail, "Bob Jones", acmeSlug, redSlug, "member", "member")
	registerSeededUser(result, h.BaseURL, jwtIssuer, carolID, UserCarolEmail, carolEmail, "Carol White", acmeSlug, blueSlug, "member", "member")
	registerSeededUser(result, h.BaseURL, jwtIssuer, daveID, UserDaveEmail, daveEmail, "Dave Black", globexSlug, engSlug, "owner", "admin")
	registerSeededUser(result, h.BaseURL, jwtIssuer, eveID, UserEveEmail, eveEmail, "Eve Mallory", globexSlug, engSlug, "member", "member")

	// --- Seed memories ---
	seedMemories(t, ctx, acmeOrg, globexOrg, redSlug, blueSlug, engSlug, result)

	// --- Seed skills ---
	seedSkills(t, ctx, acmeOrg, globexOrg, redSlug, blueSlug, engSlug, result)

	// --- Seed MCP servers ---
	seedMCPServers(t, ctx, acmeOrg, globexOrg, redSlug, blueSlug, engSlug, result)

	// --- Seed credentials ---
	seedCredentials(t, ctx, acmeOrg, globexOrg, redSlug, engSlug, result)

	t.Logf("[e2eboot] Seed complete: %d users, %d memories seeded", len(result.Users), len(result.Memories))
	return result
}

// --- Internal helpers ---

// tryAttachToExistingSeed checks whether a previous test in the same Go
// test package has already provisioned the world (orgs, users, teams).
// If everything is found, it rebuilds a fully-populated SeedResult and
// returns it. Otherwise it returns nil and the caller proceeds with full
// provisioning.
//
// The decision is all-or-nothing: if any of the 2 orgs, 5 users, or 3
// teams is missing, attach fails and the caller falls through to provision.
// In practice this means re-runs after a manual partial drop will simply
// re-seed everything cleanly.
func tryAttachToExistingSeed(t *testing.T, ctx context.Context, h *Harness,
	acmeSlug, globexSlug, redSlug, blueSlug, engSlug,
	aliceEmail, bobEmail, carolEmail, daveEmail, eveEmail string) *SeedResult {
	t.Helper()

	acmeOrg, err := h.PlatformBackend().Organizations().GetBySlug(ctx, acmeSlug)
	if err != nil || acmeOrg == nil {
		return nil
	}
	globexOrg, err := h.PlatformBackend().Organizations().GetBySlug(ctx, globexSlug)
	if err != nil || globexOrg == nil {
		return nil
	}

	aliceUser, err := h.PlatformBackend().Users().GetByEmail(ctx, aliceEmail)
	if err != nil || aliceUser == nil {
		return nil
	}
	bobUser, err := h.PlatformBackend().Users().GetByEmail(ctx, bobEmail)
	if err != nil || bobUser == nil {
		return nil
	}
	carolUser, err := h.PlatformBackend().Users().GetByEmail(ctx, carolEmail)
	if err != nil || carolUser == nil {
		return nil
	}
	daveUser, err := h.PlatformBackend().Users().GetByEmail(ctx, daveEmail)
	if err != nil || daveUser == nil {
		return nil
	}
	eveUser, err := h.PlatformBackend().Users().GetByEmail(ctx, eveEmail)
	if err != nil || eveUser == nil {
		return nil
	}

	// Verify teams exist in their respective org schemas.
	acmeOds, err := h.PlatformBackend().ForOrg(acmeSlug)
	if err != nil {
		return nil
	}
	globexOds, err := h.PlatformBackend().ForOrg(globexSlug)
	if err != nil {
		return nil
	}
	if team, _ := acmeOds.Teams().GetTeamBySlug(ctx, redSlug); team == nil {
		return nil
	}
	if team, _ := acmeOds.Teams().GetTeamBySlug(ctx, blueSlug); team == nil {
		return nil
	}
	if team, _ := globexOds.Teams().GetTeamBySlug(ctx, engSlug); team == nil {
		return nil
	}

	// All entities exist — rebuild the SeedResult with fresh JWTs.
	jwtIssuer := api.NewJWTIssuer(defaultJWTSecret, 15*time.Minute, 90*24*time.Hour)
	result := &SeedResult{Users: make(map[string]*SeededUser)}

	registerSeededUser(result, h.BaseURL, jwtIssuer, aliceUser.ID, UserAliceEmail, aliceEmail, "Alice Smith", acmeSlug, redSlug, "owner", "admin")
	registerSeededUser(result, h.BaseURL, jwtIssuer, bobUser.ID, UserBobEmail, bobEmail, "Bob Jones", acmeSlug, redSlug, "member", "member")
	registerSeededUser(result, h.BaseURL, jwtIssuer, carolUser.ID, UserCarolEmail, carolEmail, "Carol White", acmeSlug, blueSlug, "member", "member")
	registerSeededUser(result, h.BaseURL, jwtIssuer, daveUser.ID, UserDaveEmail, daveEmail, "Dave Black", globexSlug, engSlug, "owner", "admin")
	registerSeededUser(result, h.BaseURL, jwtIssuer, eveUser.ID, UserEveEmail, eveEmail, "Eve Mallory", globexSlug, engSlug, "member", "member")

	return result
}

func provisionOrg(t *testing.T, ctx context.Context, h *Harness, slug, name string) string {
	t.Helper()
	orgID := uuid.New().String()

	dbName := config.OrgDBName(h.Suffix, slug)
	org := &store.Organization{
		ID:        orgID,
		Name:      name,
		Slug:      slug,
		DBName:    dbName,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	if err := h.PlatformBackend().Organizations().Create(ctx, org); err != nil {
		t.Fatalf("[seed] create org %s: %v", slug, err)
	}
	if err := h.PlatformBackend().ProvisionOrg(ctx, orgID, slug); err != nil {
		t.Fatalf("[seed] provision org %s: %v", slug, err)
	}

	// Register cleanup to drop the org DB.
	// In shared/inspector mode we deliberately keep all data around so the
	// developer can browse it in the UI after the suite completes.
	if !h.SharedMode {
		t.Cleanup(func() {
			if err := h.PlatformBackend().DecommissionOrg(context.Background(), slug); err != nil {
				t.Logf("[seed] WARN: decommission org %s: %v", slug, err)
			}
		})
	}

	t.Logf("[seed] Provisioned org %s (id=%s)", slug, orgID)
	return orgID
}

func createUser(t *testing.T, ctx context.Context, h *Harness, email, displayName string) string {
	t.Helper()
	userID := uuid.New().String()
	// Hash a real password so the developer can log into the kept-alive UI
	// as Alice/Bob/etc. Tests authenticate via minted JWTs and never use
	// the password directly. Hashing is done with the same library/cost
	// the /api/auth/register handler uses.
	hash, err := bcrypt.GenerateFromPassword([]byte(SeededUserPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("[seed] hash password for %s: %v", email, err)
	}
	user := &store.User{
		ID:           userID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: string(hash),
		Status:       "active",
		CreatedAt:    time.Now(),
	}
	if err := h.PlatformBackend().Users().Create(ctx, user); err != nil {
		t.Fatalf("[seed] create user %s: %v", email, err)
	}
	return userID
}

func addOrgMember(t *testing.T, ctx context.Context, h *Harness, userID, orgID, role string) {
	t.Helper()
	if err := h.PlatformBackend().Organizations().AddMember(ctx, userID, orgID, role); err != nil {
		t.Fatalf("[seed] add org member %s: %v", userID, err)
	}
}

func getOrgDataStore(t *testing.T, h *Harness, orgSlug string) store.OrgDataStore {
	t.Helper()
	ods, err := h.PlatformBackend().ForOrg(orgSlug)
	if err != nil {
		t.Fatalf("[seed] ForOrg(%s): %v", orgSlug, err)
	}
	return ods
}

func provisionTeam(t *testing.T, ctx context.Context, ods store.OrgDataStore, slug, name string) string {
	t.Helper()
	teamID := uuid.New().String()
	team := &store.Team{
		ID:         teamID,
		Name:       name,
		Slug:       slug,
		SchemaName: "team_" + slug, // PG uses this for SET search_path; SQLite ignores it
		CreatedAt:  time.Now(),
	}
	if err := ods.Teams().CreateTeam(ctx, team); err != nil {
		t.Fatalf("[seed] create team %s: %v", slug, err)
	}
	if err := ods.ProvisionTeam(ctx, slug); err != nil {
		t.Fatalf("[seed] provision team schema %s: %v", slug, err)
	}
	return teamID
}

func addTeamMember(t *testing.T, ctx context.Context, ods store.OrgDataStore, userID, teamID, role string) {
	t.Helper()
	m := &store.TeamMembership{
		UserID:   userID,
		TeamID:   teamID,
		Role:     role,
		JoinedAt: time.Now(),
	}
	if err := ods.Teams().AddMember(ctx, m); err != nil {
		t.Fatalf("[seed] add team member %s to %s: %v", userID, teamID, err)
	}
}

func provisionPersonalSchema(t *testing.T, ctx context.Context, ods store.OrgDataStore, userID string) {
	t.Helper()
	if err := ods.ProvisionPersonalSchema(ctx, userID); err != nil {
		t.Fatalf("[seed] provision personal schema for %s: %v", userID, err)
	}
}

// registerSeededUser stores a SeededUser in result.Users keyed by mapKey
// (the original email constant) and records the actual storedEmail used in
// the DB. In isolated mode mapKey == storedEmail; in shared mode they differ
// (storedEmail has a per-test plus-tag).
func registerSeededUser(result *SeedResult, baseURL string, issuer *api.JWTIssuer, userID, mapKey, storedEmail, name, orgSlug, teamSlug, orgRole, teamRole string) {
	token, err := issuer.IssueAccessToken(userID, storedEmail, name, orgSlug, teamSlug, orgRole, "")
	if err != nil {
		panic(fmt.Sprintf("seed: issue token for %s: %v", storedEmail, err))
	}
	result.Users[mapKey] = &SeededUser{
		ID:       userID,
		Email:    storedEmail,
		Name:     name,
		OrgSlug:  orgSlug,
		TeamSlug: teamSlug,
		OrgRole:  orgRole,
		TeamRole: teamRole,
		Token:    token,
		baseURL:  baseURL,
	}
}

// --- Data seeding ---

func seedMemories(t *testing.T, ctx context.Context, acmeOrg, globexOrg store.OrgDataStore, redSlug, blueSlug, engSlug string, result *SeedResult) {
	t.Helper()

	alice := result.Users[UserAliceEmail]
	bob := result.Users[UserBobEmail]
	carol := result.Users[UserCarolEmail]
	dave := result.Users[UserDaveEmail]
	eve := result.Users[UserEveEmail]

	// Personal memories
	addMemory(t, ctx, acmeOrg.ForUser(alice.ID).Memories(), MemAlicePersonal, alice.ID, result)
	addMemory(t, ctx, acmeOrg.ForUser(bob.ID).Memories(), MemBobPersonal, bob.ID, result)
	addMemory(t, ctx, acmeOrg.ForUser(carol.ID).Memories(), MemCarolPersonal, carol.ID, result)
	addMemory(t, ctx, globexOrg.ForUser(dave.ID).Memories(), MemDavePersonal, dave.ID, result)
	addMemory(t, ctx, globexOrg.ForUser(eve.ID).Memories(), MemEvePersonal, eve.ID, result)

	// Team memories
	addMemory(t, ctx, acmeOrg.ForTeam(redSlug).Memories(), MemAcmeRedTeam, alice.ID, result)
	addMemory(t, ctx, acmeOrg.ForTeam(blueSlug).Memories(), MemAcmeBlueTeam, carol.ID, result)
	addMemory(t, ctx, globexOrg.ForTeam(engSlug).Memories(), MemGlobexTeam, dave.ID, result)

	// Org memories
	addMemory(t, ctx, acmeOrg.OrgMemories(), MemAcmeOrg, alice.ID, result)
	addMemory(t, ctx, globexOrg.OrgMemories(), MemGlobexOrg, dave.ID, result)
}

func addMemory(t *testing.T, ctx context.Context, ms store.MemoryStore, label, createdBy string, result *SeedResult) {
	t.Helper()
	entry := store.MemoryEntry{
		Content:   fmt.Sprintf("Seeded memory: %s. This content is unique for test assertions.", label),
		Category:  "e2e-test",
		CreatedBy: createdBy,
	}
	if err := ms.Add(ctx, entry); err != nil {
		t.Fatalf("[seed] add memory %s: %v", label, err)
	}
	result.Memories = append(result.Memories, SeededMemory{
		Label:   label,
		Content: entry.Content,
	})
}

func seedSkills(t *testing.T, ctx context.Context, acmeOrg, globexOrg store.OrgDataStore, redSlug, blueSlug, engSlug string, result *SeedResult) {
	t.Helper()

	alice := result.Users[UserAliceEmail]
	carol := result.Users[UserCarolEmail]
	dave := result.Users[UserDaveEmail]

	// Team skills
	saveSkill(t, ctx, acmeOrg.ForTeam(redSlug).Skills(), SkillAcmeRedTeam, alice.ID)
	saveSkill(t, ctx, acmeOrg.ForTeam(blueSlug).Skills(), SkillAcmeBlueTeam, carol.ID)
	saveSkill(t, ctx, globexOrg.ForTeam(engSlug).Skills(), SkillGlobexTeam, dave.ID)

	// Org skills
	saveSkill(t, ctx, acmeOrg.OrgSkills(), SkillAcmeOrg, alice.ID)
	saveSkill(t, ctx, globexOrg.OrgSkills(), SkillGlobexOrg, dave.ID)
}

func saveSkill(t *testing.T, ctx context.Context, ss store.SkillStore, label, createdBy string) {
	t.Helper()
	// Skill content follows the frontmatter format expected by the parser
	content := fmt.Sprintf(`---
name: %s
description: E2E test skill (%s)
---

# %s

This is a seeded skill for E2E testing. Label: %s
`, label, label, label, label)

	skill := &store.Skill{
		Name:        label,
		Description: fmt.Sprintf("E2E test skill (%s)", label),
		Content:     content,
		CreatedBy:   createdBy,
	}
	if err := ss.Save(ctx, skill); err != nil {
		t.Fatalf("[seed] save skill %s: %v", label, err)
	}
}

func seedMCPServers(t *testing.T, ctx context.Context, acmeOrg, globexOrg store.OrgDataStore, redSlug, blueSlug, engSlug string, result *SeedResult) {
	t.Helper()

	alice := result.Users[UserAliceEmail]
	carol := result.Users[UserCarolEmail]
	dave := result.Users[UserDaveEmail]

	// Team MCP servers (stub registrations — no actual MCP process running)
	saveMCPServer(t, ctx, acmeOrg.ForTeam(redSlug).MCPServers(), MCPAcmeRedTeam, alice.ID)
	saveMCPServer(t, ctx, acmeOrg.ForTeam(blueSlug).MCPServers(), MCPAcmeBlueTeam, carol.ID)
	saveMCPServer(t, ctx, globexOrg.ForTeam(engSlug).MCPServers(), MCPGlobexTeam, dave.ID)

	// Org MCP servers
	saveMCPServer(t, ctx, acmeOrg.OrgMCPServers(), MCPAcmeOrg, alice.ID)
	saveMCPServer(t, ctx, globexOrg.OrgMCPServers(), MCPGlobexOrg, dave.ID)
}

func saveMCPServer(t *testing.T, ctx context.Context, ms store.MCPServerStore, label, createdBy string) {
	t.Helper()
	enabled := true
	server := &store.MCPServer{
		ID:        uuid.New().String(),
		Name:      label,
		Command:   "echo", // stub — won't actually be invoked
		Args:      []string{"hello"},
		Transport: "stdio",
		Enabled:   &enabled,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := ms.Save(ctx, server); err != nil {
		t.Fatalf("[seed] save MCP server %s: %v", label, err)
	}
}

func seedCredentials(t *testing.T, ctx context.Context, acmeOrg, globexOrg store.OrgDataStore, redSlug, engSlug string, result *SeedResult) {
	t.Helper()

	alice := result.Users[UserAliceEmail]
	bob := result.Users[UserBobEmail]
	dave := result.Users[UserDaveEmail]
	eve := result.Users[UserEveEmail]

	// Personal credentials
	setCred(t, ctx, acmeOrg.ForUser(alice.ID).Credentials(), CredAlicePersonal)
	setCred(t, ctx, acmeOrg.ForUser(bob.ID).Credentials(), CredBobPersonal)
	setCred(t, ctx, globexOrg.ForUser(dave.ID).Credentials(), CredDavePersonal)
	setCred(t, ctx, globexOrg.ForUser(eve.ID).Credentials(), CredEvePersonal)

	// Team credentials
	setCred(t, ctx, acmeOrg.ForTeam(redSlug).Credentials(), CredAcmeRedTeam)
	setCred(t, ctx, globexOrg.ForTeam(engSlug).Credentials(), CredGlobexTeam)
}

func setCred(t *testing.T, ctx context.Context, cs store.CredentialStore, label string) {
	t.Helper()
	cred := &store.Credential{
		Type:  store.CredBearer,
		Token: fmt.Sprintf("fake-token-for-%s", label),
	}
	if err := cs.Set(ctx, label, cred); err != nil {
		t.Fatalf("[seed] set credential %s: %v", label, err)
	}
}
