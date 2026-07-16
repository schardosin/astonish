// Package api — network_denial_check.go provides an HTTP endpoint for the
// frontend to poll for network denials on a running sandbox session.
//
// GET /api/studio/sessions/{id}/network-denials
//
// The frontend polls this endpoint after receiving a tool_result that indicates
// an error. If pending draft policy proposals exist, they are returned for
// the user to approve or deny.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	nurl "net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/sandbox/openshell"
	"github.com/SAP/astonish/pkg/store"
)

// NetworkDenialCheckHandler returns pending network denial proposals for a sandbox
// associated with the given session.
//
// The frontend calls this after observing a tool_result with an error. The response
// contains any pending draft policy chunks that the user can approve or deny.
func NetworkDenialCheckHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["id"]
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}

	// Look up the sandbox name for this session.
	sandboxName, err := sandboxNameForSession(r, sessionID)
	if err != nil {
		// No sandbox for this session — not an error, just means no denials possible.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"denials":      []any{},
			"sandbox_name": "",
		})
		return
	}

	gateway, cleanup, err := gatewayClientForRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("gateway client error: %v", err), http.StatusInternalServerError)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Query the supervisor for pending draft policy chunks.
	resp, err := gateway.GetDraftPolicy(r.Context(), sandboxName, "pending")
	if err != nil {
		// Non-fatal — supervisor might not support this yet, or sandbox is gone.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"denials":      []any{},
			"sandbox_name": sandboxName,
			"error":        err.Error(),
		})
		return
	}

	denials := make([]openshell.DenialInfo, 0, len(resp.Chunks))
	for _, ch := range resp.Chunks {
		denials = append(denials, openshell.DenialInfo{
			ChunkID:        ch.ID,
			Host:           ch.Host,
			Port:           ch.Port,
			Binary:         ch.Binary,
			Rationale:      ch.Rationale,
			SecurityNotes:  ch.SecurityNotes,
			BroaderPattern: openshell.SuggestBroaderPattern(ch.Host),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"denials":      denials,
		"sandbox_name": sandboxName,
	})
}

// sandboxNameForSession resolves the OpenShell sandbox name (container name) for
// a given chat session. Returns empty string and error if not found.
func sandboxNameForSession(r *http.Request, sessionID string) (string, error) {
	// In platform mode, try PG-backed registry first for cross-replica consistency.
	registry := buildPGSessionRegistry(r.Context())
	if registry != nil {
		if name := registry.GetContainerName(sessionID); name != "" {
			return name, nil
		}
		slog.Debug("sandbox name not found in PG registry, using deterministic fallback",
			"session_id", sessionID)
	} else {
		// Personal mode — try local file-backed registry.
		localReg, err := sandbox.NewSessionRegistry()
		if err == nil {
			if name := localReg.GetContainerName(sessionID); name != "" {
				return name, nil
			}
		}
		slog.Debug("sandbox name not found in local registry, using deterministic fallback",
			"session_id", sessionID, "error", err)
	}

	// Deterministic fallback: compute the sandbox name using the same formula
	// that the OpenShell backend uses when creating sandboxes. This is safe
	// because the mapping is always "astn-sess-" + DNS-label(sessionID)[:27].
	return openshell.SandboxName(sessionID), nil
}

