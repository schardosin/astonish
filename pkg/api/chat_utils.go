package api

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/credentials"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// titleThinkTagRe strips <think>/<thinking> blocks that some models emit in
// title-generation responses.
var titleThinkTagRe = regexp.MustCompile(`(?s)<(?:think|thinking)>.*?</(?:think|thinking)>`)

// userTimestampRe matches the timestamp prefix injected by NewTimestampedUserContent.
// Format: "[2026-03-20 14:30:05 UTC]\n"
var userTimestampRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \w+\]\n`)

// eventsToMessages transforms ADK session events into a flat message list for the frontend.
// An optional redactor is applied to all text parts and tool args/results to prevent
// credential exposure. This is the defense-in-depth layer: even if retroactive transcript
// redaction missed a secret, the UI will never display it in plaintext.
func eventsToMessages(events session.Events, redactor *credentials.Redactor) []StudioMessage {
	var messages []StudioMessage
	var lastInvocationID string // track invocation boundary for coalescing

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		role := string(event.LLMResponse.Content.Role)
		eventInvID := event.InvocationID

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				msgType := "agent"
				text := part.Text
				if role == "user" {
					msgType = "user"
					// Strip the timestamp prefix injected by NewTimestampedUserContent.
					// Format: "[2026-03-20 14:30:05 UTC]\n<text>"
					text = stripUserMessageTimestamp(text)
				}
				// Defense-in-depth: redact any credential values from text
				// before sending to the frontend. This catches secrets in user
				// messages and agent responses that may have been persisted
				// before the credential was registered with the redactor.
				if redactor != nil {
					text = redactor.Redact(text)
				}
				// Coalesce with previous message of same type, but only within
				// the same invocation. Different invocations represent separate
				// user turns — merging across them produces garbled messages
				// (e.g. when multiple user messages have no model response between them).
				if len(messages) > 0 && eventInvID == lastInvocationID {
					last := &messages[len(messages)-1]
					if last.Type == msgType && last.ToolName == "" {
						last.Content += text
						continue
					}
				}
				messages = append(messages, StudioMessage{
					Type:    msgType,
					Content: text,
				})
				lastInvocationID = eventInvID
			}
			if part.FunctionCall != nil {
				args := part.FunctionCall.Args
				if redactor != nil && args != nil {
					args = redactor.RedactMap(args)
				}
				messages = append(messages, StudioMessage{
					Type:     "tool_call",
					ToolName: part.FunctionCall.Name,
					ToolArgs: args,
				})
			}
			if part.FunctionResponse != nil {
				resp := part.FunctionResponse.Response
				if redactor != nil && resp != nil {
					resp = redactor.RedactMap(resp)
				}
				messages = append(messages, StudioMessage{
					Type:       "tool_result",
					ToolName:   part.FunctionResponse.Name,
					ToolResult: summarizeToolResult(resp),
				})
			}
		}
	}

	return messages
}

// stripUserMessageTimestamp removes the timestamp prefix from a user message
// that was injected by NewTimestampedUserContent. This keeps the timestamp in
// the persisted event (for the LLM's context) while displaying clean text to
// the user in the Studio UI.
func stripUserMessageTimestamp(text string) string {
	return userTimestampRe.ReplaceAllString(text, "")
}

// summarizeToolResult converts a tool response map into a display-friendly value.
func summarizeToolResult(resp map[string]any) interface{} {
	if resp == nil {
		return nil
	}
	if v, ok := resp["result"]; ok {
		if s, ok := v.(string); ok {
			return truncateResult(s)
		}
	}
	if v, ok := resp["output"]; ok {
		if s, ok := v.(string); ok {
			return truncateResult(s)
		}
	}
	if v, ok := resp["error"]; ok {
		return v
	}
	return resp
}

// truncateResult limits tool output to a reasonable length for display.
func truncateResult(s string) string {
	const maxLen = 2000
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n\n... (truncated)"
}

// fleetEventsToMessages converts ADK events from a fleet transcript back into
// fleet-style message summaries for the frontend.
func fleetEventsToMessages(events []*session.Event) []FleetMessageSummary {
	var messages []FleetMessageSummary
	for _, event := range events {
		if event.LLMResponse.Content == nil {
			continue
		}
		// Extract text from all parts
		var text string
		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				text += part.Text
			}
		}
		if text == "" {
			continue
		}

		// Determine sender from Author field (set by fleetMessageToEvent)
		sender := event.Author
		if sender == "" {
			// Fallback: infer from role
			if event.LLMResponse.Content.Role == genai.RoleUser {
				sender = "customer"
			} else {
				sender = "agent"
			}
		}
		if sender == "user" {
			sender = "customer"
		}

		messages = append(messages, FleetMessageSummary{
			ID:        event.ID,
			Sender:    sender,
			Text:      text,
			Timestamp: event.Timestamp.Format(time.RFC3339Nano),
		})
	}
	return messages
}

// generateStudioSessionTitle calls the LLM to produce a short session title.
func generateStudioSessionTitle(llm model.LLM, store *persistentsession.FileStore, sessionID, userMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(
		"Generate a concise title (5-7 words max) for a conversation that starts with this message. "+
			"Return ONLY the title, no quotes, no punctuation at the end.\n\nUser message: %s", userMessage)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			Temperature:     genai.Ptr(float32(0.3)),
			MaxOutputTokens: 30,
		},
	}

	var title string
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				title += part.Text
			}
		}
	}

	title = titleThinkTagRe.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	if err := store.SetSessionTitle(sessionID, title); err != nil {
		slog.Warn("failed to set session title", "session_id", sessionID, "error", err)
	}
}

// toInt converts an interface{} to int (handles float64 from JSON).
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// persistRunError saves a synthetic model event to the session when the runner
// returns an error. Without this, errors are only sent as transient SSE events
// and disappear on page reload — the user sees their message but no indication
// that the model failed.
func persistRunError(ctx context.Context, svc session.Service, sessionID string, runErr error) {
	resp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   studioChatAppName,
		UserID:    studioChatUserID,
		SessionID: sessionID,
	})
	if err != nil {
		slog.Error("failed to get session", "component", "persistRunError", "session_id", sessionID, "error", err)
		return
	}

	errorEvent := &session.Event{
		ID:        fmt.Sprintf("error-%d", time.Now().UnixMilli()),
		Author:    "model",
		Timestamp: time.Now(),
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: fmt.Sprintf("[Error: %s]", runErr.Error())}},
			},
		},
	}

	if err := svc.AppendEvent(ctx, resp.Session, errorEvent); err != nil {
		slog.Error("failed to append error event to session", "component", "persistRunError", "session_id", sessionID, "error", err)
	}
}
