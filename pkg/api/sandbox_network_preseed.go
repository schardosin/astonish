package api

import (
	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
)

func init() {
	// Wire NodeTool -> netpolicy PreSeed without an import cycle.
	// This is API-wide: chat, flows, app sandboxes, and fleet all share NodeTool.
	sandbox.NetworkPolicyPreSeeder = netpolicy.EnsurePreSeedFromContext
}
