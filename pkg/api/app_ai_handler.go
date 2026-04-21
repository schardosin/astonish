package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// appAIRequest is the JSON body for POST /api/apps/ai.
// It provides a one-shot, non-streaming LLM call for in-app AI features
// (e.g., summarize data, classify items, generate descriptions).
type appAIRequest struct {
	Prompt    string `json:"prompt"`              // User's request text
	System    string `json:"system,omitempty"`    // Optional system instruction
	Context   any    `json:"context,omitempty"`   // Optional structured data context
	RequestID string `json:"requestId,omitempty"` // Echo-back request ID for relay matching
}

// appAIResponse is the JSON response for POST /api/apps/ai.
type appAIResponse struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AppAIHandler handles one-shot LLM calls from the sandboxed iframe.
// The parent page relays postMessage ai_requests here, and the response
// flows back: Go → parent → postMessage → iframe.
//
// POST /api/apps/ai
func AppAIHandler(w http.ResponseWriter, r *http.Request) {
	var req appAIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appAIResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.Prompt == "" {
		respondJSON(w, http.StatusBadRequest, appAIResponse{
			RequestID: req.RequestID,
			Error:     "prompt is required",
		})
		return
	}

	slog.Debug("app AI request", "requestId", req.RequestID, "promptLen", len(req.Prompt), "hasSystem", req.System != "", "hasContext", req.Context != nil)

	// Get the LLM from the ChatManager (same model as the main agent)
	cm := GetChatManager()
	if err := cm.ensureReady(r.Context()); err != nil {
		respondJSON(w, http.StatusInternalServerError, appAIResponse{
			RequestID: req.RequestID,
			Error:     "LLM not available: " + err.Error(),
		})
		return
	}

	llm := cm.components.LLM
	if llm == nil {
		respondJSON(w, http.StatusInternalServerError, appAIResponse{
			RequestID: req.RequestID,
			Error:     "LLM not configured",
		})
		return
	}

	// Build the user prompt — append serialized context if provided
	userPrompt := req.Prompt
	if req.Context != nil {
		contextJSON, err := json.Marshal(req.Context)
		if err == nil && len(contextJSON) > 0 && string(contextJSON) != "null" {
			userPrompt += "\n\n<context>\n" + string(contextJSON) + "\n</context>"
		}
	}

	// Build the LLM request
	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{{Text: userPrompt}},
				Role:  "user",
			},
		},
		Config: &genai.GenerateContentConfig{
			Temperature:     genai.Ptr(float32(0.3)),
			MaxOutputTokens: 4096,
		},
	}

	// Add system instruction if provided
	if strings.TrimSpace(req.System) != "" {
		llmReq.Config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.System}},
		}
	}

	// Non-streaming LLM call with 2-minute timeout
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	var result strings.Builder
	for resp, err := range llm.GenerateContent(ctx, llmReq, false) {
		if err != nil {
			slog.Debug("app AI request failed", "requestId", req.RequestID, "error", err)
			respondJSON(w, http.StatusOK, appAIResponse{
				RequestID: req.RequestID,
				Error:     fmt.Sprintf("LLM error: %v", err),
			})
			return
		}
		if resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p.Text != "" {
					result.WriteString(p.Text)
				}
			}
		}
	}

	text := result.String()
	if text == "" {
		respondJSON(w, http.StatusOK, appAIResponse{
			RequestID: req.RequestID,
			Error:     "LLM returned an empty response",
		})
		return
	}

	slog.Debug("app AI request completed", "requestId", req.RequestID, "responseLen", len(text))

	respondJSON(w, http.StatusOK, appAIResponse{
		RequestID: req.RequestID,
		Text:      text,
	})
}
