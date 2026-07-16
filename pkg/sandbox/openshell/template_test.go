package openshell

import (
	"context"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
)

// ---------------------------------------------------------------------------
// Template tests — all template operations are unsupported in the OpenShell
// backend because sandbox image lifecycle is managed by OpenShell/registries.
// ---------------------------------------------------------------------------

func TestBuildTemplate_NotSupported(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.BuildTemplate(context.Background(), sandbox.TemplateBuildSpec{
		TemplateID: "python-dev",
		Steps:      []string{"apt-get install -y python3"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should say 'not supported', got: %v", err)
	}
}

func TestSaveSessionAsTemplate_NotSupported(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.SaveSessionAsTemplate(context.Background(), "sess-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should say 'not supported', got: %v", err)
	}
}

func TestRefreshTemplate_NotSupported(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	_, err := b.RefreshTemplate(context.Background(), "any")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should say 'not supported', got: %v", err)
	}
}

func TestDeleteTemplate_IsNoop(t *testing.T) {
	gw := &mockGateway{
		createFn: func(ctx context.Context, req CreateSandboxRequest) (*CreateSandboxResponse, error) {
			t.Error("should not create sandbox for DeleteTemplate")
			return nil, nil
		},
	}
	b := newTestBackendWithGateway(t, gw)

	if err := b.DeleteTemplate(context.Background(), "old-template", false); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
}

func TestDeleteTemplate_BaseTemplateIsNoop(t *testing.T) {
	gw := &mockGateway{}
	b := newTestBackendWithGateway(t, gw)

	if err := b.DeleteTemplate(context.Background(), "@base", false); err != nil {
		t.Fatalf("DeleteTemplate(@base): %v", err)
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"abcdefgh", 8, "abcdefgh"},
		{"abcdefghijklmn", 8, "abcdefgh"},
		{"short", 10, "short"},
	}
	for _, tt := range tests {
		got := truncateID(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncateID(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}
