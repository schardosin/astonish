// Package filestore wraps the existing Astonish file-based storage systems
// behind the abstract store.Store interfaces.
//
// Sandbox template / layer / event-journal behavior in personal mode:
//
//   - SandboxTemplateStore: reads are served from pkg/sandbox.TemplateRegistry
//     (existing JSON file). Writes (Create/Update/Delete) are supported in a
//     degraded mode that preserves only the Name, Description, CreatedAt, and
//     UpdatedAt fields -- Scope, OwnerID, ParentTemplateID, TopLayerID are
//     dropped per the personal-mode invariants (sandbox-backends.md §6.4).
//     Resolve returns ErrUnsupported (the DAG model is platform-only).
//
//   - LayerStore: every method returns ErrUnsupported. Personal-mode Incus
//     uses snapshots, not the content-addressed layer store.
//
//   - ChatEventJournal: every method returns ErrUnsupported. Personal mode is
//     a single process; cross-pod continuity is meaningless.
package filestore

import (
	"context"
	"errors"
	"time"

	"github.com/schardosin/astonish/pkg/sandbox/tmplmeta"
	"github.com/schardosin/astonish/pkg/store"
)

// --------------------------------------------------------------------------
// SandboxTemplateStore wrapper
// --------------------------------------------------------------------------

// sandboxTemplateStore wraps the existing TemplateRegistry (JSON file-backed)
// in the store.SandboxTemplateStore interface. Reads map TemplateMeta rows to
// store.SandboxTemplate with flat personal-scope semantics. Writes persist a
// subset of fields to the JSON file; DAG fields are accepted in the API but
// silently dropped.
type sandboxTemplateStore struct {
	reg *tmplmeta.TemplateRegistry
}

// NewSandboxTemplateStore wraps the given TemplateRegistry. If reg is nil,
// the store is a no-op that returns ErrUnsupported for every method -- useful
// in tests or when the caller explicitly wants no filesystem interaction.
func NewSandboxTemplateStore(reg *tmplmeta.TemplateRegistry) store.SandboxTemplateStore {
	return &sandboxTemplateStore{reg: reg}
}

func (s *sandboxTemplateStore) Create(ctx context.Context, tpl *store.SandboxTemplate) error {
	if s.reg == nil {
		return store.ErrUnsupported
	}
	if tpl == nil || tpl.Slug == "" {
		return errors.New("sandbox template slug is required")
	}
	now := time.Now().UTC()
	if tpl.CreatedAt.IsZero() {
		tpl.CreatedAt = now
	}
	tpl.UpdatedAt = now
	meta := &tmplmeta.TemplateMeta{
		Name:        tpl.Slug,
		Description: tpl.Description,
		CreatedAt:   tpl.CreatedAt,
	}
	if err := s.reg.Add(meta); err != nil {
		return err
	}
	if tpl.ID == "" {
		tpl.ID = tpl.Slug // personal mode uses slug-as-id
	}
	return nil
}

func (s *sandboxTemplateStore) GetByID(ctx context.Context, id string) (*store.SandboxTemplate, error) {
	if s.reg == nil {
		return nil, store.ErrUnsupported
	}
	meta := s.reg.Get(id)
	if meta == nil {
		return nil, nil
	}
	return metaToTemplate(meta), nil
}

func (s *sandboxTemplateStore) GetBySlug(ctx context.Context, scope store.SandboxTemplateScope, ownerID, slug string) (*store.SandboxTemplate, error) {
	if s.reg == nil {
		return nil, store.ErrUnsupported
	}
	// Personal mode ignores scope and ownerID by design (§6.4).
	_ = scope
	_ = ownerID
	meta := s.reg.Get(slug)
	if meta == nil {
		return nil, nil
	}
	return metaToTemplate(meta), nil
}

func (s *sandboxTemplateStore) List(ctx context.Context, filter store.SandboxTemplateFilter) ([]*store.SandboxTemplate, error) {
	if s.reg == nil {
		return nil, store.ErrUnsupported
	}
	// Personal mode: filter is ignored. All templates are scope=personal.
	_ = filter
	metas := s.reg.List()
	out := make([]*store.SandboxTemplate, 0, len(metas))
	for _, m := range metas {
		out = append(out, metaToTemplate(m))
	}
	return out, nil
}

func (s *sandboxTemplateStore) Update(ctx context.Context, tpl *store.SandboxTemplate) error {
	if s.reg == nil {
		return store.ErrUnsupported
	}
	if tpl == nil || tpl.Slug == "" {
		return errors.New("sandbox template slug is required")
	}
	existing := s.reg.Get(tpl.Slug)
	if existing == nil {
		return errors.New("sandbox template not found")
	}
	existing.Description = tpl.Description
	tpl.UpdatedAt = time.Now().UTC()
	return s.reg.Update(existing)
}

