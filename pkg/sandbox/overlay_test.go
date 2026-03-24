package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveLowerLayers(t *testing.T) {
	// Set platform to native Linux so statOnSandboxHost uses os.Stat
	origPlatform := activePlatform
	activePlatform = PlatformLinuxNative
	t.Cleanup(func() { activePlatform = origPlatform })

	poolPath := t.TempDir()

	// Helper: create directories for a template snapshot rootfs
	mkSnapshotRootfs := func(templateName string) string {
		p := filepath.Join(poolPath, "containers-snapshots", TemplateName(templateName), SnapshotName, "rootfs")
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("base template single layer", func(t *testing.T) {
		expectedPath := mkSnapshotRootfs(BaseTemplate)

		registry := &TemplateRegistry{templates: make(map[string]*TemplateMeta)}

		got, err := ResolveLowerLayers(poolPath, BaseTemplate, registry)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != expectedPath {
			t.Errorf("got %q, want %q", got, expectedPath)
		}
	})

	t.Run("base template missing snapshot", func(t *testing.T) {
		emptyPool := t.TempDir()
		registry := &TemplateRegistry{templates: make(map[string]*TemplateMeta)}

		_, err := ResolveLowerLayers(emptyPool, BaseTemplate, registry)
		if err == nil {
			t.Fatal("expected error for missing snapshot")
		}
	})

	t.Run("custom template not in registry", func(t *testing.T) {
		registry := &TemplateRegistry{templates: make(map[string]*TemplateMeta)}

		_, err := ResolveLowerLayers(poolPath, "my-custom", registry)
		if err == nil {
			t.Fatal("expected error for template not in registry")
		}
		if !strings.Contains(err.Error(), "not found in registry") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil registry for custom template", func(t *testing.T) {
		_, err := ResolveLowerLayers(poolPath, "my-custom", nil)
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
		if !strings.Contains(err.Error(), "registry is nil") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("custom template with materialized snapshot", func(t *testing.T) {
		expectedPath := mkSnapshotRootfs("my-materialized")

		registry := &TemplateRegistry{templates: make(map[string]*TemplateMeta)}
		registry.templates["my-materialized"] = &TemplateMeta{
			Name:    "my-materialized",
			BasedOn: BaseTemplate,
		}

		got, err := ResolveLowerLayers(poolPath, "my-materialized", registry)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != expectedPath {
			t.Errorf("got %q, want %q", got, expectedPath)
		}
	})
}

func TestOverlaySessionDir(t *testing.T) {
	got := OverlaySessionDir("abc12345")
	if !strings.Contains(got, "abc12345") {
		t.Errorf("expected session ID in path, got %q", got)
	}
	if !strings.HasPrefix(got, overlayBaseDir) {
		t.Errorf("expected overlayBaseDir prefix, got %q", got)
	}
}

func TestOverlayUpperDir(t *testing.T) {
	got := OverlayUpperDir("astn-sess-abc12345")
	if !strings.HasSuffix(got, "/upper") {
		t.Errorf("expected /upper suffix, got %q", got)
	}
	if !strings.Contains(got, "astn-sess-abc12345") {
		t.Errorf("expected container name in path, got %q", got)
	}
}

func TestSnapshotRootfsPath(t *testing.T) {
	got := SnapshotRootfsPath("/pool", "base")
	want := "/pool/containers-snapshots/astn-tpl-base/snap/rootfs"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTemplateRegistrySaveLoad(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "templates.json")

	// Create and populate
	r1 := &TemplateRegistry{
		templates: make(map[string]*TemplateMeta),
		filePath:  filePath,
	}
	r1.templates["base"] = &TemplateMeta{
		Name:      "base",
		CreatedAt: time.Now(),
	}
	r1.templates["custom"] = &TemplateMeta{
		Name:    "custom",
		BasedOn: "base",
	}
	if err := r1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load into fresh registry
	r2 := &TemplateRegistry{
		templates: make(map[string]*TemplateMeta),
		filePath:  filePath,
	}
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if r2.Get("base") == nil {
		t.Error("base template not loaded")
	}
	if m := r2.Get("custom"); m == nil {
		t.Error("custom template not loaded")
	} else if m.BasedOn != "base" {
		t.Errorf("BasedOn = %q, want base", m.BasedOn)
	}
}
