//go:build integration

package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/sandbox"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// ---------------------------------------------------------------------------
// Test infrastructure: real Postgres, isolated schema per test.
//
// Env: ASTONISH_TEST_DSN must point to a Postgres instance (any DB name —
// the test creates & drops its own schema). Example:
//   export ASTONISH_TEST_DSN="postgres://postgres:554252@192.168.1.196:5432/astonish_dxmtlc_platform"
// ---------------------------------------------------------------------------

func mustTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		t.Skip("ASTONISH_TEST_DSN not set; skipping integration test")
	}
	return dsn
}

// setupChainTestSchema creates a fresh schema, applies platform migrations,
// and returns a pooled connection pinned to that schema plus the schema name.
// The schema is dropped on test cleanup.
func setupChainTestSchema(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := mustTestDSN(t)
	ctx := context.Background()

	// Bootstrap: create schema via a temporary connection.
	bootConn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	schema := fmt.Sprintf("chain_test_%s", t.Name())
	// Sanitize: replace non-alphanum with underscore.
	sanitized := ""
	for _, c := range schema {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			sanitized += string(c)
		} else {
			sanitized += "_"
		}
	}
	schema = sanitized
	if _, err := bootConn.Exec(ctx,
		fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schema}.Sanitize()),
	); err != nil {
		bootConn.Close(ctx)
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		conn2, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			_, _ = conn2.Exec(context.Background(),
				fmt.Sprintf("DROP SCHEMA %s CASCADE", pgx.Identifier{schema}.Sanitize()))
			conn2.Close(context.Background())
		}
	})

	// Apply platform migrations pinned to the schema.
	if _, err := bootConn.Exec(ctx,
		fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
	); err != nil {
		bootConn.Close(ctx)
		t.Fatalf("pin migration search_path: %v", err)
	}
	if err := pgstore.Migrate(ctx, bootConn, pgstore.MigrationPlatform, schema); err != nil {
		bootConn.Close(ctx)
		t.Fatalf("migrate: %v", err)
	}
	bootConn.Close(ctx)

	// Build a dedicated pool pinned to the test schema.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.MaxConns = 4
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx,
			fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
		)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// helpers to construct store pointers
func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Integration test: 4 scenarios
//
// These validate the full layer-chain resolution pipeline against a real
// Postgres instance with the actual schema and seed data. Each test modifies
// only its own isolated schema.
//
// The scenarios mirror the user acceptance matrix:
//   1. Fresh install, no team template      → backend default ["@base"]
//   2. Configured base, no team template    → ["@base", configuredTop]
//   3. Configured base + team template      → ["@base", configuredTop, teamTop]
//   4. Fresh install + team template        → ["@base", teamTop]
// ---------------------------------------------------------------------------

func TestChainScenario1_FreshInstall_NoTeam(t *testing.T) {
	pool := setupChainTestSchema(t)
	ctx := context.Background()

	tplStore := pgstore.NewPGSandboxTemplateStoreDirect(pool)
	templates := pgstore.NewPGSandboxTemplateStore(pool)

	// After migration 005, @base.top_layer_id = '@base' (sentinel).
	// resolveBaseLayerChain must return nil (sentinel filtered).
	baseChain := resolveBaseLayerChainWith(ctx, tplStore)
	if baseChain != nil {
		t.Errorf("Scenario 1: resolveBaseLayerChain = %v, want nil", baseChain)
	}

	// No team template exists → resolveTemplateLayerChain must return nil.
	teamChain := resolveTemplateLayerChainWith(ctx, templates, "team-general")
	if teamChain != nil {
		t.Errorf("Scenario 1: resolveTemplateLayerChain = %v, want nil", teamChain)
	}

	// Effective chat chain: backend default ["@base"] (SessionSpec.LayerChain
	// falls through to spec.TemplateID when empty — tested in session.go:180).
}

func TestChainScenario2_ConfiguredBase_NoTeam(t *testing.T) {
	pool := setupChainTestSchema(t)
	ctx := context.Background()

	tplStore := pgstore.NewPGSandboxTemplateStoreDirect(pool)
	templates := pgstore.NewPGSandboxTemplateStore(pool)
	layers := pgstore.NewPGLayerStore(pool)

	// Simulate Configure Base: insert a new layer, update @base's top_layer_id.
	configuredTop := "aabbccdd1234567890abcdef1234567890abcdef1234567890abcdef12345678"
	if err := layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:    configuredTop,
		CephFSPath: "/mnt/astonish-layers/" + configuredTop,
		SizeBytes:  1000,
	}); err != nil {
		t.Fatalf("PutLayer: %v", err)
	}
	if err := tplStore.SetBaseConfig(ctx, configuredTop, []byte(`{}`), ""); err != nil {
		t.Fatalf("SetBaseConfig: %v", err)
	}

	// resolveBaseLayerChain must return ["@base", configuredTop].
	baseChain := resolveBaseLayerChainWith(ctx, tplStore)
	wantBase := []string{sandbox.BaseTemplateID, configuredTop}
	if !slicesEqual(baseChain, wantBase) {
		t.Errorf("Scenario 2: resolveBaseLayerChain = %v, want %v", baseChain, wantBase)
	}

	// No team template → nil.
	teamChain := resolveTemplateLayerChainWith(ctx, templates, "team-general")
	if teamChain != nil {
		t.Errorf("Scenario 2: resolveTemplateLayerChain = %v, want nil", teamChain)
	}

	// Effective chat chain: ["@base", configuredTop] via InjectSandboxLayerChainIfEmpty.
}