func (s *sandboxTemplateStore) Delete(ctx context.Context, id string) error {
	if s.reg == nil {
		return store.ErrUnsupported
	}
	return s.reg.Remove(id)
}

// Resolve is platform-only: the DAG does not exist in personal mode (§6.4).
func (s *sandboxTemplateStore) Resolve(ctx context.Context, id string) (*store.ResolvedTemplateChain, error) {
	return nil, store.ErrUnsupported
}

func (s *sandboxTemplateStore) ListRoots(ctx context.Context) ([]*store.SandboxTemplate, error) {
	if s.reg == nil {
		return nil, store.ErrUnsupported
	}
	// Every template in personal mode is conceptually a root (no DAG).
	return s.List(ctx, store.SandboxTemplateFilter{})
}

func (s *sandboxTemplateStore) GetBaseConfig(context.Context) (*store.BaseConfigInfo, error) {
	return nil, store.ErrUnsupported
}

func (s *sandboxTemplateStore) SetBaseConfig(context.Context, string, []byte, string) error {
	return store.ErrUnsupported
}

func (s *sandboxTemplateStore) GetBaseTopLayerID(context.Context) (string, error) {
	return "", store.ErrUnsupported
}

func (s *sandboxTemplateStore) AcquireBuildLock(context.Context) (bool, func(), error) {
	return false, nil, store.ErrUnsupported
}

func (s *sandboxTemplateStore) IsBuildInProgress(context.Context) (bool, error) {
	return false, store.ErrUnsupported
}

func metaToTemplate(m *tmplmeta.TemplateMeta) *store.SandboxTemplate {
	return &store.SandboxTemplate{
		ID:          m.Name,
		Slug:        m.Name,
		Scope:       store.SandboxTemplateScopePersonal,
		OwnerID:     "",
		Name:        m.Name,
		Description: m.Description,
		Version:     1,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.CreatedAt,
	}
}

// --------------------------------------------------------------------------
// LayerStore stub
// --------------------------------------------------------------------------

// unsupportedLayerStore returns store.ErrUnsupported for every method.
// Personal-mode Incus uses snapshots, not the content-addressed layer store.
type unsupportedLayerStore struct{}

// NewLayerStore returns a LayerStore that refuses every operation.
// This is the filestore/personal-mode implementation.
func NewLayerStore() store.LayerStore { return &unsupportedLayerStore{} }

func (unsupportedLayerStore) PutLayer(context.Context, *store.SandboxLayer) error {
	return store.ErrUnsupported
}
func (unsupportedLayerStore) GetLayer(context.Context, string) (*store.SandboxLayer, error) {
	return nil, store.ErrUnsupported
}
func (unsupportedLayerStore) IncrementRefCount(context.Context, string) error {
	return store.ErrUnsupported
}
func (unsupportedLayerStore) DecrementRefCount(context.Context, string) error {
	return store.ErrUnsupported
}
func (unsupportedLayerStore) ListUnreferenced(context.Context, time.Duration) ([]*store.SandboxLayer, error) {
	return nil, store.ErrUnsupported
}
func (unsupportedLayerStore) ListAll(context.Context) ([]*store.SandboxLayer, error) {
	return nil, store.ErrUnsupported
}
func (unsupportedLayerStore) DeleteLayer(context.Context, string) error {
	return store.ErrUnsupported
}

// --------------------------------------------------------------------------
// ChatEventJournal stub
// --------------------------------------------------------------------------

// unsupportedChatEventJournal returns store.ErrUnsupported for every method.
// Personal mode is a single process; cross-pod continuity is meaningless.
type unsupportedChatEventJournal struct{}

// NewChatEventJournal returns a journal that refuses every operation. This is
// the filestore/personal-mode implementation.
func NewChatEventJournal() store.ChatEventJournal { return &unsupportedChatEventJournal{} }

func (unsupportedChatEventJournal) Append(context.Context, []*store.ChatEvent) error {
	return store.ErrUnsupported
}
func (unsupportedChatEventJournal) ReadSince(context.Context, string, int64, int) ([]*store.ChatEvent, error) {
	return nil, store.ErrUnsupported
}
func (unsupportedChatEventJournal) LastSeq(context.Context, string) (int64, error) {
	return 0, store.ErrUnsupported
}

// Compile-time assertions.
var (
	_ store.SandboxTemplateStore = (*sandboxTemplateStore)(nil)
	_ store.LayerStore           = (*unsupportedLayerStore)(nil)
	_ store.ChatEventJournal     = (*unsupportedChatEventJournal)(nil)
)
