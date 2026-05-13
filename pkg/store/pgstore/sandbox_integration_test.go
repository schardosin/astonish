//go:build integration

package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// ---------------------------------------------------------------------------
// Helpers for Phase A sandbox stores.
//
// The SandboxTemplateStore and LayerStore use UNQUALIFIED table names
// (sandbox_templates / sandbox_layers) and rely on search_path to resolve
// them. For isolation each test gets its own schema and a dedicated pool
// whose AfterConnect hook pins search_path to that schema.
//
// ChatEventJournal is schema-qualified by construction so the shared
// testPool + setupTestSchema helpers from memories_integration_test.go work.
// ---------------------------------------------------------------------------

// setupPlatformTestSchema creates a fresh schema, applies the platform
// migration set against it (via a conn whose search_path is pinned), and
// returns (pinnedPool, schemaName). The pool's connections all have
// search_path = <schema>, so unqualified DDL/DML in the Phase A migration
// and in pgSandboxTemplateStore / pgLayerStore resolve correctly.
//
// Cleanup drops the schema and closes the pool.
func setupPlatformTestSchema(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	// Acquire the shared pool just to create the schema.
	bootstrap := testPool(t)
	ctx := context.Background()
	schema := testSchema(t)
	if _, err := bootstrap.Exec(ctx,
		fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schema}.Sanitize()),
	); err != nil {
		t.Fatalf("create platform test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrap.Exec(context.Background(),
			fmt.Sprintf("DROP SCHEMA %s CASCADE", pgx.Identifier{schema}.Sanitize()))
	})

	// Build a dedicated pool pinned to the test schema.
	dsn := mustTestDSN(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.MaxConns = 4
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		// Include public so the pgvector type (installed in public) and any
		// platform tables created there by earlier tests remain resolvable.
		_, err := c.Exec(ctx,
			fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
		)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create pinned pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Apply platform migrations via a dedicated conn (must also have
	// search_path pinned).
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for migrations: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx,
		fmt.Sprintf("SET search_path TO %s, public", pgx.Identifier{schema}.Sanitize()),
	); err != nil {
		t.Fatalf("pin migration search_path: %v", err)
	}
	if err := Migrate(ctx, conn, MigrationPlatform, schema); err != nil {
		t.Fatalf("apply platform migrations: %v", err)
	}

	return pool, schema
}

// mustTestDSN returns the test DSN or skips the test. Mirrors the skip
// semantics of testPool().
func mustTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		t.Skip("ASTONISH_TEST_DSN not set; skipping integration test")
	}
	return dsn
}

// seedBaseTemplate inserts the global "@base" root so child templates can
// reference it via parent_template_id. Returns the base template ID.
func seedBaseTemplate(t *testing.T, ctx context.Context, ts store.SandboxTemplateStore) string {
	t.Helper()
	base := &store.SandboxTemplate{
		Slug:    "base",
		Scope:   store.SandboxTemplateScopeGlobal,
		OwnerID: "",
		Name:    "@base",
		Version: 1,
	}
	if err := ts.Create(ctx, base); err != nil {
		t.Fatalf("seed @base: %v", err)
	}
	if base.ID == "" {
		t.Fatal("seed @base: ID not populated")
	}
	return base.ID
}

// ---------------------------------------------------------------------------
// SandboxTemplateStore integration tests
// ---------------------------------------------------------------------------

