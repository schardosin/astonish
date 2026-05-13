package filestore

import (
	"context"
	"errors"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

// Personal-mode invariant (§6.4): LayerStore returns ErrUnsupported for every
// method. These tests lock that behavior in so no one accidentally promotes
// the stub to a real implementation.
func TestUnsupportedLayerStore(t *testing.T) {
	ls := NewLayerStore()
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"PutLayer", func() error {
			return ls.PutLayer(ctx, &store.SandboxLayer{LayerID: "x", CephFSPath: "/x"})
		}},
		{"GetLayer", func() error {
			_, err := ls.GetLayer(ctx, "x")
			return err
		}},
		{"IncrementRefCount", func() error {
			return ls.IncrementRefCount(ctx, "x")
		}},
		{"DecrementRefCount", func() error {
			return ls.DecrementRefCount(ctx, "x")
		}},
		{"ListUnreferenced", func() error {
			_, err := ls.ListUnreferenced(ctx, 0)
			return err
		}},
		{"DeleteLayer", func() error {
			return ls.DeleteLayer(ctx, "x")
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if !errors.Is(err, store.ErrUnsupported) {
				t.Fatalf("%s: got %v; want ErrUnsupported", tc.name, err)
			}
		})
	}
}

// Same for ChatEventJournal.
func TestUnsupportedChatEventJournal(t *testing.T) {
	j := NewChatEventJournal()
	ctx := context.Background()

	if err := j.Append(ctx, []*store.ChatEvent{{ChatSessionID: "s", EventType: "t"}}); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("Append: got %v; want ErrUnsupported", err)
	}
	if _, err := j.ReadSince(ctx, "s", 0, 10); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("ReadSince: got %v; want ErrUnsupported", err)
	}
	if _, err := j.LastSeq(ctx, "s"); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("LastSeq: got %v; want ErrUnsupported", err)
	}
}

// Personal-mode SandboxTemplateStore with nil registry is a no-op: every
// method returns ErrUnsupported. This is useful when the personal-mode CLI
// is compiled without access to a TemplateRegistry (rare but supported).
func TestSandboxTemplateStore_NilRegistry(t *testing.T) {
	ts := NewSandboxTemplateStore(nil)
	ctx := context.Background()

	if err := ts.Create(ctx, &store.SandboxTemplate{Slug: "x"}); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("Create: got %v; want ErrUnsupported", err)
	}
	if _, err := ts.GetByID(ctx, "x"); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("GetByID: got %v; want ErrUnsupported", err)
	}
	if _, err := ts.GetBySlug(ctx, store.SandboxTemplateScopePersonal, "", "x"); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("GetBySlug: got %v; want ErrUnsupported", err)
	}
	if _, err := ts.List(ctx, store.SandboxTemplateFilter{}); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("List: got %v; want ErrUnsupported", err)
	}
	if err := ts.Update(ctx, &store.SandboxTemplate{Slug: "x"}); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("Update: got %v; want ErrUnsupported", err)
	}
	if err := ts.Delete(ctx, "x"); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("Delete: got %v; want ErrUnsupported", err)
	}
	// Resolve is always ErrUnsupported on filestore (DAG is platform-only).
	if _, err := ts.Resolve(ctx, "x"); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("Resolve: got %v; want ErrUnsupported", err)
	}
}