func TestChainScenario3_ConfiguredBase_TeamTemplate(t *testing.T) {
	pool := setupChainTestSchema(t)
	ctx := context.Background()

	tplStore := pgstore.NewPGSandboxTemplateStoreDirect(pool)
	templates := pgstore.NewPGSandboxTemplateStore(pool)
	layers := pgstore.NewPGLayerStore(pool)

	// 1. Configure base.
	configuredTop := "1111111111111111111111111111111111111111111111111111111111111111"
	if err := layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:    configuredTop,
		CephFSPath: "/mnt/astonish-layers/" + configuredTop,
		SizeBytes:  2000,
	}); err != nil {
		t.Fatalf("PutLayer configured: %v", err)
	}
	if err := tplStore.SetBaseConfig(ctx, configuredTop, []byte(`{}`), ""); err != nil {
		t.Fatalf("SetBaseConfig: %v", err)
	}

	// 2. Create team layer.
	teamTop := "2222222222222222222222222222222222222222222222222222222222222222"
	if err := layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:     teamTop,
		ParentLayer: strPtr(sandbox.BaseTemplateID),
		CephFSPath:  "/mnt/astonish-layers/" + teamTop,
		SizeBytes:   500,
	}); err != nil {
		t.Fatalf("PutLayer team: %v", err)
	}

	// 3. Create team template with parent=@base (well-known UUID).
	baseID := getBaseTemplateID(t, ctx, templates)
	teamTpl := &store.SandboxTemplate{
		Slug:             "team-general",
		Scope:            store.SandboxTemplateScopeTeam,
		OwnerID:          "general",
		Name:             "Team General",
		ParentTemplateID: &baseID,
		TopLayerID:       &teamTop,
		Version:          1,
	}
	if err := templates.Create(ctx, teamTpl); err != nil {
		t.Fatalf("Create team template: %v", err)
	}

	// resolveBaseLayerChain → ["@base", configuredTop].
	baseChain := resolveBaseLayerChainWith(ctx, tplStore)
	wantBase := []string{sandbox.BaseTemplateID, configuredTop}
	if !slicesEqual(baseChain, wantBase) {
		t.Errorf("Scenario 3: resolveBaseLayerChain = %v, want %v", baseChain, wantBase)
	}

	// resolveTemplateLayerChain → ["@base", configuredTop, teamTop].
	teamChain := resolveTemplateLayerChainWith(ctx, templates, "team-general")
	wantTeam := []string{sandbox.BaseTemplateID, configuredTop, teamTop}
	if !slicesEqual(teamChain, wantTeam) {
		t.Errorf("Scenario 3: resolveTemplateLayerChain = %v, want %v", teamChain, wantTeam)
	}

	// Effective chat chain: team chain wins (InjectSandboxLayerChain is called
	// before InjectSandboxLayerChainIfEmpty, so the non-nil team chain takes
	// precedence).
}

func TestChainScenario4_FreshInstall_TeamTemplate(t *testing.T) {
	pool := setupChainTestSchema(t)
	ctx := context.Background()

	tplStore := pgstore.NewPGSandboxTemplateStoreDirect(pool)
	templates := pgstore.NewPGSandboxTemplateStore(pool)
	layers := pgstore.NewPGLayerStore(pool)

	// @base is un-configured (top_layer_id = '@base' sentinel from migration).

	// Create team layer.
	teamTop := "3333333333333333333333333333333333333333333333333333333333333333"
	if err := layers.PutLayer(ctx, &store.SandboxLayer{
		LayerID:     teamTop,
		ParentLayer: strPtr(sandbox.BaseTemplateID),
		CephFSPath:  "/mnt/astonish-layers/" + teamTop,
		SizeBytes:   300,
	}); err != nil {
		t.Fatalf("PutLayer team: %v", err)
	}

	// Create team template with parent=@base.
	baseID := getBaseTemplateID(t, ctx, templates)
	teamTpl := &store.SandboxTemplate{
		Slug:             "team-general",
		Scope:            store.SandboxTemplateScopeTeam,
		OwnerID:          "general",
		Name:             "Team General",
		ParentTemplateID: &baseID,
		TopLayerID:       &teamTop,
		Version:          1,
	}
	if err := templates.Create(ctx, teamTpl); err != nil {
		t.Fatalf("Create team template: %v", err)
	}

	// resolveBaseLayerChain → nil (sentinel filtered).
	baseChain := resolveBaseLayerChainWith(ctx, tplStore)
	if baseChain != nil {
		t.Errorf("Scenario 4: resolveBaseLayerChain = %v, want nil", baseChain)
	}

	// resolveTemplateLayerChain → ["@base", teamTop].
	// The sentinel in CTE result is filtered; only teamTop survives.
	teamChain := resolveTemplateLayerChainWith(ctx, templates, "team-general")
	wantTeam := []string{sandbox.BaseTemplateID, teamTop}
	if !slicesEqual(teamChain, wantTeam) {
		t.Errorf("Scenario 4: resolveTemplateLayerChain = %v, want %v", teamChain, wantTeam)
	}

	// Effective chat chain: ["@base", teamTop] — team chain wins, base is nil.
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getBaseTemplateID retrieves the well-known @base template's UUID from the
// platform schema (seeded by migration 005).
func getBaseTemplateID(t *testing.T, ctx context.Context, templates store.SandboxTemplateStore) string {
	t.Helper()
	roots, err := templates.ListRoots(ctx)
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	for _, r := range roots {
		if r.Slug == "base" && r.Scope == store.SandboxTemplateScopeGlobal {
			return r.ID
		}
	}
	t.Fatal("@base template not found after migration")
	return ""
}
