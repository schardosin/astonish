package sandbox

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SAP/astonish/pkg/store"
)

// spyPool records which ToolNodePool method was called and with what args.
type spyPool struct {
	method   string
	session  string
	template string
	chain    []string
	image    string
}

func (s *spyPool) GetOrCreate(sessionID string) ToolNodeClient {
	s.method = "GetOrCreate"
	s.session = sessionID
	return &stubClient{}
}

func (s *spyPool) GetOrCreateWithTemplate(sessionID, template string) ToolNodeClient {
	s.method = "GetOrCreateWithTemplate"
	s.session = sessionID
	s.template = template
	return &stubClient{}
}

func (s *spyPool) GetOrCreateWithChain(sessionID, template string, chain []string) ToolNodeClient {
	s.method = "GetOrCreateWithChain"
	s.session = sessionID
	s.template = template
	s.chain = chain
	return &stubClient{}
}

func (s *spyPool) GetOrCreateWithImage(sessionID, template string, chain []string, image string) ToolNodeClient {
	s.method = "GetOrCreateWithImage"
	s.session = sessionID
	s.template = template
	s.chain = chain
	s.image = image
	return &stubClient{}
}

func (s *spyPool) Cleanup() {}

func (s *spyPool) GetBackend() Backend { return nil }

func (s *spyPool) Alias(_, _ string) {}

func (s *spyPool) Remove(_ string) {}

// stubClient satisfies ToolNodeClient for test purposes.
type stubClient struct{}

func (c *stubClient) BindSession(string)           {}
func (c *stubClient) EnsureReady(string) error     { return nil }
func (c *stubClient) Call(string, string, map[string]interface{}) (json.RawMessage, error) {
	return nil, nil
}
func (c *stubClient) Close() error { return nil }
func (c *stubClient) IsClosed() bool { return false }

func TestGetClientFromContext_ChainWithoutTemplate(t *testing.T) {
	// When a layer chain is set but no template name exists (the @base-only
	// configured case), getClientFromContext must still pass the chain.
	spy := &spyPool{}
	nt := &NodeTool{pool: spy}

	ctx := context.Background()
	ctx = store.WithSandboxLayerChain(ctx, []string{"@base", "sha256:abc123"})
	// No template set — SandboxTemplateFromContext returns ""

	client := nt.getClientFromContext(ctx, "session-1")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if spy.method != "GetOrCreateWithImage" {
		t.Fatalf("expected GetOrCreateWithImage, got %s", spy.method)
	}
	if spy.template != "" {
		t.Fatalf("expected empty template, got %q", spy.template)
	}
	if len(spy.chain) != 2 || spy.chain[0] != "@base" || spy.chain[1] != "sha256:abc123" {
		t.Fatalf("unexpected chain: %v", spy.chain)
	}
	if spy.image != "" {
		t.Fatalf("expected empty image, got %q", spy.image)
	}
}

func TestGetClientFromContext_ChainWithTemplate(t *testing.T) {
	// When both template and chain are set (team-template case),
	// getClientFromContext passes both.
	spy := &spyPool{}
	nt := &NodeTool{pool: spy}

	ctx := context.Background()
	ctx = store.WithSandboxTemplate(ctx, "my-team-tpl")
	ctx = store.WithSandboxLayerChain(ctx, []string{"@base", "sha256:team123"})

	client := nt.getClientFromContext(ctx, "session-2")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if spy.method != "GetOrCreateWithImage" {
		t.Fatalf("expected GetOrCreateWithImage, got %s", spy.method)
	}
	if spy.template != "my-team-tpl" {
		t.Fatalf("expected template 'my-team-tpl', got %q", spy.template)
	}
	if len(spy.chain) != 2 {
		t.Fatalf("unexpected chain: %v", spy.chain)
	}
}

func TestGetClientFromContext_TemplateNoChain(t *testing.T) {
	// When template is set but no chain (e.g., Incus backend or unresolved),
	// getClientFromContext uses GetOrCreateWithTemplate.
	spy := &spyPool{}
	nt := &NodeTool{pool: spy}

	ctx := context.Background()
	ctx = store.WithSandboxTemplate(ctx, "my-tpl")

	client := nt.getClientFromContext(ctx, "session-3")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if spy.method != "GetOrCreateWithTemplate" {
		t.Fatalf("expected GetOrCreateWithTemplate, got %s", spy.method)
	}
	if spy.template != "my-tpl" {
		t.Fatalf("expected template 'my-tpl', got %q", spy.template)
	}
}

func TestGetClientFromContext_NoTemplateNoChain(t *testing.T) {
	// When neither template nor chain (personal mode, fresh install),
	// getClientFromContext uses plain GetOrCreate.
	spy := &spyPool{}
	nt := &NodeTool{pool: spy}

	ctx := context.Background()

	client := nt.getClientFromContext(ctx, "session-4")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if spy.method != "GetOrCreate" {
		t.Fatalf("expected GetOrCreate, got %s", spy.method)
	}
}

func TestGetClientFromContext_ImageOnly(t *testing.T) {
	// When image is set but no chain (OpenShell with custom image, no layer chain),
	// getClientFromContext uses GetOrCreateWithImage.
	spy := &spyPool{}
	nt := &NodeTool{pool: spy}

	ctx := context.Background()
	ctx = store.WithSandboxImage(ctx, "ghcr.io/sap/custom-sandbox:v2")

	client := nt.getClientFromContext(ctx, "session-img")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if spy.method != "GetOrCreateWithImage" {
		t.Fatalf("expected GetOrCreateWithImage, got %s", spy.method)
	}
	if spy.image != "ghcr.io/sap/custom-sandbox:v2" {
		t.Fatalf("expected custom image, got %q", spy.image)
	}
	if len(spy.chain) != 0 {
		t.Fatalf("expected no chain, got %v", spy.chain)
	}
}

func TestGetClientFromContext_LazyClientTakesPriority(t *testing.T) {
	// When a lazyClient is set (fleet sessions), it takes priority
	// regardless of context values.
	spy := &spyPool{}
	lnc := &LazyNodeClient{}
	nt := &NodeTool{pool: spy, lazyClient: lnc}

	ctx := context.Background()
	ctx = store.WithSandboxTemplate(ctx, "ignored")
	ctx = store.WithSandboxLayerChain(ctx, []string{"@base", "sha256:ignored"})

	client := nt.getClientFromContext(ctx, "session-5")
	if client != lnc {
		t.Fatal("expected lazyClient to be returned")
	}
	if spy.method != "" {
		t.Fatalf("pool should not have been called, but got %s", spy.method)
	}
}