// looksLikeNetworkDenial checks a shell_command response map for indicators
// that the failure was caused by a blocked network connection (L7 proxy denial).
// This is intentionally broad — false positives are acceptable because the
// frontend will verify with an actual GetDraftPolicy call.
//
// Two tiers of detection:
//   - Strong indicators (proxy CONNECT 403, tunnel failures) trigger regardless
//     of exit code, because multi-line scripts often exit 0 even when curl fails
//     in the middle.
//   - Weak indicators (generic network errors) only trigger when exit code != 0,
//     to avoid false positives from scripts that handle errors gracefully.
func looksLikeNetworkDenial(resp map[string]any) bool {
	stdout, _ := resp["stdout"].(string)
	if stdout == "" {
		return false
	}

	lower := strings.ToLower(stdout)

	// Strong indicators — these are unambiguous proxy/tunnel denial signals.
	// They fire regardless of exit code because agents typically write multi-line
	// scripts where curl fails mid-script but the overall exit code is 0.
	for _, indicator := range proxyDenialIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	// Weak indicators — require non-zero exit code to reduce false positives.
	if !hasNonZeroExitCode(resp) {
		return false
	}
	for _, indicator := range networkErrorIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// hasNonZeroExitCode returns true if the response has a non-zero exit_code field.
func hasNonZeroExitCode(resp map[string]any) bool {
	exitCode, ok := resp["exit_code"]
	if !ok {
		return false
	}
	switch v := exitCode.(type) {
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}

// proxyDenialIndicators are strong signals that the L7 proxy explicitly blocked
// a connection. These are unambiguous — a CONNECT tunnel 403 or explicit egress
// denial can only come from the proxy, not from the target server.
var proxyDenialIndicators = []string{
	"connect tunnel failed, response 403",
	"connect tunnel failed",
	"proxy returned 403",
	"proxy refused connection",
	"egress denied",
	"egress blocked",
	"policy violation",
	"http/1.1 403 forbidden\r\r\n< content-type: application/json\r\r\n< content-length: 101",
}

// networkErrorIndicators are weaker signals that require a non-zero exit code
// to be actionable. These could appear in normal error handling output, so we
// only flag them when the overall command failed.
var networkErrorIndicators = []string{
	"connection refused",
	"connection reset",
	"connection timed out",
	"network is unreachable",
	"no route to host",
	"403 forbidden",
	"ssl_error_handshake",
	"could not resolve host",
	"name or service not known",
	"failed to connect",
}

// extractDenialsFromOutput parses tool stdout to extract denied host:port pairs.
// Returns a slice of maps suitable for JSON serialization in the SSE event.
//
// It looks for patterns like:
//   - "CONNECT identity-3.qa-de-1.cloud.sap:443 HTTP/1.1" followed by 403
//   - "curl: (56) CONNECT tunnel failed, response 403"
//   - Direct "Establish HTTP proxy tunnel to host:port"
func extractDenialsFromOutput(stdout string) []map[string]any {
	seen := make(map[string]bool)
	var denials []map[string]any

	// Pattern 1: "CONNECT host:port HTTP/1.1" — the proxy CONNECT request
	for _, match := range connectHostPattern.FindAllStringSubmatch(stdout, -1) {
		host := match[1]
		port := 443 // default
		if match[2] != "" {
			if p, err := strconv.Atoi(match[2]); err == nil {
				port = p
			}
		}
		key := fmt.Sprintf("%s:%d", host, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		denials = append(denials, map[string]any{
			"host":            host,
			"port":            port,
			"broader_pattern": openshell.SuggestBroaderPattern(host),
		})
	}

	// Pattern 2: "Establish HTTP proxy tunnel to host:port"
	for _, match := range tunnelToPattern.FindAllStringSubmatch(stdout, -1) {
		host := match[1]
		port := 443
		if match[2] != "" {
			if p, err := strconv.Atoi(match[2]); err == nil {
				port = p
			}
		}
		key := fmt.Sprintf("%s:%d", host, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		denials = append(denials, map[string]any{
			"host":            host,
			"port":            port,
			"broader_pattern": openshell.SuggestBroaderPattern(host),
		})
	}

	// Pattern 3: "Could not resolve host: hostname"
	for _, match := range resolveFailPattern.FindAllStringSubmatch(stdout, -1) {
		host := match[1]
		port := 443
		key := fmt.Sprintf("%s:%d", host, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		denials = append(denials, map[string]any{
			"host":            host,
			"port":            port,
			"broader_pattern": openshell.SuggestBroaderPattern(host),
		})
	}

	return denials
}

var (
	// Matches: "> CONNECT host:port HTTP/1.1" or "CONNECT host:port HTTP/"
	connectHostPattern = regexp.MustCompile(`CONNECT\s+([a-zA-Z0-9._-]+):(\d+)\s+HTTP/`)
	// Matches: "Establish HTTP proxy tunnel to host:port"
	tunnelToPattern = regexp.MustCompile(`tunnel to ([a-zA-Z0-9._-]+):(\d+)`)
	// Matches: "Could not resolve host: hostname"
	resolveFailPattern = regexp.MustCompile(`(?i)could not resolve host:\s*([a-zA-Z0-9._-]+)`)
)

// networkToolNames is the set of tools that make HTTP requests to external
// endpoints and whose errors should be checked for proxy denial indicators.
var networkToolNames = map[string]bool{
	"browser_navigate": true,
	"browser_tabs":     true,
	"web_fetch":        true,
	"http_request":     true,
	"read_pdf":         true,
}

// isNetworkTool returns true if the tool name is one that makes external
// network requests and should be checked for proxy denial errors.
func isNetworkTool(name string) bool {
	return networkToolNames[name]
}

// looksLikeNetworkToolDenial checks the error field of a non-shell tool's
// response for indicators that the failure was caused by the L7 proxy blocking
// the connection. These tools return errors in resp["error"] (set by the ADK
// when a tool function returns a non-nil Go error).
//
// The Go HTTP client, when routed through the OpenShell MITM proxy, produces
// errors containing strings like:
//   - "proxyconnect tcp: dial tcp ... 403"
//   - "connect tunnel failed, response 403"
//   - "net::ERR_TUNNEL_CONNECTION_FAILED" (Chrome/CDP via rod)
//   - "Proxy-Connection: keep-alive\r\n\r\nHTTP/1.1 403 Forbidden"
func looksLikeNetworkToolDenial(resp map[string]any) bool {
	errVal, ok := resp["error"]
	if !ok || errVal == nil {
		return false
	}

	var errStr string
	switch v := errVal.(type) {
	case string:
		errStr = v
	case error:
		errStr = v.Error()
	default:
		return false
	}

	if errStr == "" {
		return false
	}

	lower := strings.ToLower(errStr)

	// Check strong proxy denial indicators (same as shell_command).
	for _, indicator := range proxyDenialIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	// Chrome-specific errors from browser tools via CDP/rod.
	for _, indicator := range chromeNetworkDenialIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	// Go HTTP client proxy rejection patterns. When the proxy returns a
	// non-2xx status to the CONNECT request, Go's transport wraps it as:
	//   Get "https://host/path": Forbidden
	//   Get "https://host/path": Proxy Authentication Required
	// These are unambiguous proxy denials when they appear in a tool's
	// error field (the tool never got a response from the target server).
	for _, indicator := range goHTTPProxyDenialIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	// Weak indicators — always trigger for tool errors because a non-nil
	// error return already implies failure (unlike shell_command where
	// exit code 0 scripts may contain incidental error text).
	for _, indicator := range networkErrorIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}

	return false
}

// chromeNetworkDenialIndicators are error strings specific to Chrome/CDP
// when the browser cannot establish a connection through the proxy.
var chromeNetworkDenialIndicators = []string{
	"net::err_tunnel_connection_failed",
	"net::err_proxy_connection_failed",
	"net::err_connection_refused",
	"net::err_connection_reset",
	"net::err_connection_closed",
	"net::err_name_not_resolved",
	"net::err_proxy_auth_unsupported",
}

// goHTTPProxyDenialIndicators match Go's net/http transport error format when
// a proxy returns a non-2xx status to the CONNECT request. Go wraps these as:
//
//	Get "https://host/path": Forbidden
//	Get "https://host/path": Proxy Authentication Required
//
// The quoted-URL-then-colon-then-status pattern is unique to url.Error and
// won't match legitimate server 403 responses (which come as successful HTTP
// responses with a status code, not as Go errors).
var goHTTPProxyDenialIndicators = []string{
	"\": forbidden",
	"\": proxy authentication required",
	"\": service unavailable",
	"\": bad gateway",
}

// extractDenialFromToolError extracts the denied host and port from a network
// tool's error message and/or its response map. It tries multiple strategies:
//  1. Parse the URL from the response map (tools store their target URL)
//  2. Use the fallbackURL (from FunctionCall.Args) if provided
//  3. Parse host:port patterns from the error string itself
//  4. Fall back to the same extraction patterns used for shell_command stdout
func extractDenialFromToolError(resp map[string]any, fallbackURL string) []map[string]any {
	seen := make(map[string]bool)
	var denials []map[string]any

	// Strategy 1: Extract host from URL fields in the response.
	// Tools like web_fetch, http_request, browser_navigate include the URL.
	for _, key := range []string{"url", "URL", "final_url"} {
		if urlStr, ok := resp[key].(string); ok && urlStr != "" {
			if host, port := hostPortFromURL(urlStr); host != "" {
				k := fmt.Sprintf("%s:%d", host, port)
				if !seen[k] {
					seen[k] = true
					denials = append(denials, map[string]any{
						"host":            host,
						"port":            port,
						"broader_pattern": openshell.SuggestBroaderPattern(host),
					})
				}
			}
		}
	}

	// Strategy 2: Use fallback URL from the original FunctionCall.Args["url"].
	// This covers Chrome/CDP errors (net::ERR_TUNNEL_CONNECTION_FAILED) which
	// don't include the target hostname in the error string.
	if fallbackURL != "" {
		if host, port := hostPortFromURL(fallbackURL); host != "" {
			k := fmt.Sprintf("%s:%d", host, port)
			if !seen[k] {
				seen[k] = true
				denials = append(denials, map[string]any{
					"host":            host,
					"port":            port,
					"broader_pattern": openshell.SuggestBroaderPattern(host),
				})
			}
		}
	}

	// Strategy 3: Parse host from the error string using existing patterns
	// and additional patterns for Go HTTP client errors.
	errStr := ""
	if v, ok := resp["error"].(string); ok {
		errStr = v
	}
	if errStr != "" {
		// Go HTTP client errors often contain the target host:port, e.g.:
		// "proxyconnect tcp: dial tcp api.example.com:443: ..."
		// "Get \"https://api.example.com/path\": proxyconnect ..."
		for _, match := range goHTTPHostPattern.FindAllStringSubmatch(errStr, -1) {
			host := match[1]
			port := 443
			if match[2] != "" {
				if p, err := strconv.Atoi(match[2]); err == nil {
					port = p
				}
			}
			k := fmt.Sprintf("%s:%d", host, port)
			if !seen[k] {
				seen[k] = true
				denials = append(denials, map[string]any{
					"host":            host,
					"port":            port,
					"broader_pattern": openshell.SuggestBroaderPattern(host),
				})
			}
		}

		// Also try extracting a URL from the error (Go wraps it in quotes).
		for _, match := range urlInErrorPattern.FindAllStringSubmatch(errStr, -1) {
			if host, port := hostPortFromURL(match[1]); host != "" {
				k := fmt.Sprintf("%s:%d", host, port)
				if !seen[k] {
					seen[k] = true
					denials = append(denials, map[string]any{
						"host":            host,
						"port":            port,
						"broader_pattern": openshell.SuggestBroaderPattern(host),
					})
				}
			}
		}

		// Fall back to the same patterns used for shell stdout.
		for _, match := range connectHostPattern.FindAllStringSubmatch(errStr, -1) {
			host := match[1]
			port := 443
			if match[2] != "" {
				if p, err := strconv.Atoi(match[2]); err == nil {
					port = p
				}
			}
			k := fmt.Sprintf("%s:%d", host, port)
			if !seen[k] {
				seen[k] = true
				denials = append(denials, map[string]any{
					"host":            host,
					"port":            port,
					"broader_pattern": openshell.SuggestBroaderPattern(host),
				})
			}
		}
	}

	return denials
}

var (
	// Matches host:port in Go HTTP client dial errors, e.g.
	// "dial tcp api.example.com:443: connect: connection refused"
	goHTTPHostPattern = regexp.MustCompile(`dial tcp ([a-zA-Z0-9._-]+):(\d+)`)
	// Matches a URL in quotes within an error message, e.g.
	// `Get "https://api.example.com/path": proxyconnect ...`
	urlInErrorPattern = regexp.MustCompile(`"(https?://[^"]+)"`)
)

// hostPortFromURL parses a URL string and returns (host, port).
// Defaults port to 443 for https, 80 for http.
func hostPortFromURL(rawURL string) (string, int) {
	parsed, err := nurl.Parse(rawURL)
	if err != nil {
		return "", 0
	}
	host := parsed.Hostname()
	if host == "" {
		return "", 0
	}
	port := 443
	if parsed.Scheme == "http" {
		port = 80
	}
	if parsed.Port() != "" {
		if p, err := strconv.Atoi(parsed.Port()); err == nil {
			port = p
		}
	}
	return host, port
}

// filterDenialsByPolicy checks each detected denial against the effective
// network policy (loaded from the runner's context). Endpoints that are:
//   - PolicyAllow: auto-approved via gateway in the background, removed from list
//   - PolicyDeny: silently dropped, removed from list
//   - PolicyUnknown: kept in the returned list (will prompt the user)
//
// Returns the filtered list of denials that still need interactive approval.
func (cr *ChatRunner) filterDenialsByPolicy(denials []map[string]any) []map[string]any {
	nps := store.NetworkPolicyStoresFromContext(cr.ctx)
	if nps == nil {
		// No policy stores configured — all denials prompt the user.
		return denials
	}

	ep := &EffectivePolicy{
		Platform: loadRulesQuiet(cr.ctx, nps.Platform),
		Org:      loadRulesQuiet(cr.ctx, nps.Org),
		Team:     loadRulesQuiet(cr.ctx, nps.Team),
	}

	var unknown []map[string]any
	for _, d := range denials {
		host, _ := d["host"].(string)
		port := uint32(443)
		if p, ok := d["port"].(int); ok {
			port = uint32(p)
		}

		switch ep.Check(host, port) {
		case PolicyAllow:
			// Synchronously add the endpoint to sandbox policy so the agent's
			// next retry (same run iteration) succeeds immediately.
			cr.autoApproveEndpoint(host, port)
		case PolicyDeny:
			// Silently suppress — do not show dialog or approve.
			slog.Info("network denial suppressed by policy",
				"host", host, "port", port, "session", cr.SessionID)
		default: // PolicyUnknown
			unknown = append(unknown, d)
		}
	}
	return unknown
}

// autoApproveEndpoint adds an endpoint to the running sandbox's network policy
// via the gateway, without user interaction. This is called when the effective
// policy says "allow" for a detected denial.
func (cr *ChatRunner) autoApproveEndpoint(host string, port uint32) {
	if cr.gatewayConfig == nil {
		slog.Warn("auto-approve: no gateway config available",
			"host", host, "port", port, "session", cr.SessionID)
		return
	}

	sandboxName := openshell.SandboxName(cr.SessionID)

	gateway, err := openshell.NewGRPCGatewayClient(*cr.gatewayConfig)
	if err != nil {
		slog.Warn("auto-approve: failed to create gateway client",
			"host", host, "port", port, "error", err)
		return
	}
	defer gateway.Close()

	ops := []openshell.PolicyMergeOp{
		{
			Type:     openshell.PolicyMergeAddEndpoint,
			RuleName: "astonish-egress",
			Endpoint: &openshell.EndpointSpec{Host: host, Port: port},
		},
	}

	resp, err := gateway.UpdateConfig(cr.ctx, sandboxName, ops)
	if err != nil {
		slog.Warn("auto-approve: failed to update sandbox policy",
			"host", host, "port", port, "sandbox", sandboxName, "error", err)
		return
	}

	// Wait for the proxy to load the new policy before the agent retries.
	waitForPolicyLoad(cr.ctx, gateway, sandboxName, resp.PolicyVersion)

	slog.Info("auto-approved network access via policy",
		"host", host, "port", port, "sandbox", sandboxName, "session", cr.SessionID)
}

// loadRulesQuiet loads rules from a store, returning empty slice on nil or error.
func loadRulesQuiet(ctx context.Context, s store.NetworkPolicyStore) []store.NetworkPolicyRule {
	if s == nil {
		return nil
	}
	rules, err := s.List(ctx)
	if err != nil {
		slog.Warn("failed to load network policy rules", "error", err)
		return nil
	}
	return rules
}

// preSeedNetworkPolicy pushes all "allow" endpoints from the effective network
// policy (platform/org/team) to the running sandbox's proxy. This ensures that
// endpoints added to the allow-list after sandbox creation are honoured
// immediately without requiring a failed request + reactive auto-approve cycle.
//
// Called once at the start of each ChatRunner.Run(). Best-effort: errors are
// logged but do not abort the chat — the reactive auto-approve path remains as
// a fallback.
func (cr *ChatRunner) preSeedNetworkPolicy() {
	if cr.gatewayConfig == nil {
		return
	}
	nps := store.NetworkPolicyStoresFromContext(cr.ctx)
	if nps == nil {
		return
	}

	// Collect all "allow" rules across tiers.
	var endpoints []openshell.EndpointSpec
	for _, rules := range [][]store.NetworkPolicyRule{
		loadRulesQuiet(cr.ctx, nps.Platform),
		loadRulesQuiet(cr.ctx, nps.Org),
		loadRulesQuiet(cr.ctx, nps.Team),
	} {
		for _, r := range rules {
			if r.Action == store.NetworkPolicyAllow {
				endpoints = append(endpoints, openshell.EndpointSpec{
					Host: r.Host,
					Port: r.Port,
				})
			}
		}
	}

	if len(endpoints) == 0 {
		return
	}

	sandboxName := openshell.SandboxName(cr.SessionID)

	gateway, err := openshell.NewGRPCGatewayClient(*cr.gatewayConfig)
	if err != nil {
		slog.Warn("pre-seed network policy: failed to create gateway client",
			"session", cr.SessionID, "error", err)
		return
	}
	defer gateway.Close()

	// Build merge operations for all allow-list endpoints.
	ops := make([]openshell.PolicyMergeOp, 0, len(endpoints))
	for _, ep := range endpoints {
		ops = append(ops, openshell.PolicyMergeOp{
			Type:     openshell.PolicyMergeAddEndpoint,
			RuleName: "astonish-egress",
			Endpoint: &openshell.EndpointSpec{Host: ep.Host, Port: ep.Port},
		})
	}

	_, err = gateway.UpdateConfig(cr.ctx, sandboxName, ops)
	if err != nil {
		// Non-fatal: the reactive auto-approve path will cover these cases
		// when the first denial is detected.
		slog.Warn("pre-seed network policy: UpdateConfig failed",
			"sandbox", sandboxName, "session", cr.SessionID, "endpoints", len(endpoints), "error", err)
		return
	}

	slog.Info("pre-seeded network policy with allow-list endpoints",
		"sandbox", sandboxName, "session", cr.SessionID, "count", len(endpoints))
}
