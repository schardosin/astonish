// Package openshell — denial_detector.go implements network denial detection
// for the dynamic network policy approval system.
//
// After a tool execution fails, the DenialDetector checks whether the OpenShell
// supervisor has pending draft policy proposals (indicating a blocked network
// connection). It uses a two-phase approach:
//
//  1. Pull: Wait briefly, then query GetDraftPolicy for pending chunks.
//  2. Stream fallback: If the pull finds nothing, subscribe to WatchSandbox
//     for a short window to catch delayed denial events.
//
// This avoids parsing tool output and works regardless of which binary
// (curl, python, go, etc.) triggered the denial.

package openshell

import (
	"context"
	"io"
	"strings"
	"time"
)

// DenialDetector checks for network denials on a sandbox after tool failures.
type DenialDetector struct {
	gateway GatewayClient
}

// NewDenialDetector creates a DenialDetector that uses the given gateway client.
func NewDenialDetector(gateway GatewayClient) *DenialDetector {
	return &DenialDetector{gateway: gateway}
}

// DenialInfo contains structured information about a network denial.
type DenialInfo struct {
	// ChunkID is the draft policy chunk ID (used to approve/reject).
	ChunkID string `json:"chunk_id"`
	// Host is the denied endpoint hostname.
	Host string `json:"host"`
	// Port is the denied endpoint port.
	Port uint32 `json:"port"`
	// Binary is the binary that triggered the denial (e.g., "/usr/bin/curl").
	Binary string `json:"binary"`
	// Rationale is the supervisor's explanation.
	Rationale string `json:"rationale,omitempty"`
	// SecurityNotes contains any security concerns.
	SecurityNotes string `json:"security_notes,omitempty"`
	// BroaderPattern is a suggested broader host pattern (e.g., "**.cloud.sap").
	BroaderPattern string `json:"broader_pattern,omitempty"`
}

// CheckForDenials checks if the supervisor has pending draft policy proposals
// for the given sandbox. It first waits briefly for the supervisor to aggregate
// the denial, then queries for pending chunks. If none are found, it falls back
// to a short WatchSandbox stream subscription.
//
// Returns nil if no denials are detected within the timeout.
// The caller should invoke this asynchronously after a tool execution failure.
func (d *DenialDetector) CheckForDenials(ctx context.Context, sandboxName string) ([]DenialInfo, error) {
	// Phase 1: Wait for supervisor to aggregate the denial.
	select {
	case <-time.After(800 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Phase 2: Pull pending draft policy chunks.
	denials, err := d.queryPendingChunks(ctx, sandboxName)
	if err != nil {
		return nil, err
	}
	if len(denials) > 0 {
		return denials, nil
	}

	// Phase 3: Stream fallback — wait up to 5s for a DraftPolicyUpdate event.
	return d.waitForDraftUpdate(ctx, sandboxName, 5*time.Second)
}

// queryPendingChunks calls GetDraftPolicy to retrieve pending proposals.
func (d *DenialDetector) queryPendingChunks(ctx context.Context, sandboxName string) ([]DenialInfo, error) {
	resp, err := d.gateway.GetDraftPolicy(ctx, sandboxName, "pending")
	if err != nil {
		// Non-fatal: the sandbox might not support draft policies, or it might
		// have been deleted between the tool failure and this check.
		return nil, nil
	}

	if len(resp.Chunks) == 0 {
		return nil, nil
	}

	denials := make([]DenialInfo, 0, len(resp.Chunks))
	for _, ch := range resp.Chunks {
		info := DenialInfo{
			ChunkID:        ch.ID,
			Host:           ch.Host,
			Port:           ch.Port,
			Binary:         ch.Binary,
			Rationale:      ch.Rationale,
			SecurityNotes:  ch.SecurityNotes,
			BroaderPattern: suggestBroaderPattern(ch.Host),
		}
		denials = append(denials, info)
	}
	return denials, nil
}

// waitForDraftUpdate opens a short-lived WatchSandbox stream to catch delayed
// denial notifications. If a DraftPolicyUpdate arrives within the timeout,
// it re-queries GetDraftPolicy to retrieve the actual chunks.
func (d *DenialDetector) waitForDraftUpdate(ctx context.Context, sandboxName string, timeout time.Duration) ([]DenialInfo, error) {
	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stream, err := d.gateway.WatchSandbox(watchCtx, sandboxName, WatchOpts{
		FollowEvents: true,
		EventTail:    0,
	})
	if err != nil {
		// Non-fatal: WatchSandbox might not be supported or the sandbox is gone.
		return nil, nil
	}
	defer stream.Close()

	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF || watchCtx.Err() != nil {
				return nil, nil
			}
			return nil, nil
		}

		if event.Type == SandboxEventDraftUpdate && event.DraftPolicyUpdate != nil {
			if event.DraftPolicyUpdate.NewChunks > 0 {
				// New chunks detected — re-query for the actual proposals.
				return d.queryPendingChunks(ctx, sandboxName)
			}
		}
	}
}

// SuggestBroaderPattern generates a broader host glob pattern from a specific
// hostname. For example:
//
//	"identity-3.qa-de-1.cloud.sap" → "**.cloud.sap"
//	"api.internal.mycompany.com"   → "**.mycompany.com"
//	"example.com"                  → "" (already minimal)
func SuggestBroaderPattern(host string) string {
	return suggestBroaderPattern(host)
}

// suggestBroaderPattern generates a broader host glob pattern from a specific
// hostname. For example:
//
//	"identity-3.qa-de-1.cloud.sap" → "**.cloud.sap"
//	"api.internal.mycompany.com"   → "**.mycompany.com"
//	"example.com"                  → "" (already minimal)
func suggestBroaderPattern(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		// Already a domain like "example.com" — no broader pattern.
		return ""
	}

	// Find a reasonable "registrable domain" approximation.
	// Strategy: keep the last 2 parts as the base domain, and use ** prefix.
	// For hosts like "identity-3.qa-de-1.cloud.sap":
	//   parts = [identity-3, qa-de-1, cloud, sap]
	//   base = "cloud.sap" → pattern = "**.cloud.sap"
	//
	// For hosts like "api.internal.mycompany.com":
	//   parts = [api, internal, mycompany, com]
	//   base = "mycompany.com" → pattern = "**.mycompany.com"
	//
	// This is a heuristic — the user can always edit the pattern.
	if len(parts) >= 3 {
		base := strings.Join(parts[len(parts)-2:], ".")
		// Only suggest if the host has more than one subdomain level above base.
		if len(parts) > 3 {
			return "**." + base
		}
		// Exactly one subdomain: suggest *. pattern (single level).
		return "*." + base
	}

	return ""
}