func TestPGSandboxTemplateStore_CRUD(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)

	baseID := seedBaseTemplate(t, ctx, ts)

	orgID := uuid.New().String()
	tpl := &store.SandboxTemplate{
		Slug:             "node-dev",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "Node Dev",
		Description:      "Node 22 + pnpm",
		ParentTemplateID: &baseID,
		Version:          1,
	}
	if err := ts.Create(ctx, tpl); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tpl.ID == "" {
		t.Fatal("Create did not populate ID")
	}

	got, err := ts.GetByID(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Slug != "node-dev" || got.OwnerID != orgID {
		t.Errorf("GetByID mismatch: slug=%q owner=%q", got.Slug, got.OwnerID)
	}
	if got.ParentTemplateID == nil || *got.ParentTemplateID != baseID {
		t.Errorf("ParentTemplateID = %v, want %v", got.ParentTemplateID, baseID)
	}

	gotBySlug, err := ts.GetBySlug(ctx, store.SandboxTemplateScopeOrg, orgID, "node-dev")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if gotBySlug == nil || gotBySlug.ID != tpl.ID {
		t.Errorf("GetBySlug mismatch")
	}

	// Update mutable fields.
	tpl.Name = "Node Dev v2"
	tpl.Description = "Node 22 + pnpm + corepack"
	tpl.Version = 2
	if err := ts.Update(ctx, tpl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = ts.GetByID(ctx, tpl.ID)
	if got.Name != "Node Dev v2" || got.Version != 2 {
		t.Errorf("Update did not persist: name=%q version=%d", got.Name, got.Version)
	}

	// List with filter.
	tpls, err := ts.List(ctx, store.SandboxTemplateFilter{
		Scope:   store.SandboxTemplateScopeOrg,
		OwnerID: orgID,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tpls) != 1 {
		t.Errorf("List returned %d templates, want 1", len(tpls))
	}

	// Delete.
	if err := ts.Delete(ctx, tpl.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = ts.GetByID(ctx, tpl.ID)
	if got != nil {
		t.Error("GetByID after Delete returned non-nil")
	}
}

func TestPGSandboxTemplateStore_UniqueSlug(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)
	baseID := seedBaseTemplate(t, ctx, ts)

	orgID := uuid.New().String()
	first := &store.SandboxTemplate{
		Slug:             "dupe",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "First",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := &store.SandboxTemplate{
		Slug:             "dupe",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "Second",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, second); err == nil {
		t.Fatal("expected unique-violation error on duplicate (scope, owner, slug)")
	}
}

func TestPGSandboxTemplateStore_Resolve(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)
	ls := NewPGLayerStore(pool)

	// Seed 3 layers.
	layerA := seedLayer(t, ctx, ls, "aaaaaaaaaaaaaaaa")
	layerB := seedLayer(t, ctx, ls, "bbbbbbbbbbbbbbbb")
	layerC := seedLayer(t, ctx, ls, "cccccccccccccccc")

	// Base has layerA as top.
	base := &store.SandboxTemplate{
		Slug:       "base",
		Scope:      store.SandboxTemplateScopeGlobal,
		Name:       "@base",
		TopLayerID: &layerA,
	}
	if err := ts.Create(ctx, base); err != nil {
		t.Fatalf("create base: %v", err)
	}
	orgID := uuid.New().String()
	mid := &store.SandboxTemplate{
		Slug:             "org-customized",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          orgID,
		Name:             "Org customized",
		ParentTemplateID: &base.ID,
		TopLayerID:       &layerB,
	}
	if err := ts.Create(ctx, mid); err != nil {
		t.Fatalf("create mid: %v", err)
	}
	userID := uuid.New().String()
	leaf := &store.SandboxTemplate{
		Slug:             "my-box",
		Scope:            store.SandboxTemplateScopePersonal,
		OwnerID:          userID,
		Name:             "My box",
		ParentTemplateID: &mid.ID,
		TopLayerID:       &layerC,
	}
	if err := ts.Create(ctx, leaf); err != nil {
		t.Fatalf("create leaf: %v", err)
	}

	chain, err := ts.Resolve(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if chain == nil {
		t.Fatal("Resolve returned nil for existing template")
	}
	want := []string{layerA, layerB, layerC} // oldest first
	if len(chain.LayerIDs) != 3 {
		t.Fatalf("LayerIDs len = %d, want 3: %v", len(chain.LayerIDs), chain.LayerIDs)
	}
	for i, got := range chain.LayerIDs {
		if got != want[i] {
			t.Errorf("LayerIDs[%d] = %q, want %q", i, got, want[i])
		}
	}

	// Missing template returns nil, nil.
	chain, err = ts.Resolve(ctx, uuid.New().String())
	if err != nil {
		t.Fatalf("Resolve missing: %v", err)
	}
	if chain != nil {
		t.Errorf("Resolve missing returned %+v, want nil", chain)
	}
}

func TestPGSandboxTemplateStore_CycleRejected(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)
	baseID := seedBaseTemplate(t, ctx, ts)

	// Create A → base.
	a := &store.SandboxTemplate{
		Slug:             "a",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          uuid.New().String(),
		Name:             "A",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, a); err != nil {
		t.Fatalf("create A: %v", err)
	}

	// Try to repoint A's parent to itself — must be rejected either by the
	// Update's immutability contract or by the cycle trigger if attempted
	// at raw SQL level. We exercise both paths.
	// 1) Update() is documented to ignore parent changes; the row stays
	//    pointed at baseID. Not a cycle test per se, but confirms immutability.
	selfID := a.ID
	a.ParentTemplateID = &selfID
	if err := ts.Update(ctx, a); err != nil {
		t.Fatalf("Update (should ignore parent change): %v", err)
	}
	got, _ := ts.GetByID(ctx, a.ID)
	if got.ParentTemplateID == nil || *got.ParentTemplateID != baseID {
		t.Errorf("Parent mutated through Update: got=%v, want=%v",
			got.ParentTemplateID, baseID)
	}

	// 2) Direct SQL cycle attempt should fire the trigger.
	_, err := pool.Exec(ctx,
		`UPDATE sandbox_templates SET parent_template_id = id WHERE id = $1`,
		a.ID,
	)
	if err == nil {
		t.Error("expected cycle trigger to reject self-parent UPDATE")
	}
}

func TestPGSandboxTemplateStore_ListRoots(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ts := NewPGSandboxTemplateStore(pool)

	baseID := seedBaseTemplate(t, ctx, ts)
	child := &store.SandboxTemplate{
		Slug:             "child",
		Scope:            store.SandboxTemplateScopeOrg,
		OwnerID:          uuid.New().String(),
		Name:             "Child",
		ParentTemplateID: &baseID,
	}
	if err := ts.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	roots, err := ts.ListRoots(ctx)
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("ListRoots returned %d roots, want 1", len(roots))
	}
	if roots[0].ID != baseID {
		t.Errorf("ListRoots[0].ID = %q, want %q", roots[0].ID, baseID)
	}
}

// ---------------------------------------------------------------------------
// LayerStore integration tests
// ---------------------------------------------------------------------------

// seedLayer inserts a layer with a unique synthetic hex ID (64 chars to pass
// as a plausible sha256). Returns the ID.
func seedLayer(t *testing.T, ctx context.Context, ls store.LayerStore, seed string) string {
	t.Helper()
	id := fmt.Sprintf("%064s", seed)[:64]
	layer := &store.SandboxLayer{
		LayerID:    id,
		CephFSPath: "/mnt/astonish-layers/" + id,
		SizeBytes:  1024,
	}
	if err := ls.PutLayer(ctx, layer); err != nil {
		t.Fatalf("PutLayer(%s): %v", id, err)
	}
	return id
}

func TestPGLayerStore_PutAndGet(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ls := NewPGLayerStore(pool)

	id := seedLayer(t, ctx, ls, "abcdef1234567890")
	got, err := ls.GetLayer(ctx, id)
	if err != nil {
		t.Fatalf("GetLayer: %v", err)
	}
	if got == nil {
		t.Fatal("GetLayer returned nil")
	}
	if got.LayerID != id {
		t.Errorf("LayerID = %q, want %q", got.LayerID, id)
	}
	if got.RefCount != 0 {
		t.Errorf("RefCount = %d, want 0 on fresh layer", got.RefCount)
	}
	if got.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want 1024", got.SizeBytes)
	}

	// Idempotent re-put: must succeed and must not change ref_count.
	if err := ls.IncrementRefCount(ctx, id); err != nil {
		t.Fatalf("IncrementRefCount: %v", err)
	}
	dup := &store.SandboxLayer{
		LayerID:    id,
		CephFSPath: "/different/path",
		SizeBytes:  99999,
	}
	if err := ls.PutLayer(ctx, dup); err != nil {
		t.Fatalf("PutLayer idempotent: %v", err)
	}
	got, _ = ls.GetLayer(ctx, id)
	if got.RefCount != 1 {
		t.Errorf("RefCount after idempotent put = %d, want 1", got.RefCount)
	}
	if got.SizeBytes != 1024 {
		t.Errorf("SizeBytes after idempotent put = %d, want 1024 (unchanged)", got.SizeBytes)
	}

	// Missing layer.
	got, err = ls.GetLayer(ctx, fmt.Sprintf("%064s", "deadbeef"))
	if err != nil {
		t.Fatalf("GetLayer missing: %v", err)
	}
	if got != nil {
		t.Errorf("GetLayer missing returned %+v", got)
	}
}

