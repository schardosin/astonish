package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

// fakeTemplateStore is an in-memory SandboxTemplateStore for resolver tests.
type fakeTemplateStore struct {
	byID   map[string]*store.SandboxTemplate
	bySlug map[string]*store.SandboxTemplate // key = scope|ownerID|slug

	// erroringScopes returns store.ErrUnsupported for matching scopes.
	erroringScopes map[store.SandboxTemplateScope]bool
}

func newFakeTemplateStore(tpls ...*store.SandboxTemplate) *fakeTemplateStore {
	f := &fakeTemplateStore{
		byID:           map[string]*store.SandboxTemplate{},
		bySlug:         map[string]*store.SandboxTemplate{},
		erroringScopes: map[store.SandboxTemplateScope]bool{},
	}
	for _, t := range tpls {
		f.byID[t.ID] = t
		f.bySlug[string(t.Scope)+"|"+t.OwnerID+"|"+t.Slug] = t
	}
	return f
}

func (f *fakeTemplateStore) Create(context.Context, *store.SandboxTemplate) error {
	return errors.New("not implemented")
}
func (f *fakeTemplateStore) GetByID(_ context.Context, id string) (*store.SandboxTemplate, error) {
	return f.byID[id], nil
}
func (f *fakeTemplateStore) GetBySlug(_ context.Context, scope store.SandboxTemplateScope, ownerID, slug string) (*store.SandboxTemplate, error) {
	if f.erroringScopes[scope] {
		return nil, store.ErrUnsupported
	}
	return f.bySlug[string(scope)+"|"+ownerID+"|"+slug], nil
}
func (f *fakeTemplateStore) List(_ context.Context, filter store.SandboxTemplateFilter) ([]*store.SandboxTemplate, error) {
	if f.erroringScopes[filter.Scope] {
		return nil, store.ErrUnsupported
	}
	var out []*store.SandboxTemplate
	for _, t := range f.byID {
		if filter.Scope != "" && t.Scope != filter.Scope {
			continue
		}
		if filter.OwnerID != "" && t.OwnerID != filter.OwnerID {
			continue
		}
		if filter.Purpose != "" && t.Purpose != filter.Purpose {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
func (f *fakeTemplateStore) Update(context.Context, *store.SandboxTemplate) error {
	return errors.New("not implemented")
}
func (f *fakeTemplateStore) Delete(context.Context, string) error {
	return errors.New("not implemented")
}
func (f *fakeTemplateStore) Resolve(context.Context, string) (*store.ResolvedTemplateChain, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTemplateStore) ListRoots(context.Context) ([]*store.SandboxTemplate, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTemplateStore) GetBaseConfig(context.Context) (*store.BaseConfigInfo, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTemplateStore) SetBaseConfig(context.Context, string, []byte, string) error {
	return errors.New("not implemented")
}
func (f *fakeTemplateStore) GetBaseTopLayerID(context.Context) (string, error) {
	return "", errors.New("not implemented")
}
func (f *fakeTemplateStore) AcquireBuildLock(context.Context) (bool, func(), error) {
	return false, nil, errors.New("not implemented")
}
func (f *fakeTemplateStore) IsBuildInProgress(context.Context) (bool, error) {
	return false, errors.New("not implemented")
}
func (f *fakeTemplateStore) AcquireTemplateBuildLock(context.Context, string) (bool, func(), error) {
	return false, nil, errors.New("not implemented")
}
func (f *fakeTemplateStore) IsTemplateBuildInProgress(context.Context, string) (bool, error) {
	return false, errors.New("not implemented")
}

func tpl(id, slug string, scope store.SandboxTemplateScope, owner string) *store.SandboxTemplate {
	return &store.SandboxTemplate{
		ID:        id,
		Slug:      slug,
		Scope:     scope,
		OwnerID:   owner,
		Name:      slug,
		Version:   1,
		CreatedAt: time.Unix(0, 0),
		UpdatedAt: time.Unix(0, 0),
	}
}

// TestResolve_Cascade verifies the personal > team > org > global ordering.
func TestResolve_Cascade(t *testing.T) {
	personalT := tpl("p1", "personal-default", store.SandboxTemplateScopePersonal, "user-1")
	teamT := tpl("t1", "team-default", store.SandboxTemplateScopeTeam, "team-1")
	orgT := tpl("o1", "org-default", store.SandboxTemplateScopeOrg, "org-1")
	baseT := tpl("b1", "base", store.SandboxTemplateScopeGlobal, "")

	cases := []struct {
		name    string
		present []*store.SandboxTemplate
		req     DefaultTemplateRequest
		want    string // expected template ID
	}{
		{
			name:    "all present returns personal",
			present: []*store.SandboxTemplate{personalT, teamT, orgT, baseT},
			req:     DefaultTemplateRequest{UserID: "user-1", TeamID: "team-1", OrgID: "org-1"},
			want:    "p1",
		},
		{
			name:    "no personal returns team",
			present: []*store.SandboxTemplate{teamT, orgT, baseT},
			req:     DefaultTemplateRequest{UserID: "user-1", TeamID: "team-1", OrgID: "org-1"},
			want:    "t1",
		},
		{
			name:    "no personal or team returns org",
			present: []*store.SandboxTemplate{orgT, baseT},
			req:     DefaultTemplateRequest{UserID: "user-1", TeamID: "team-1", OrgID: "org-1"},
			want:    "o1",
		},
		{
			name:    "only base returns base",
			present: []*store.SandboxTemplate{baseT},
			req:     DefaultTemplateRequest{UserID: "user-1", TeamID: "team-1", OrgID: "org-1"},
			want:    "b1",
		},
		{
			name:    "personal mode (no team/org) falls straight to personal",
			present: []*store.SandboxTemplate{personalT},
			req:     DefaultTemplateRequest{UserID: "user-1"},
			want:    "p1",
		},
		{
			name:    "personal mode with only base falls back to base",
			present: []*store.SandboxTemplate{baseT},
			req:     DefaultTemplateRequest{UserID: "user-1"},
			want:    "b1",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeTemplateStore(tc.present...)
			r := NewDefaultTemplateResolver(fs)
			got, err := r.Resolve(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got == nil || got.ID != tc.want {
				t.Fatalf("Resolve returned %+v; want ID=%s", got, tc.want)
			}
		})
	}
}

// TestResolve_NoMatchErrorsOrFallback verifies the Fallback vs error paths.
func TestResolve_NoMatchErrorsOrFallback(t *testing.T) {
	fs := newFakeTemplateStore() // empty store
	r := NewDefaultTemplateResolver(fs)

	// No match, no fallback → ErrNoDefaultTemplate.
	_, err := r.Resolve(context.Background(), DefaultTemplateRequest{UserID: "u"})
	if !errors.Is(err, ErrNoDefaultTemplate) {
		t.Fatalf("got %v; want ErrNoDefaultTemplate", err)
	}

	// No match, fallback → synthetic template with the fallback slug.
	got, err := r.Resolve(context.Background(), DefaultTemplateRequest{
		UserID: "u", Fallback: "astonish-base",
	})
	if err != nil {
		t.Fatalf("Resolve with fallback: %v", err)
	}
	if got == nil || got.Slug != "astonish-base" {
		t.Fatalf("got %+v; want slug=astonish-base", got)
	}
}

// TestResolve_PreferredSlug hits a specific template in each scope.
func TestResolve_PreferredSlug(t *testing.T) {
	custom := tpl("p2", "my-custom", store.SandboxTemplateScopePersonal, "user-1")
	defaulT := tpl("p1", "default", store.SandboxTemplateScopePersonal, "user-1")

	fs := newFakeTemplateStore(custom, defaulT)
	r := NewDefaultTemplateResolver(fs)
	got, err := r.Resolve(context.Background(), DefaultTemplateRequest{
		UserID:        "user-1",
		PreferredSlug: "my-custom",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil || got.ID != "p2" {
		t.Fatalf("got %+v; want p2", got)
	}
}

// TestResolve_ErrUnsupportedContinuesCascade: if a scope's store returns
// ErrUnsupported (e.g. filestore for team/org), the resolver continues rather
// than aborting.
func TestResolve_ErrUnsupportedContinuesCascade(t *testing.T) {
	personal := tpl("p1", "default", store.SandboxTemplateScopePersonal, "user-1")
	fs := newFakeTemplateStore(personal)
	fs.erroringScopes[store.SandboxTemplateScopeTeam] = true
	fs.erroringScopes[store.SandboxTemplateScopeOrg] = true
	fs.erroringScopes[store.SandboxTemplateScopeGlobal] = true

	r := NewDefaultTemplateResolver(fs)
	got, err := r.Resolve(context.Background(), DefaultTemplateRequest{
		UserID: "user-1", TeamID: "team-1", OrgID: "org-1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil || got.ID != "p1" {
		t.Fatalf("got %+v; want p1", got)
	}
}
