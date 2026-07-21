// Package api — network_grant_handlers.go implements HTTP handlers for the
// dynamic network policy approval system.
//
// These endpoints allow users to approve or deny network access requests
// that were blocked by the OpenShell supervisor's L7 proxy. Approvals
// result in live policy updates to running sandboxes.
//
// Endpoints:
//
//	POST /api/studio/sessions/{id}/network-grants/approve
//	POST /api/studio/sessions/{id}/network-grants/approve-broader
//	POST /api/studio/sessions/{id}/network-grants/deny

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/SAP/astonish/pkg/sandbox/netpolicy"
	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

// networkGrantApproveRequest is the body for approving a specific draft chunk.
type networkGrantApproveRequest struct {
	// ChunkID is the draft policy chunk to approve.
	ChunkID string `json:"chunk_id"`
	// SandboxName is the sandbox to update.
	SandboxName string `json:"sandbox_name"`
}

// networkGrantApproveBroaderRequest is the body for approving a broader pattern.
type networkGrantApproveBroaderRequest struct {
	// Host is the broader host pattern (e.g., "**.cloud.sap").
	Host string `json:"host"`
	// Port is the endpoint port.
	Port uint32 `json:"port"`
	// SandboxName is the sandbox to update.
	SandboxName string `json:"sandbox_name"`
}

// networkGrantDenyRequest is the body for rejecting a draft chunk.
type networkGrantDenyRequest struct {
	// ChunkID is the draft policy chunk to reject.
	ChunkID string `json:"chunk_id"`
	// SandboxName is the sandbox name.
	SandboxName string `json:"sandbox_name"`
	// Reason is an optional denial reason.
	Reason string `json:"reason,omitempty"`
}

// NetworkGrantApproveHandler approves a specific draft policy chunk,
// causing the OpenShell supervisor to merge the endpoint into the
// sandbox's active network policy.
func NetworkGrantApproveHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	var req networkGrantApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ChunkID == "" {
		http.Error(w, "chunk_id is required", http.StatusBadRequest)
		return
	}
	// Resolve sandbox name from session if not provided.
	if req.SandboxName == "" {
		name, err := sandboxNameForSession(r, sessionID)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot resolve sandbox: %v", err), http.StatusBadRequest)
			return
		}
		req.SandboxName = name
	}

	gateway, cleanup, err := gatewayClientForRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get gateway client: %v", err), http.StatusInternalServerError)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	resp, err := gateway.ApproveDraftChunk(r.Context(), req.SandboxName, req.ChunkID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to approve chunk: %v", err), http.StatusInternalServerError)
		return
	}

	// Wait for the sandbox proxy to actually load the new policy before
	// returning to the frontend. This prevents the race where the agent
	// retries the blocked request before the proxy has applied the update.
	waitForPolicyLoad(r.Context(), gateway, req.SandboxName, resp.PolicyVersion)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"approved":       true,
		"policy_version": resp.PolicyVersion,
		"policy_hash":    resp.PolicyHash,
	})
}

// NetworkGrantApproveBroaderHandler approves a broader host pattern by
// directly adding a network rule via UpdateConfig (not tied to a specific chunk).
func NetworkGrantApproveBroaderHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	var req networkGrantApproveBroaderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Host == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	// Resolve sandbox name from session if not provided.
	if req.SandboxName == "" {
		name, err := sandboxNameForSession(r, sessionID)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot resolve sandbox: %v", err), http.StatusBadRequest)
			return
		}
		req.SandboxName = name
	}

	port := req.Port
	if port == 0 {
		port = 443
	}

	gateway, cleanup, err := gatewayClientForRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get gateway client: %v", err), http.StatusInternalServerError)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	ops := []openshell.PolicyMergeOp{
		{
			Type:     openshell.PolicyMergeAddEndpoint,
			RuleName: "astonish-egress",
			Endpoint: &openshell.EndpointSpec{Host: req.Host, Port: port},
		},
	}

	resp, err := gateway.UpdateConfig(r.Context(), req.SandboxName, ops)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to update config: %v", err), http.StatusInternalServerError)
		return
	}

	// Wait for the sandbox proxy to actually load the new policy.
	netpolicy.WaitForPolicyLoad(r.Context(), gateway, req.SandboxName, resp.PolicyVersion)

	// Persist to durable team/org network policy so scheduler sandboxes
	// inherit this grant via pre-seed (live UpdateConfig alone does not).
	persistNetworkAllowRule(r, req.Host, port)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"approved":       true,
		"host":           req.Host,
		"port":           port,
		"policy_version": resp.PolicyVersion,
	})
}

// NetworkGrantDenyHandler rejects a draft policy chunk.
func NetworkGrantDenyHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]

	var req networkGrantDenyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// For deny without chunk_id (stdout-extracted denials), just acknowledge.
	if req.ChunkID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"denied": true})
		return
	}
	// Resolve sandbox name from session if not provided.
	if req.SandboxName == "" {
		name, err := sandboxNameForSession(r, sessionID)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot resolve sandbox: %v", err), http.StatusBadRequest)
			return
		}
		req.SandboxName = name
	}

	gateway, cleanup, err := gatewayClientForRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get gateway client: %v", err), http.StatusInternalServerError)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	if err := gateway.RejectDraftChunk(r.Context(), req.SandboxName, req.ChunkID, req.Reason); err != nil {
		http.Error(w, fmt.Sprintf("failed to reject chunk: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"denied": true,
	})
}

// gatewayClientForRequest creates an OpenShell gateway client from the request context.
// This is a convenience wrapper around the sandbox backend setup.
func gatewayClientForRequest(r *http.Request) (openshell.GatewayClient, func(), error) {
	appCfg := effectiveAppConfig(r)
	if appCfg == nil {
		return nil, nil, fmt.Errorf("no app config in request context")
	}

	cfg := openshell.GRPCClientConfig{
		Addr: appCfg.Sandbox.OpenShell.GatewayAddr,
		TLS:  appCfg.Sandbox.OpenShell.OpenShellGatewayTLS(),
	}

	client, err := openshell.NewGRPCGatewayClient(cfg)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() { client.Close() }
	return client, cleanup, nil
}

// waitForPolicyLoad polls until the sandbox proxy has loaded the policy version.
func waitForPolicyLoad(ctx context.Context, gateway openshell.GatewayClient, sandboxName string, targetVersion uint32) {
	netpolicy.WaitForPolicyLoad(ctx, gateway, sandboxName, targetVersion)
}

// persistNetworkAllowRule saves an allow rule to the team (preferred) or org
// NetworkPolicyStore so future sandboxes (including scheduler) pre-seed it.
func persistNetworkAllowRule(r *http.Request, host string, port uint32) {
	svc := store.FromRequest(r)
	if svc == nil {
		return
	}
	target := svc.TeamNetworkPolicies
	scope := "team"
	if target == nil {
		target = svc.NetworkPolicies
		scope = "org"
	}
	if target == nil {
		slog.Debug("persist network allow: no team/org policy store available", "host", host)
		return
	}
	rule := &store.NetworkPolicyRule{
		Host:   host,
		Port:   port,
		Action: store.NetworkPolicyAllow,
	}
	if user := GetPlatformUser(r); user != nil {
		rule.CreatedBy = user.ID
	}
	if err := target.Save(r.Context(), rule); err != nil {
		slog.Warn("persist network allow rule failed",
			"host", host, "port", port, "scope", scope, "error", err)
		return
	}
	slog.Info("persisted network allow rule",
		"host", host, "port", port, "scope", scope)
}