func TestPGLayerStore_RefCountLifecycle(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ls := NewPGLayerStore(pool)

	id := seedLayer(t, ctx, ls, "refcount")

	// Inc twice → 2.
	if err := ls.IncrementRefCount(ctx, id); err != nil {
		t.Fatalf("Inc 1: %v", err)
	}
	if err := ls.IncrementRefCount(ctx, id); err != nil {
		t.Fatalf("Inc 2: %v", err)
	}
	got, _ := ls.GetLayer(ctx, id)
	if got.RefCount != 2 {
		t.Errorf("RefCount after 2xInc = %d, want 2", got.RefCount)
	}

	// Dec once → 1.
	if err := ls.DecrementRefCount(ctx, id); err != nil {
		t.Fatalf("Dec 1: %v", err)
	}
	got, _ = ls.GetLayer(ctx, id)
	if got.RefCount != 1 {
		t.Errorf("RefCount after Dec = %d, want 1", got.RefCount)
	}

	// DeleteLayer must refuse while referenced.
	if err := ls.DeleteLayer(ctx, id); err == nil {
		t.Error("DeleteLayer on referenced layer should fail")
	}

	// Dec to 0.
	if err := ls.DecrementRefCount(ctx, id); err != nil {
		t.Fatalf("Dec 2: %v", err)
	}

	// Further Dec at 0 must error.
	if err := ls.DecrementRefCount(ctx, id); err == nil {
		t.Error("Decrement at 0 should error")
	}

	// Inc on missing layer must error.
	if err := ls.IncrementRefCount(ctx, fmt.Sprintf("%064s", "missing")); err == nil {
		t.Error("Increment missing layer should error")
	}

	// DeleteLayer at 0 now succeeds.
	if err := ls.DeleteLayer(ctx, id); err != nil {
		t.Fatalf("DeleteLayer at 0: %v", err)
	}
	got, _ = ls.GetLayer(ctx, id)
	if got != nil {
		t.Errorf("layer still present after Delete: %+v", got)
	}
}

