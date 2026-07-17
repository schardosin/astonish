package api

import (
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Mock for baseTopLayerResolver (narrow interface used by resolveBaseLayerChainWith)
// ---------------------------------------------------------------------------

type mockBaseTopLayerResolver struct {
	topLayerID string
	err        error
}

func (m *mockBaseTopLayerResolver) GetBaseTopLayerID(_ context.Context) (string, error) {
	return m.topLayerID, m.err
}

// ---------------------------------------------------------------------------
// Mock template store with configurable Resolve behavior for chain tests.
// Extends the existing mockTemplateStore with explicit chain-return support.
// ---------------------------------------------------------------------------

type chainMockTemplateStore struct {
	mockTemplateStore
	// resolveChains maps template ID → layer IDs returned by Resolve.
	resolveChains map[string][]string
}

func newChainMockTemplateStore() *chainMockTemplateStore {
	return &chainMockTemplateStore{
		mockTemplateStore: mockTemplateStore{templates: make(map[string]*store.SandboxTemplate)},
		resolveChains:     make(map[string][]string),
	}
}

func (m *chainMockTemplateStore) Resolve(_ context.Context, id string) (*store.ResolvedTemplateChain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	chain, ok := m.resolveChains[id]
	if !ok {
		return nil, nil
	}
	return &store.ResolvedTemplateChain{TemplateID: id, LayerIDs: chain}, nil
}

// ---------------------------------------------------------------------------
// resolveBaseLayerChainWith tests
// ---------------------------------------------------------------------------

func TestResolveBaseLayerChainWith_EmptyTopLayer(t *testing.T) {
	mock := &mockBaseTopLayerResolver{topLayerID: ""}
	got := resolveBaseLayerChainWith(context.Background(), mock)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestResolveBaseLayerChainWith_SentinelOnly(t *testing.T) {
	// Fresh install: top_layer_id is the literal "@base" sentinel.
	// Resolver must return nil (no configured delta).
	mock := &mockBaseTopLayerResolver{topLayerID: sandbox.BaseTemplateID}
	got := resolveBaseLayerChainWith(context.Background(), mock)
	if got != nil {
		t.Errorf("expected nil for @base sentinel, got %v", got)
	}
}

func TestResolveBaseLayerChainWith_Configured(t *testing.T) {
	sha := "abc123def456"
	mock := &mockBaseTopLayerResolver{topLayerID: sha}
	got := resolveBaseLayerChainWith(context.Background(), mock)
	want := []string{sandbox.BaseTemplateID, sha}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveBaseLayerChainWith_DBError(t *testing.T) {
	mock := &mockBaseTopLayerResolver{topLayerID: "", err: context.DeadlineExceeded}
	got := resolveBaseLayerChainWith(context.Background(), mock)
	if got != nil {
		t.Errorf("expected nil on error, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// resolveTemplateLayerChainWith tests
// ---------------------------------------------------------------------------

func TestResolveTemplateLayerChainWith_BadSlug(t *testing.T) {
	ts := newChainMockTemplateStore()
	// Slug without "team-" prefix → nil.
	got := resolveTemplateLayerChainWith(context.Background(), ts, "my-template")
	if got != nil {
		t.Errorf("expected nil for non-team slug, got %v", got)
	}
}

func TestResolveTemplateLayerChainWith_NotFound(t *testing.T) {
	ts := newChainMockTemplateStore()
	got := resolveTemplateLayerChainWith(context.Background(), ts, "team-general")
	if got != nil {
		t.Errorf("expected nil for missing template, got %v", got)
	}
}

func TestResolveTemplateLayerChainWith_FreshBase_TeamLayer(t *testing.T) {
	// Scenario 4 (fresh install + team). CTE returns ["@base", "<team>"].
	// The sentinel must be filtered, resulting in ["@base", "<team>"].
	ts := newChainMockTemplateStore()
	teamTopLayer := "5bb51b4ee115571f488bf63ec29a8ce734dea6fa"
	ts.templates["tpl-1"] = &store.SandboxTemplate{
		ID:      "tpl-1",
		Slug:    "team-general",
		Scope:   store.SandboxTemplateScopeTeam,
		OwnerID: "general",
	}
	// CTE would return root's top_layer_id (@base sentinel) then team's top_layer_id.
	ts.resolveChains["tpl-1"] = []string{sandbox.BaseTemplateID, teamTopLayer}

	got := resolveTemplateLayerChainWith(context.Background(), ts, "team-general")
	want := []string{sandbox.BaseTemplateID, teamTopLayer}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveTemplateLayerChainWith_ConfiguredBase_TeamLayer(t *testing.T) {
	// Scenario 3 (configured base + team). CTE returns ["<configuredTop>", "<team>"].
	// Neither is the sentinel, both pass through.
	ts := newChainMockTemplateStore()
	configuredTop := "0b423684a56a68b128ba4daaa9a2459f407e1c36"
	teamTopLayer := "5bb51b4ee115571f488bf63ec29a8ce734dea6fa"
	ts.templates["tpl-2"] = &store.SandboxTemplate{
		ID:      "tpl-2",
		Slug:    "team-general",
		Scope:   store.SandboxTemplateScopeTeam,
		OwnerID: "general",
	}
	ts.resolveChains["tpl-2"] = []string{configuredTop, teamTopLayer}

	got := resolveTemplateLayerChainWith(context.Background(), ts, "team-general")
	want := []string{sandbox.BaseTemplateID, configuredTop, teamTopLayer}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveTemplateLayerChainWith_OnlySentinelInChain(t *testing.T) {
	// Degenerate: template exists but CTE only returns the sentinel.
	// This would happen if a template is created with parent=@base and
	// its own top_layer_id is nil (only root's sentinel propagates).
	ts := newChainMockTemplateStore()
	ts.templates["tpl-3"] = &store.SandboxTemplate{
		ID:      "tpl-3",
		Slug:    "team-empty",
		Scope:   store.SandboxTemplateScopeTeam,
		OwnerID: "empty",
	}
	ts.resolveChains["tpl-3"] = []string{sandbox.BaseTemplateID}

	got := resolveTemplateLayerChainWith(context.Background(), ts, "team-empty")
	if got != nil {
		t.Errorf("expected nil for sentinel-only chain, got %v", got)
	}
}

func TestResolveTemplateLayerChainWith_SingleTeamLayer(t *testing.T) {
	// Team template with no parent deltas (fresh base, team has layer).
	// CTE returns only team's layer (non-sentinel).
	ts := newChainMockTemplateStore()
	teamLayer := "deadbeef01234567"
	ts.templates["tpl-4"] = &store.SandboxTemplate{
		ID:      "tpl-4",
		Slug:    "team-dev",
		Scope:   store.SandboxTemplateScopeTeam,
		OwnerID: "dev",
	}
	ts.resolveChains["tpl-4"] = []string{teamLayer}

	got := resolveTemplateLayerChainWith(context.Background(), ts, "team-dev")
	want := []string{sandbox.BaseTemplateID, teamLayer}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveTemplateLayerChainWith_EmptyResolve(t *testing.T) {
	// Template exists but Resolve returns nil (broken DAG).
	ts := newChainMockTemplateStore()
	ts.templates["tpl-5"] = &store.SandboxTemplate{
		ID:      "tpl-5",
		Slug:    "team-broken",
		Scope:   store.SandboxTemplateScopeTeam,
		OwnerID: "broken",
	}
	// Don't add resolveChains entry → Resolve returns nil.

	got := resolveTemplateLayerChainWith(context.Background(), ts, "team-broken")
	if got != nil {
		t.Errorf("expected nil for empty resolve, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// resolveTemplateImageWith tests
// ---------------------------------------------------------------------------

func TestResolveTemplateImageWith_CustomImage(t *testing.T) {
	ts := newMockTemplateStore()
	img := "ghcr.io/sap/astonish-sandbox-general:5de96c7a79eb"
	ts.templates["tpl-img-1"] = &store.SandboxTemplate{
		ID:           "tpl-img-1",
		Slug:         "team-general",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: &img,
	}

	got := resolveTemplateImageWith(context.Background(), ts, "team-general")
	if got != img {
		t.Errorf("got %q, want %q", got, img)
	}
}

func TestResolveTemplateImageWith_NilImage_ReturnsEmpty(t *testing.T) {
	// Team template exists but SandboxImage is nil (reverted to default).
	ts := newMockTemplateStore()
	ts.templates["tpl-img-2"] = &store.SandboxTemplate{
		ID:           "tpl-img-2",
		Slug:         "team-general",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: nil,
	}

	got := resolveTemplateImageWith(context.Background(), ts, "team-general")
	if got != "" {
		t.Errorf("got %q, want empty string for nil SandboxImage", got)
	}
}

func TestResolveTemplateImageWith_EmptyStringImage_ReturnsEmpty(t *testing.T) {
	// Edge case: SandboxImage pointer to empty string.
	ts := newMockTemplateStore()
	empty := ""
	ts.templates["tpl-img-3"] = &store.SandboxTemplate{
		ID:           "tpl-img-3",
		Slug:         "team-general",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: &empty,
	}

	got := resolveTemplateImageWith(context.Background(), ts, "team-general")
	if got != "" {
		t.Errorf("got %q, want empty string for ptr-to-empty SandboxImage", got)
	}
}

func TestResolveTemplateImageWith_TemplateNotFound(t *testing.T) {
	ts := newMockTemplateStore()

	got := resolveTemplateImageWith(context.Background(), ts, "team-nonexistent")
	if got != "" {
		t.Errorf("got %q, want empty string for missing template", got)
	}
}

func TestResolveTemplateImageWith_BadSlugPrefix(t *testing.T) {
	// Slug without "team-" prefix should return empty immediately.
	ts := newMockTemplateStore()
	img := "docker.io/org/custom:v1"
	ts.templates["tpl-img-4"] = &store.SandboxTemplate{
		ID:           "tpl-img-4",
		Slug:         "my-template",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: &img,
	}

	got := resolveTemplateImageWith(context.Background(), ts, "my-template")
	if got != "" {
		t.Errorf("got %q, want empty string for non-team slug", got)
	}
}

// ---------------------------------------------------------------------------
// resolvePlatformDockerfileBodyWith tests
// ---------------------------------------------------------------------------

func TestResolvePlatformDockerfileBodyWith_HasBody(t *testing.T) {
	ts := newMockTemplateStore()
	body := "USER root\nRUN apt-get update && apt-get install -y git curl\nUSER sandbox"
	ts.templates["base-tpl"] = &store.SandboxTemplate{
		ID:             "base-tpl",
		Slug:           "base",
		Scope:          store.SandboxTemplateScopeGlobal,
		OwnerID:        "",
		DockerfileBody: &body,
	}

	got := resolvePlatformDockerfileBodyWith(context.Background(), ts)
	if got != body {
		t.Errorf("got %q, want %q", got, body)
	}
}

func TestResolvePlatformDockerfileBodyWith_NilBody(t *testing.T) {
	ts := newMockTemplateStore()
	ts.templates["base-tpl"] = &store.SandboxTemplate{
		ID:             "base-tpl",
		Slug:           "base",
		Scope:          store.SandboxTemplateScopeGlobal,
		OwnerID:        "",
		DockerfileBody: nil,
	}

	got := resolvePlatformDockerfileBodyWith(context.Background(), ts)
	if got != "" {
		t.Errorf("got %q, want empty string for nil DockerfileBody", got)
	}
}

func TestResolvePlatformDockerfileBodyWith_NoBaseTemplate(t *testing.T) {
	ts := newMockTemplateStore()
	// Empty store, no @base template exists.

	got := resolvePlatformDockerfileBodyWith(context.Background(), ts)
	if got != "" {
		t.Errorf("got %q, want empty string when @base template missing", got)
	}
}

// ---------------------------------------------------------------------------
// Image fallback chain tests
//
// These verify the core invariant: when a team has no custom image, the
// system falls through to the platform image; when neither exists, it falls
// through to the config default (represented as empty string here).
// ---------------------------------------------------------------------------

func TestImageFallbackChain_TeamCustom(t *testing.T) {
	// When a team has a custom image, it takes priority.
	ts := newMockTemplateStore()
	teamImg := "docker.io/org/team-sandbox:abc123"
	baseImg := "docker.io/org/platform-sandbox:def456"
	ts.templates["team-tpl"] = &store.SandboxTemplate{
		ID:           "team-tpl",
		Slug:         "team-general",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: &teamImg,
	}
	ts.templates["base-tpl"] = &store.SandboxTemplate{
		ID:           "base-tpl",
		Slug:         "base",
		Scope:        store.SandboxTemplateScopeGlobal,
		OwnerID:      "",
		SandboxImage: &baseImg,
	}

	// Simulate the chat handler fallback: team first, then base.
	ctx := context.Background()
	img := resolveTemplateImageWith(ctx, ts, "team-general")
	if img == "" {
		img = resolveBaseImageWith(ctx, ts)
	}
	if img != teamImg {
		t.Errorf("got %q, want team image %q to take priority", img, teamImg)
	}
}

func TestImageFallbackChain_TeamReverted_UsesPlatform(t *testing.T) {
	// Team reverted to default (SandboxImage = nil), platform has an image.
	ts := newMockTemplateStore()
	baseImg := "docker.io/org/platform-sandbox:def456"
	ts.templates["team-tpl"] = &store.SandboxTemplate{
		ID:           "team-tpl",
		Slug:         "team-general",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: nil, // reverted
	}
	ts.templates["base-tpl"] = &store.SandboxTemplate{
		ID:           "base-tpl",
		Slug:         "base",
		Scope:        store.SandboxTemplateScopeGlobal,
		OwnerID:      "",
		SandboxImage: &baseImg,
	}

	ctx := context.Background()
	img := resolveTemplateImageWith(ctx, ts, "team-general")
	if img == "" {
		img = resolveBaseImageWith(ctx, ts)
	}
	if img != baseImg {
		t.Errorf("got %q, want platform image %q when team reverted", img, baseImg)
	}
}

func TestImageFallbackChain_NoPlatform_UsesConfigDefault(t *testing.T) {
	// Neither team nor platform has a custom image → empty (config default).
	ts := newMockTemplateStore()
	ts.templates["team-tpl"] = &store.SandboxTemplate{
		ID:           "team-tpl",
		Slug:         "team-general",
		Scope:        store.SandboxTemplateScopeTeam,
		OwnerID:      "general",
		SandboxImage: nil,
	}
	ts.templates["base-tpl"] = &store.SandboxTemplate{
		ID:           "base-tpl",
		Slug:         "base",
		Scope:        store.SandboxTemplateScopeGlobal,
		OwnerID:      "",
		SandboxImage: nil,
	}

	ctx := context.Background()
	img := resolveTemplateImageWith(ctx, ts, "team-general")
	if img == "" {
		img = resolveBaseImageWith(ctx, ts)
	}
	if img != "" {
		t.Errorf("got %q, want empty string (config default) when nothing set", img)
	}
}
