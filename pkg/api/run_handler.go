package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ChatRequest represents the request body for /api/chat
type ChatRequest struct {
	AgentID   string `json:"agentId"`
	Message   string `json:"message"` // User input
	SessionID string `json:"sessionId"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
}

// SessionManager manages active sessions
type SessionManager struct {
	service  session.Service
	sessions map[string]session.Session
	mu       sync.RWMutex
}

var globalSessionManager *SessionManager
var sessionOnce sync.Once

// Global MCP manager to maintain browser sessions across requests
var globalMCPManager *mcp.Manager
var globalMCPToolsets []tool.Toolset
var mcpOnce sync.Once
var mcpMu sync.RWMutex

// GetSessionManager returns the singleton session manager
func GetSessionManager() *SessionManager {
	sessionOnce.Do(func() {
		baseService := session.InMemoryService()
		globalSessionManager = &SessionManager{
			service:  common.NewAutoInitService(baseService),
			sessions: make(map[string]session.Session),
		}
	})
	return globalSessionManager
}

// GetMCPToolsets returns the cached MCP toolsets, initializing if needed
func GetMCPToolsets(ctx context.Context) []tool.Toolset {
	mcpOnce.Do(func() {
		var err error
		globalMCPManager, err = mcp.NewManager()
		if err != nil {
			fmt.Printf("[MCP] Failed to create manager: %v\n", err)
			return
		}
		if err := globalMCPManager.InitializeToolsets(ctx); err != nil {
			fmt.Printf("[MCP] Failed to initialize toolsets: %v\n", err)
			return
		}
		globalMCPToolsets = globalMCPManager.GetToolsets()
		fmt.Printf("[MCP] Initialized %d toolsets (browser sessions will persist)\n", len(globalMCPToolsets))
	})
	
	mcpMu.RLock()
	defer mcpMu.RUnlock()
	return globalMCPToolsets
}

// SendSSE sends a Server-Sent Event
func SendSSE(w io.Writer, flusher http.Flusher, eventType string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("Error marshaling SSE data: %v\n", err)
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
	if flusher != nil {
		flusher.Flush()
	}
}

// SendErrorSSE sends an error event
func SendErrorSSE(w io.Writer, flusher http.Flusher, msg string) {
	SendSSE(w, flusher, "error", map[string]string{"error": msg})
}

// HandleChat handles the /api/chat endpoint with SSE streaming
func HandleChat(w http.ResponseWriter, r *http.Request) {
	// Parse request (could be POST body)
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Printf("[Chat API] Error decoding request: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.AgentID == "" {
		http.Error(w, "AgentID is required", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send ping
	SendSSE(w, flusher, "ping", map[string]string{"status": "connected"})

	ctx := r.Context()
	sm := GetSessionManager()

	// 1. Load Agent Config
	agentPath, _, err := findAgentPath(req.AgentID)
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Agent not found: %s", req.AgentID))
		return
	}

	cfg, err := config.LoadAgent(agentPath)
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to load configuration: %v", err))
		return
	}

	// 2. Determine Provider/Model
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load app config: %v\n", err)
		appCfg = &config.AppConfig{}
	}

	providerName := req.Provider
	if providerName == "" {
		providerName = appCfg.General.DefaultProvider
	}
	// Normalize provider name (handle Display Name -> ID mapping)
	// Frontend might send "SAP AI Core" instead of "sap_ai_core"
	normalizedProvider := providerName
	for id, displayName := range provider.ProviderDisplayNames {
		if displayName == providerName {
			normalizedProvider = id
			break
		}
	}
	providerName = normalizedProvider

	if providerName == "" {
		providerName = "gemini"
	}

	modelName := req.Model
	if modelName == "" {
		modelName = appCfg.General.DefaultModel
	}

	// 3. Initialize Provider/LLM
	llm, err := provider.GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to initialize provider: %v", err))
		return
	}

	// 4. Initialize Tools
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to initialize tools: %v", err))
		return
	}

	// Initialize MCP (using global cache to persist browser sessions)
	mcpToolsets := GetMCPToolsets(ctx)

	// 5. Create Astonish Agent & ADK Agent
	astonishAgent := agent.NewAstonishAgentWithToolsets(cfg, llm, internalTools, mcpToolsets)
	astonishAgent.DebugMode = false // Disable verbose debug output
	astonishAgent.IsWebMode = true  // Enable Web mode for UI (disables ANSI colors)
	astonishAgent.SessionService = sm.service

	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_agent",
		Description: cfg.Description,
		Run:         astonishAgent.Run,
	})
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create agent: %v", err))
		return
	}

	// 6. Manage Session
	sm.mu.Lock()
	sess, exists := sm.sessions[req.SessionID]
	if !exists {
		// Create new session
		resp, err := sm.service.Create(ctx, &session.CreateRequest{
			AppName: "astonish",
			UserID:  req.SessionID,
		})
		if err != nil {
			sm.mu.Unlock()
			SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create session: %v", err))
			return
		}
		sess = resp.Session
		sm.sessions[req.SessionID] = sess
	}
	sm.mu.Unlock()

	// 7. Create Runner
	rnr, err := runner.New(runner.Config{
		AppName:        "astonish",
		Agent:          adkAgent,
		SessionService: sm.service,
	})
	if err != nil {
		SendErrorSSE(w, flusher, fmt.Sprintf("Failed to create runner: %v", err))
		return
	}

	// 8. Run & Stream
	var userMsg *genai.Content
	if req.Message != "" {
		userMsg = genai.NewContentFromText(req.Message, genai.RoleUser)
	}

	SendSSE(w, flusher, "status", map[string]string{"status": "running"})

	var lastNodeName string
	var currentNodeType string // Track node type for conditional streaming
	var hasOutputModel bool    // Track if current node has output_model
	var toolCallCount int      // Track tool calls for text suppression

	for event, err := range rnr.Run(ctx, req.SessionID, sess.ID(), userMsg, adkagent.RunConfig{}) {
		if err != nil {
			SendErrorSSE(w, flusher, err.Error())
			return
		}

		// Check for _user_message_display marker - this event has proper display content
		isUserMessageDisplay := event.Actions.StateDelta != nil && event.Actions.StateDelta["_user_message_display"] != nil

		// Stream LLM Text chunks (only for appropriate node types)
		// Suppress for: update_state (internal state changes), tool (internal processing)
		// Allow for: llm, output, input (prompts should be visible to users)
		// EXCEPTION: Always stream tool approval requests (they contain approval_options)
		// EXCEPTION: Always stream _user_message_display events (properly formatted output)
		// SUPPRESS: Text after tool calls for output_model nodes (raw JSON)
		isApprovalRequest := event.Actions.StateDelta != nil && event.Actions.StateDelta["approval_options"] != nil
		shouldStream := currentNodeType == "" || currentNodeType == "llm" || currentNodeType == "output" || currentNodeType == "input" || isApprovalRequest || isUserMessageDisplay

		// For output_model nodes, suppress text AFTER tool calls (this is raw JSON)
		// But allow text BEFORE tools (greeting) and _user_message_display events
		if hasOutputModel && toolCallCount > 0 && !isUserMessageDisplay && !isApprovalRequest {
			shouldStream = false
		}

		if shouldStream && event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" {
					SendSSE(w, flusher, "text", map[string]string{
						"text": part.Text,
					})
				}
			}
		}

		// Track tool calls AFTER processing text for this event
		// This ensures greeting text in the same event as tool call is sent first
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil {
					toolCallCount++
				}
			}
		}

		// Stream State Updates / Node Transitions
		if event.Actions.StateDelta != nil {
			delta := event.Actions.StateDelta

			// Detect node transition
			if nodeName, ok := delta["current_node"].(string); ok {
				// Only send if node actually changed
				if nodeName != lastNodeName {
					lastNodeName = nodeName
					// Also check node_type if available (sometimes implicit)
					nodeType, _ := delta["node_type"].(string)
					currentNodeType = nodeType // Update for streaming filter

					// Reset tool call count for new node
					toolCallCount = 0

					// Check if this node has output_model (from config)
					hasOutputModel = false
					for _, node := range cfg.Nodes {
						if node.Name == nodeName && len(node.OutputModel) > 0 {
							hasOutputModel = true
							break
						}
					}

					SendSSE(w, flusher, "node", map[string]string{
						"node": nodeName,
						"type": nodeType,
					})
				}
			}

			// Check for Retry Info (smart error handling)
			if retryInfoVal, ok := delta["_retry_info"]; ok {
				if retryInfo, ok := retryInfoVal.(map[string]interface{}); ok {
					attempt := 0
					maxRetries := 3
					reason := ""

					if a, ok := retryInfo["attempt"].(int); ok {
						attempt = a
					} else if a, ok := retryInfo["attempt"].(float64); ok {
						attempt = int(a)
					}

					if m, ok := retryInfo["max_retries"].(int); ok {
						maxRetries = m
					} else if m, ok := retryInfo["max_retries"].(float64); ok {
						maxRetries = int(m)
					}

					if r, ok := retryInfo["reason"].(string); ok {
						reason = r
					}

					SendSSE(w, flusher, "retry", map[string]interface{}{
						"attempt":    attempt,
						"maxRetries": maxRetries,
						"reason":     reason,
					})
				}
			}

			// Check for Failure Info (smart error handling)
			if failureInfoVal, ok := delta["_failure_info"]; ok {
				if failureInfo, ok := failureInfoVal.(map[string]interface{}); ok {
					title, _ := failureInfo["title"].(string)
					reason, _ := failureInfo["reason"].(string)
					originalError, _ := failureInfo["original_error"].(string)
					suggestion, _ := failureInfo["suggestion"].(string)

					SendSSE(w, flusher, "error_info", map[string]interface{}{
						"title":         title,
						"reason":        reason,
						"suggestion":    suggestion,
						"originalError": originalError,
					})
				}
			}

			// Capture input request from approval_options (tool approval)
			if options, ok := delta["approval_options"].([]string); ok {
				SendSSE(w, flusher, "input_request", map[string]interface{}{
					"options": options,
				})
			} else if optionsRaw, ok := delta["approval_options"].([]interface{}); ok {
				SendSSE(w, flusher, "input_request", map[string]interface{}{
					"options": optionsRaw,
				})
			}

			// Capture input request from input_options (input node)
			if options, ok := delta["input_options"].([]string); ok && len(options) > 0 {
				// Input node with predefined options
				SendSSE(w, flusher, "input_request", map[string]interface{}{
					"options": options,
				})
			} else if optionsRaw, ok := delta["input_options"].([]interface{}); ok && len(optionsRaw) > 0 {
				// Handle []interface{} case
				SendSSE(w, flusher, "input_request", map[string]interface{}{
					"options": optionsRaw,
				})
			} else if waiting, ok := delta["waiting_for_input"].(bool); ok && waiting {
				// Free-text input (no options) - send empty options to enable input
				SendSSE(w, flusher, "input_request", map[string]interface{}{
					"options": []string{},
				})
			}

			// Send full state delta for UI variables view
			SendSSE(w, flusher, "state", delta)
		}
	}

	SendSSE(w, flusher, "done", map[string]bool{"done": true})
}
