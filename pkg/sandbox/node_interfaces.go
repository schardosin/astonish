// Package sandbox — interface abstractions over the concrete
// NodeClientPool / LazyNodeClient types.
//
// These interfaces exist so tool-wrapping code (NodeTool, SetupFlowSandbox)
// can accept either the existing Incus-bound *NodeClientPool or a future
// backend-agnostic pool that talks to sandbox.Backend. They isolate the
// *minimum* surface NodeTool needs — not the full 20+ methods on the
// concrete types — to keep the abstraction load-bearing rather than
// ceremonial.
//
// Design intent (Phase E §11):
//
//   - *NodeClientPool and *LazyNodeClient already satisfy these
//     interfaces by name; no behavioural change on the Incus path.
//   - A second implementation backed by sandbox.Backend.ExecInteractive
//     will live in backend_pool.go (Phase E slice 2). That one serves
//     the k8s backend without needing an Incus client or the full
//     LazyNodeClient surface.
//   - NodeTool consumes these interfaces via type assertion so existing
//     callers passing *NodeClientPool keep working verbatim.
//
// Why not embed these in the concrete types' declarations? The concrete
// types pre-date the interfaces. Adding method signatures in a separate
// file makes it easy to see at a glance what NodeTool actually needs
// without hunting through 1000+ lines of node.go.

package sandbox

import (
	"encoding/json"
)

// ToolNodeClient is the minimal surface a NodeTool needs from whatever object
// services its tool calls. Both *LazyNodeClient (Incus) and future
// backend-agnostic clients satisfy this.
//
// The two methods mirror NodeTool's actual usage (see node_tool.go):
//
//   - BindSession: warmed-up side effect called from ProcessRequest so
//     container / pod creation overlaps the LLM call.
//   - Call:         the RPC that executes a named tool inside the sandbox
//     and returns its JSON result.
//
// Implementations MUST be safe for concurrent use per sessionID and
// MUST NOT assume Call is preceded by BindSession (BindSession is a
// latency optimisation, not a correctness barrier).
//
// Named ToolNodeClient (not NodeClient) because the unqualified name is
// already taken by the Incus-specific NodeClient struct in node.go. The
// "Tool" prefix also signals the consumer: this is the view a NodeTool
// (pkg/sandbox/node_tool.go) cares about, not the transport.
type ToolNodeClient interface {
	// BindSession triggers background sandbox provisioning for the
	// given session ID. Idempotent: second + subsequent calls with any
	// session ID are no-ops.
	BindSession(sessionID string)

	// Call executes toolName with args inside the sandbox bound to
	// sessionID and returns the raw JSON result. The caller is
	// responsible for JSON-decoding into a concrete result shape.
	Call(sessionID, toolName string, args map[string]interface{}) (json.RawMessage, error)
}

// ToolNodePool is the minimal surface a NodeTool needs from a pool. Both
// *NodeClientPool (Incus) and the forthcoming backendPool satisfy this.
//
// The pool's job is to vend one ToolNodeClient per sessionID. The optional
// template override lets chat sessions pick a non-default template
// (store.SandboxTemplateFromContext) on a per-request basis without
// reconfiguring the pool globally.
type ToolNodePool interface {
	// GetOrCreate returns the ToolNodeClient for the given session. If none
	// exists, one is constructed lazily. Returns nil when the pool is
	// closed or sessionID is empty.
	GetOrCreate(sessionID string) ToolNodeClient

	// GetOrCreateWithTemplate is like GetOrCreate but pins a specific
	// template for a fresh session. If the session already has a client,
	// the override is ignored (templates are immutable post-create).
	GetOrCreateWithTemplate(sessionID, template string) ToolNodeClient

	// GetOrCreateWithChain is like GetOrCreateWithTemplate but also
	// supplies a pre-resolved layer chain (oldest-first). On backends
	// that use content-addressed layers (K8s), this chain is passed
	// directly to SessionSpec.LayerChain, bypassing name-based lookup.
	// On Incus (where templates are named containers), chain is ignored.
	GetOrCreateWithChain(sessionID, template string, chain []string) ToolNodeClient

	// GetOrCreateWithImage is like GetOrCreateWithChain but also supplies a
	// per-template container image. On OpenShell, this image is passed to
	// SessionSpec.Image, overriding the global config default. On backends
	// that don't use per-template images (K8s, Incus), image is ignored.
	GetOrCreateWithImage(sessionID, template string, chain []string, image string) ToolNodeClient

	// Cleanup destroys every client the pool has vended. After Cleanup
	// the pool is unusable; further GetOrCreate calls return nil.
	Cleanup()
}

// Compile-time assertions that the concrete Incus-bound types satisfy
// the interfaces. A change to either side that breaks the contract
// fails the build rather than silently slipping into a runtime panic.
//
// NOTE: LazyNodeClient.Call's signature already matches ToolNodeClient.Call
// (it takes sessionID as the first arg even though the client is
// already session-bound — see node.go:452 for the rationale around
// activity tracking and session re-binding on retry).
var (
	_ ToolNodeClient = (*LazyNodeClient)(nil)
)

// nodePoolAdapter wraps *NodeClientPool so it satisfies the ToolNodePool
// interface. We can't add an interface-returning GetOrCreate method
// directly on *NodeClientPool without breaking existing callers that
// depend on the concrete *LazyNodeClient return type (chat_factory,
// fleet wiring). The adapter gives NodeTool and flow code a clean
// interface without forcing the concrete-returning methods to change.
type nodePoolAdapter struct {
	p *NodeClientPool
}

// AsNodePool returns an interface-compatible view of the NodeClientPool.
// The returned value forwards every method to the wrapped pool, so any
// state changes (SetEnv, SetOrgContext, etc.) made directly on the
// concrete pool remain observable through the adapter.
//
// Nil-safety: passing a nil pool returns a nil ToolNodePool, so callers
// checking `pool == nil` keep working when the wrap is transparent.
func AsNodePool(p *NodeClientPool) ToolNodePool {
	if p == nil {
		return nil
	}
	return &nodePoolAdapter{p: p}
}

func (a *nodePoolAdapter) GetOrCreate(sessionID string) ToolNodeClient {
	c := a.p.GetOrCreate(sessionID)
	if c == nil {
		return nil // keep nil sentinel for callers that type-check
	}
	return c
}

func (a *nodePoolAdapter) GetOrCreateWithTemplate(sessionID, template string) ToolNodeClient {
	c := a.p.GetOrCreateWithTemplate(sessionID, template)
	if c == nil {
		return nil
	}
	return c
}

func (a *nodePoolAdapter) GetOrCreateWithChain(sessionID, template string, _ []string) ToolNodeClient {
	// Incus pool ignores the chain; templates are named containers on disk.
	return a.GetOrCreateWithTemplate(sessionID, template)
}

func (a *nodePoolAdapter) GetOrCreateWithImage(sessionID, template string, _ []string, _ string) ToolNodeClient {
	// Incus pool ignores chain and image; templates are named containers.
	return a.GetOrCreateWithTemplate(sessionID, template)
}

func (a *nodePoolAdapter) Cleanup() { a.p.Cleanup() }