func TestPGLayerStore_ListUnreferenced(t *testing.T) {
	pool, _ := setupPlatformTestSchema(t)
	ctx := context.Background()
	ls := NewPGLayerStore(pool)

	unref := seedLayer(t, ctx, ls, "unref")
	refd := seedLayer(t, ctx, ls, "refd")
	if err := ls.IncrementRefCount(ctx, refd); err != nil {
		t.Fatalf("Inc refd: %v", err)
	}

	// With grace=0 both-layers-old enough, only unref shows.
	got, err := ls.ListUnreferenced(ctx, 0)
	if err != nil {
		t.Fatalf("ListUnreferenced: %v", err)
	}
	if len(got) != 1 || got[0].LayerID != unref {
		ids := make([]string, len(got))
		for i, l := range got {
			ids[i] = l.LayerID
		}
		t.Errorf("ListUnreferenced = %v, want [%s]", ids, unref)
	}

	// Large grace: nothing qualifies (layer just added).
	got, err = ls.ListUnreferenced(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ListUnreferenced grace=1h: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListUnreferenced grace=1h returned %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// ChatEventJournal integration tests (team-schema)
// ---------------------------------------------------------------------------

// seedSession inserts a minimal sessions row so the FK + last_seq UPDATE land.
func seedSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, id string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %s.sessions (id) VALUES ($1)`,
			pgx.Identifier{schema}.Sanitize()),
		id,
	)
	if err != nil {
		t.Fatalf("seed session %s: %v", id, err)
	}
}

func TestPGChatEventJournal_AppendAndRead(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	chatID := uuid.New().String()
	seedSession(t, ctx, pool, schema, chatID)

	// LastSeq on fresh session = 0.
	seq, err := j.LastSeq(ctx, chatID)
	if err != nil {
		t.Fatalf("LastSeq fresh: %v", err)
	}
	if seq != 0 {
		t.Errorf("LastSeq fresh = %d, want 0", seq)
	}

	// Append 3 events in one batch.
	payload := func(s string) []byte {
		b, _ := json.Marshal(map[string]string{"msg": s})
		return b
	}
	events := []*store.ChatEvent{
		{ChatSessionID: chatID, EventType: "user.message", Payload: payload("hi"), ProducerPod: "pod-a"},
		{ChatSessionID: chatID, EventType: "assistant.partial", Payload: payload("think"), ProducerPod: "pod-a"},
		{ChatSessionID: chatID, EventType: "assistant.final", Payload: payload("done"), ProducerPod: "pod-a"},
	}
	if err := j.Append(ctx, events); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Seq values assigned in order.
	for i, e := range events {
		wantSeq := int64(i + 1)
		if e.Seq != wantSeq {
			t.Errorf("events[%d].Seq = %d, want %d", i, e.Seq, wantSeq)
		}
	}

	seq, _ = j.LastSeq(ctx, chatID)
	if seq != 3 {
		t.Errorf("LastSeq after batch = %d, want 3", seq)
	}

	// Append another batch — seq continues.
	more := []*store.ChatEvent{
		{ChatSessionID: chatID, EventType: "user.message", Payload: payload("again"), ProducerPod: "pod-b"},
	}
	if err := j.Append(ctx, more); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if more[0].Seq != 4 {
		t.Errorf("second batch Seq = %d, want 4", more[0].Seq)
	}

	// ReadSince(0) → all 4 events.
	got, err := j.ReadSince(ctx, chatID, 0, 0)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("ReadSince returned %d events, want 4", len(got))
	}
	for i, e := range got {
		if e.Seq != int64(i+1) {
			t.Errorf("got[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
	}

	// ReadSince(2) → events 3 and 4.
	got, err = j.ReadSince(ctx, chatID, 2, 0)
	if err != nil {
		t.Fatalf("ReadSince(2): %v", err)
	}
	if len(got) != 2 || got[0].Seq != 3 || got[1].Seq != 4 {
		seqs := make([]int64, len(got))
		for i, e := range got {
			seqs[i] = e.Seq
		}
		t.Errorf("ReadSince(2) seqs = %v, want [3 4]", seqs)
	}

	// ReadSince with limit.
	got, err = j.ReadSince(ctx, chatID, 0, 2)
	if err != nil {
		t.Fatalf("ReadSince limit: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ReadSince limit=2 returned %d", len(got))
	}
}

func TestPGChatEventJournal_AppendRejectsMixedChat(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	chatA := uuid.New().String()
	chatB := uuid.New().String()
	seedSession(t, ctx, pool, schema, chatA)
	seedSession(t, ctx, pool, schema, chatB)

	events := []*store.ChatEvent{
		{ChatSessionID: chatA, EventType: "x", Payload: []byte("{}")},
		{ChatSessionID: chatB, EventType: "x", Payload: []byte("{}")},
	}
	if err := j.Append(ctx, events); err == nil {
		t.Error("expected error on mixed-chat batch")
	}

	// Neither session should have advanced.
	for _, id := range []string{chatA, chatB} {
		seq, err := j.LastSeq(ctx, id)
		if err != nil {
			t.Fatalf("LastSeq %s: %v", id, err)
		}
		if seq != 0 {
			t.Errorf("LastSeq %s = %d after failed mixed batch, want 0", id, seq)
		}
	}
}

func TestPGChatEventJournal_AppendUnknownChat(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	events := []*store.ChatEvent{
		{ChatSessionID: uuid.New().String(), EventType: "x", Payload: []byte("{}")},
	}
	if err := j.Append(ctx, events); err == nil {
		t.Error("expected error when chat session does not exist")
	}
}

func TestPGChatEventJournal_EmptyBatchNoop(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()

	j := NewPGChatEventJournal(pool, schema)
	if err := j.Append(ctx, nil); err != nil {
		t.Errorf("Append(nil) = %v, want nil", err)
	}
	if err := j.Append(ctx, []*store.ChatEvent{}); err != nil {
		t.Errorf("Append([]) = %v, want nil", err)
	}
}
