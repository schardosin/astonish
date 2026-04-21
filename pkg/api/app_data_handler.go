package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/mcp"
)

// appDataRequest is the JSON body for POST /api/apps/data.
// sourceId uses convention-based routing:
//   - "mcp:<serverName>/<toolName>"      → invoke MCP tool
//   - "http:<METHOD>:<url>"              → make HTTP request (no auth)
//   - "http:<METHOD>:<url>@<credential>" → make HTTP request with credential auth
//   - "static:<key>"                     → return static data from app config
type appDataRequest struct {
	SourceID  string         `json:"sourceId"`
	Args      map[string]any `json:"args"`
	RequestID string         `json:"requestId"`
	AppName   string         `json:"appName,omitempty"` // Optional: for saved app data source lookups
}

// appDataResponse is the JSON response for data/action endpoints.
type appDataResponse struct {
	RequestID string `json:"requestId"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AppDataHandler handles one-shot data requests from the sandboxed iframe.
// The parent page relays postMessage data_requests here, and the response
// flows back: Go → parent → postMessage → iframe.
//
// POST /api/apps/data
func AppDataHandler(w http.ResponseWriter, r *http.Request) {
	var req appDataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.SourceID == "" {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			RequestID: req.RequestID,
			Error:     "sourceId is required",
		})
		return
	}

	slog.Debug("app data request", "sourceId", req.SourceID, "requestId", req.RequestID)

	data, err := resolveDataSource(r, req.SourceID, req.Args, req.AppName)
	if err != nil {
		slog.Debug("app data request failed", "sourceId", req.SourceID, "error", err)
		respondJSON(w, http.StatusOK, appDataResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, appDataResponse{
		RequestID: req.RequestID,
		Data:      data,
	})
}

// AppActionHandler handles action requests (mutations) from the sandboxed iframe.
// Similar to AppDataHandler but semantically for write operations.
//
// POST /api/apps/action
func AppActionHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ActionID  string         `json:"actionId"`
		Payload   map[string]any `json:"payload"`
		RequestID string         `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.ActionID == "" {
		respondJSON(w, http.StatusBadRequest, appDataResponse{
			RequestID: req.RequestID,
			Error:     "actionId is required",
		})
		return
	}

	slog.Debug("app action request", "actionId", req.ActionID, "requestId", req.RequestID)

	// Actions use the same routing convention as data sources
	data, err := resolveDataSource(r, req.ActionID, req.Payload, "")
	if err != nil {
		respondJSON(w, http.StatusOK, appDataResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, appDataResponse{
		RequestID: req.RequestID,
		Data:      data,
	})
}

// AppStreamHandler provides SSE streaming for saved apps with data sources
// that have a refresh interval. The parent page connects to this and forwards
// data_update events to the iframe.
//
// GET /api/apps/{name}/stream?sourceId=X
func AppStreamHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	sourceID := r.URL.Query().Get("sourceId")

	if sourceID == "" {
		http.Error(w, "sourceId query parameter is required", http.StatusBadRequest)
		return
	}

	// Load the app to get data source config (for interval)
	app, err := apps.LoadApp(name)
	if err != nil {
		http.Error(w, "app not found: "+err.Error(), http.StatusNotFound)
		return
	}

	// Find the data source config
	var interval time.Duration
	var dsArgs map[string]any
	for _, ds := range app.DataSources {
		if ds.ID == sourceID {
			if ds.Interval != "" {
				interval, _ = time.ParseDuration(ds.Interval)
			}
			dsArgs = ds.Config
			break
		}
	}

	// Default to 30s if no interval configured
	if interval <= 0 {
		interval = 30 * time.Second
	}

	// Minimum interval of 5s to prevent abuse
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	// Set up SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	slog.Debug("app stream started", "app", name, "sourceId", sourceID, "interval", interval)

	// Send initial data immediately
	data, err := resolveDataSource(r, sourceID, dsArgs, name)
	sendStreamEvent(w, flusher, sourceID, data, err)

	// Poll at the configured interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			slog.Debug("app stream ended (client disconnected)", "app", name, "sourceId", sourceID)
			return
		case <-ticker.C:
			data, err := resolveDataSource(r, sourceID, dsArgs, name)
			sendStreamEvent(w, flusher, sourceID, data, err)
		}
	}
}

// sendStreamEvent writes a single SSE event with data_update payload.
func sendStreamEvent(w http.ResponseWriter, flusher http.Flusher, sourceID string, data any, err error) {
	evt := map[string]any{
		"sourceId": sourceID,
	}
	if err != nil {
		evt["error"] = err.Error()
	} else {
		evt["data"] = data
	}
	jsonBytes, _ := json.Marshal(evt)
	fmt.Fprintf(w, "event: data_update\ndata: %s\n\n", jsonBytes)
	flusher.Flush()
}

// resolveDataSource routes a sourceId to the appropriate backend:
//   - "mcp:<server>/<tool>"           → MCP tool invocation
//   - "http:<METHOD>:<url>"           → server-side HTTP request (no auth)
//   - "http:<METHOD>:<url>@<cred>"    → server-side HTTP request with credential auth
//   - "static:<key>"                  → static data from app's DataSource config
//
// If sourceId doesn't match any convention, it tries to find a matching
// DataSource in the saved app (if appName is provided).
func resolveDataSource(r *http.Request, sourceID string, args map[string]any, appName string) (any, error) {
	// Convention-based routing
	if strings.HasPrefix(sourceID, "mcp:") {
		return resolveMCPSource(r.Context(), sourceID[4:], args)
	}
	if strings.HasPrefix(sourceID, "http:") {
		return resolveHTTPSource(sourceID[5:], args)
	}
	if strings.HasPrefix(sourceID, "static:") {
		return resolveStaticSource(sourceID[7:], appName)
	}

	// Fallback: look up in saved app's DataSources config
	if appName != "" {
		return resolveAppDataSource(r, sourceID, args, appName)
	}

	return nil, fmt.Errorf("unknown source format: %q (expected mcp:<server>/<tool>, http:<METHOD>:<url>, or static:<key>)", sourceID)
}

