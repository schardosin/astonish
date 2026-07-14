package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

func TestValidateBootstrapFile(t *testing.T) {
	t.Parallel()
	if err := validateBootstrapFile(store.BootstrapFile{Path: "/root/app/.astonish/start-services.sh", Content: "#!/bin/bash\n"}); err != nil {
		t.Fatalf("valid file: %v", err)
	}
	if err := validateBootstrapFile(store.BootstrapFile{Path: "relative", Content: "x"}); err == nil {
		t.Fatal("expected error for relative path")
	}
	if err := validateBootstrapFile(store.BootstrapFile{Path: "/root/../etc/passwd", Content: "x"}); err == nil {
		t.Fatal("expected error for .. path")
	}
	if err := validateBootstrapFile(store.BootstrapFile{Path: "/tmp/x", Content: ""}); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestMaterializeBootstrapFilesIncus(t *testing.T) {
	t.Parallel()
	var cmds []string
	err := MaterializeBootstrapFilesIncus(context.Background(), func(command []string, env map[string]string) ([]byte, []byte, int, error) {
		cmds = append(cmds, strings.Join(command, " "))
		return nil, nil, 0, nil
	}, []store.BootstrapFile{{
		Path:    "/root/demo/.astonish/start-services.sh",
		Content: "#!/bin/bash\necho hi\n",
		Mode:    "0755",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 exec, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0], "base64") || !strings.Contains(cmds[0], "/root/demo/.astonish/start-services.sh") {
		t.Fatalf("unexpected command: %s", cmds[0])
	}
}

func TestLookupBootstrapFilesFromRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg := NewInMemoryRegistry(dir)
	reg.SeedForTest(&TemplateMeta{
		Name: "demo",
		BootstrapFiles: []BootstrapFileMeta{
			{Path: "/root/demo/.astonish/start-services.sh", Content: "#!/bin/bash\n", Mode: "0755"},
		},
	})
	files := LookupBootstrapFiles(context.Background(), reg, nil, "demo")
	if len(files) != 1 || files[0].Path != "/root/demo/.astonish/start-services.sh" {
		t.Fatalf("unexpected files: %#v", files)
	}
	if LookupBootstrapFiles(context.Background(), reg, nil, "base") != nil {
		t.Fatal("base should have no bootstrap files")
	}
}
