package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
)

// TestResolveChannelUser_InjectsAllSkillStores verifies that the channel
// resolver injects Platform, Org, and Team skill stores into the context.
// Regression test for: channels previously only injected Team skills, causing
// Telegram users to not see Organization and Platform skills.
func TestResolveChannelUser_InjectsAllSkillStores(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()

	// Step 1: Create a user
	userID := uuid.New().String()
	user := &store.User{
		ID:          userID,
		Email:       "test@example.com",
		DisplayName: "Test User",
		Status:      "active",
		CreatedAt:   time.Now(),
	}
	if err := esStore.Users().Create(ctx, user); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	// Step 2: Create an organization and add user as member
	orgID := uuid.New().String()
	org := &store.Organization{
		ID:        orgID,
		Name:      "Test Org",
		Slug:      "testorg",
		Status:    "active",
		CreatedAt: time.Now(),
	}
	if err := esStore.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("Create org: %v", err)
	}
	if err := esStore.Organizations().AddMember(ctx, userID, orgID, "member"); err != nil {
		t.Fatalf("Add org member: %v", err)
	}

	// Step 3: Provision org database and create a team
	if err := esStore.ProvisionOrg(ctx, orgID, "testorg"); err != nil {
		t.Fatalf("Provision org: %v", err)
	}

	orgStore, err := esStore.ForOrg("testorg")
	if err != nil {
		t.Fatalf("ForOrg: %v", err)
	}

	teamID := uuid.New().String()
	team := &store.Team{
		ID:         teamID,
		Name:       "Test Team",
		Slug:       "testteam",
		SchemaName: "team_testteam",
		CreatedAt:  time.Now(),
	}
	if err := orgStore.Teams().CreateTeam(ctx, team); err != nil {
		t.Fatalf("Create team: %v", err)
	}
	if err := orgStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID:   userID,
		TeamID:   teamID,
		Role:     "member",
		JoinedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Add team member: %v", err)
	}

	// Step 4: Create a user-channel link (Telegram)
	now := time.Now()
	link := &store.UserChannel{
		ID:          uuid.New().String(),
		UserID:      userID,
		ChannelType: "telegram",
		ExternalID:  "tg12345",
		DisplayName: "@testuser",
		Enabled:     true,
		Verified:    true,
		VerifiedAt:  &now,
		CreatedAt:   time.Now(),
	}
	if err := esStore.UserChannels().Link(ctx, link); err != nil {
		t.Fatalf("Link user channel: %v", err)
	}

	// Step 5: Create the resolver and call ResolveChannelUser
	resolver := &channelPlatformResolver{backend: esStore}
	enrichedCtx, resolvedUserID, displayName, resolveErr := resolver.ResolveChannelUser(ctx, "telegram", "tg12345")
	if resolveErr != nil {
		t.Fatalf("ResolveChannelUser: %v", resolveErr)
	}

	if resolvedUserID != userID {
		t.Errorf("userID = %q, want %q", resolvedUserID, userID)
	}
	if displayName != "Test User" {
		t.Errorf("displayName = %q, want %q", displayName, "Test User")
	}

	// Step 6: Verify that SkillStores in context has all three tiers
	ss := store.SkillStoresFromContext(enrichedCtx)
	if ss == nil {
		t.Fatal("SkillStores not found in context")
	}
	if ss.Platform == nil {
		t.Error("SkillStores.Platform is nil — platform skills not injected")
	}
	if ss.Org == nil {
		t.Error("SkillStores.Org is nil — org skills not injected")
	}
	if ss.Team == nil {
		t.Error("SkillStores.Team is nil — team skills not injected")
	}

	// Also verify MCP server stores have all three tiers (existing behavior)
	mcpStores := store.MCPServerStoresFromContext(enrichedCtx)
	if mcpStores == nil {
		t.Fatal("MCPServerStores not found in context")
	}
	if mcpStores.Platform == nil {
		t.Error("MCPServerStores.Platform is nil")
	}
	if mcpStores.Org == nil {
		t.Error("MCPServerStores.Org is nil")
	}
	if mcpStores.Team == nil {
		t.Error("MCPServerStores.Team is nil")
	}
}

// TestResolveChannelUser_PreviousBug_OnlyTeamSkills is a negative test that
// documents the previous bug. It verifies the fix is in place by ensuring
// Platform and Org are NOT nil (they were nil before the fix).
func TestResolveChannelUser_PreviousBug_OnlyTeamSkills(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()

	// Minimal setup: user, org, team, channel link
	userID := uuid.New().String()
	if err := esStore.Users().Create(ctx, &store.User{
		ID: userID, Email: "u2@test.com", DisplayName: "User2", Status: "active", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	orgID := uuid.New().String()
	if err := esStore.Organizations().Create(ctx, &store.Organization{
		ID: orgID, Name: "Org2", Slug: "org2", Status: "active", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create org: %v", err)
	}
	if err := esStore.Organizations().AddMember(ctx, userID, orgID, "member"); err != nil {
		t.Fatalf("Add member: %v", err)
	}
	if err := esStore.ProvisionOrg(ctx, orgID, "org2"); err != nil {
		t.Fatalf("Provision org: %v", err)
	}

	orgStore, err := esStore.ForOrg("org2")
	if err != nil {
		t.Fatalf("ForOrg: %v", err)
	}

	teamID := uuid.New().String()
	if err := orgStore.Teams().CreateTeam(ctx, &store.Team{
		ID: teamID, Name: "Team2", Slug: "team2", SchemaName: "team_team2", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create team: %v", err)
	}
	if err := orgStore.Teams().AddMember(ctx, &store.TeamMembership{
		UserID: userID, TeamID: teamID, Role: "member", JoinedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Add team member: %v", err)
	}

	now := time.Now()
	if err := esStore.UserChannels().Link(ctx, &store.UserChannel{
		ID: uuid.New().String(), UserID: userID, ChannelType: "telegram", ExternalID: "tg99999",
		DisplayName: "@u2", Enabled: true, Verified: true, VerifiedAt: &now, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Link: %v", err)
	}

	// Resolve
	resolver := &channelPlatformResolver{backend: esStore}
	enrichedCtx, _, _, resolveErr := resolver.ResolveChannelUser(ctx, "telegram", "tg99999")
	if resolveErr != nil {
		t.Fatalf("ResolveChannelUser: %v", resolveErr)
	}

	// THE KEY ASSERTION: Before the fix, Platform and Org were nil.
	// After the fix, they must be non-nil.
	ss := store.SkillStoresFromContext(enrichedCtx)
	if ss == nil {
		t.Fatal("SkillStores not in context")
	}

	// This is the regression check — if this fails, the bug is back.
	if ss.Platform == nil {
		t.Fatal("REGRESSION: SkillStores.Platform is nil — only Team was injected (pre-fix behavior)")
	}
	if ss.Org == nil {
		t.Fatal("REGRESSION: SkillStores.Org is nil — only Team was injected (pre-fix behavior)")
	}
}