// resolveMCPSource invokes an MCP tool.
// serverTool format: "<serverName>/<toolName>"
func resolveMCPSource(ctx context.Context, serverTool string, args map[string]any) (any, error) {
	parts := strings.SplitN(serverTool, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid MCP source format: expected 'server/tool', got %q", serverTool)
	}
	serverName := parts[0]
	toolName := parts[1]

	result, err := mcp.InvokeTool(ctx, serverName, toolName, args)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// credentialSuffixRe matches a @credential-name suffix at the end of a URL.
// Only matches simple names (alphanumeric, dash, underscore) after the last @,
// ensuring it doesn't conflict with @ in URLs (e.g., user:pass@host).
var credentialSuffixRe = regexp.MustCompile(`@([a-zA-Z][a-zA-Z0-9_-]*)$`)

// resolveHTTPSource makes a server-side HTTP request.
// spec format: "<METHOD>:<url>" or "<METHOD>:<url>@<credential-name>"
// When a @credential-name suffix is present, the named credential is resolved
// from the credential store and its auth header is injected into the request.
func resolveHTTPSource(spec string, args map[string]any) (any, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid HTTP source format: expected 'METHOD:url', got %q", spec)
	}
	method := strings.ToUpper(parts[0])
	url := parts[1]

	// Extract @credential-name suffix from URL if present
	var credentialName string
	if m := credentialSuffixRe.FindStringSubmatchIndex(url); m != nil {
		credentialName = url[m[2]:m[3]]
		url = url[:m[0]]
	}

	// Build the request
	var body io.Reader
	if method == "POST" || method == "PUT" || method == "PATCH" {
		if args != nil {
			jsonBytes, err := json.Marshal(args)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			body = strings.NewReader(string(jsonBytes))
		}
	}

	httpReq, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	// Apply any custom headers from args
	if headers, ok := args["headers"].(map[string]any); ok {
		for k, v := range headers {
			if s, ok := v.(string); ok {
				httpReq.Header.Set(k, s)
			}
		}
	}

	// Resolve and inject credential (after custom headers — credential takes precedence for auth)
	if credentialName != "" {
		store := getAPICredentialStore()
		if store == nil {
			return nil, fmt.Errorf("credential store is not available — cannot resolve credential %q", credentialName)
		}
		headerKey, headerValue, err := store.Resolve(credentialName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credential %q: %w", credentialName, err)
		}
		httpReq.Header.Set(headerKey, headerValue)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Try to parse as JSON
	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Return as raw string if not valid JSON
		return string(respBody), nil
	}
	return result, nil
}

// resolveStaticSource returns static data from the app's DataSource config.
func resolveStaticSource(key string, appName string) (any, error) {
	if appName == "" {
		return nil, fmt.Errorf("static source %q requires an app name", key)
	}

	app, err := apps.LoadApp(appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load app %q: %w", appName, err)
	}

	for _, ds := range app.DataSources {
		if ds.ID == key && ds.Type == "static" {
			return ds.Config, nil
		}
	}

	return nil, fmt.Errorf("static data source %q not found in app %q", key, appName)
}

// resolveAppDataSource resolves a sourceId by looking up the app's DataSource config.
// This handles the case where sourceId is a logical name (e.g. "sales_data") and
// the actual routing info is in the saved app's YAML.
func resolveAppDataSource(r *http.Request, sourceID string, args map[string]any, appName string) (any, error) {
	app, err := apps.LoadApp(appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load app %q: %w", appName, err)
	}

	for _, ds := range app.DataSources {
		if ds.ID == sourceID {
			// Merge config args with request args (request args take precedence)
			mergedArgs := make(map[string]any)
			for k, v := range ds.Config {
				mergedArgs[k] = v
			}
			for k, v := range args {
				mergedArgs[k] = v
			}

			// Route based on the data source type
			switch ds.Type {
			case "mcp_tool":
				server, _ := ds.Config["server"].(string)
				tool, _ := ds.Config["tool"].(string)
				if server == "" || tool == "" {
					return nil, fmt.Errorf("mcp_tool data source %q missing server or tool config", sourceID)
				}
				// Remove server/tool from args — they're routing info, not tool args
				delete(mergedArgs, "server")
				delete(mergedArgs, "tool")
				return resolveMCPSource(r.Context(), server+"/"+tool, mergedArgs)

			case "http_api":
				urlStr, _ := ds.Config["url"].(string)
				method, _ := ds.Config["method"].(string)
				if urlStr == "" {
					return nil, fmt.Errorf("http_api data source %q missing url config", sourceID)
				}
				if method == "" {
					method = "GET"
				}
				return resolveHTTPSource(method+":"+urlStr, mergedArgs)

			case "static":
				return ds.Config, nil

			default:
				return nil, fmt.Errorf("unknown data source type %q for %q", ds.Type, sourceID)
			}
		}
	}

	return nil, fmt.Errorf("data source %q not found in app %q", sourceID, appName)
}
