package agent

import (
	"context"
	"fmt"
	"strings"
)

// ActiveApp tracks the state of a generative UI app being refined in a chat session.
// Similar to DistillReview but for iterative visual app refinement.
type ActiveApp struct {
	AppID         string   `json:"appId"`         // Stable UUID for cross-turn matching
	Title         string   `json:"title"`         // Human-readable title (e.g. "Sales Dashboard")
	Code          string   `json:"code"`          // Current version's JSX source code
	Versions      []string `json:"versions"`      // History of all code versions (index 0 = v1)
	Version       int      `json:"version"`       // Current version number (1-based)
	Modifications []string `json:"modifications"` // History of user change requests
}

// AppRefinementIntent represents what the user wants to do with an active app.
type AppRefinementIntent int

const (
	AppIntentRefine    AppRefinementIntent = iota // User wants to modify the app
	AppIntentSave                                 // User wants to save the app and stop refining
	AppIntentDone                                 // User is done refining (legacy, treated as save)
	AppIntentUnrelated                            // Message is unrelated to the app
)

// HasActiveApp returns true if the given session has an active app being refined.
func (c *ChatAgent) HasActiveApp(sessionID string) bool {
	c.activeAppMu.Lock()
	defer c.activeAppMu.Unlock()
	_, ok := c.activeApps[sessionID]
	return ok
}

// GetActiveApp returns the active app for the given session, or nil.
func (c *ChatAgent) GetActiveApp(sessionID string) *ActiveApp {
	c.activeAppMu.Lock()
	defer c.activeAppMu.Unlock()
	if c.activeApps == nil {
		return nil
	}
	return c.activeApps[sessionID]
}

// SetActiveApp stores or updates the active app for a session.
func (c *ChatAgent) SetActiveApp(sessionID string, app *ActiveApp) {
	c.activeAppMu.Lock()
	defer c.activeAppMu.Unlock()
	if c.activeApps == nil {
		c.activeApps = make(map[string]*ActiveApp)
	}
	c.activeApps[sessionID] = app
}

// ClearActiveApp removes the active app for a session.
func (c *ChatAgent) ClearActiveApp(sessionID string) {
	c.activeAppMu.Lock()
	defer c.activeAppMu.Unlock()
	delete(c.activeApps, sessionID)
}

// RecordAppModification appends a user change request to the active app's
// modification history. This is used for context injection so the LLM can see
// the evolution of the app across turns.
func (c *ChatAgent) RecordAppModification(sessionID string, modification string) {
	c.activeAppMu.Lock()
	defer c.activeAppMu.Unlock()
	if app, ok := c.activeApps[sessionID]; ok {
		app.Modifications = append(app.Modifications, modification)
	}
}

// ClassifyAppIntent determines whether a user message is a refinement request,
// a "done" signal, or unrelated to the active app. Uses magic string markers
// from UI buttons first, then falls back to LLM classification.
//
// llmFunc should be a simple prompt→response function (same signature as
// FlowDistiller.LLM). Pass nil to skip LLM classification and use heuristics only.
func (c *ChatAgent) ClassifyAppIntent(ctx context.Context, msg string, llmFunc func(ctx context.Context, prompt string) (string, error)) AppRefinementIntent {
	trimmed := strings.TrimSpace(msg)

	// Magic markers from UI buttons
	if trimmed == "__app_save__" {
		return AppIntentSave
	}
	if trimmed == "__app_done__" {
		return AppIntentSave // Done is now equivalent to save
	}

	// Quick heuristics for obvious cases
	lower := strings.ToLower(trimmed)
	saveKeywords := []string{"done", "i'm done", "im done", "that's it", "looks good", "perfect",
		"save it", "save this app", "save this", "save the app", "ship it"}
	for _, kw := range saveKeywords {
		if lower == kw {
			return AppIntentSave
		}
	}

	// LLM classification if available
	if llmFunc != nil {
		intent := c.classifyAppViaLLM(ctx, trimmed, llmFunc)
		if intent >= 0 {
			return intent
		}
	}

	// Fallback: treat as refinement (safe default — user can always say "done")
	return AppIntentRefine
}

// classifyAppViaLLM asks the LLM to classify the user's intent regarding an active app.
// Returns -1 if classification fails.
func (c *ChatAgent) classifyAppViaLLM(ctx context.Context, msg string, llmFunc func(ctx context.Context, prompt string) (string, error)) AppRefinementIntent {
	prompt := fmt.Sprintf(`You are classifying a user's message during an interactive app refinement session.
The user has just been shown a generated React component (visual app preview) and can do one of three things:

1. REFINE — They want to modify the app. Examples: "Make the header blue", "Add a search bar", "Change the chart to a bar chart", "Make it responsive".
2. SAVE — They are satisfied and want to save the app. Examples: "Looks good", "I'm done", "That's perfect", "Save it", "Ship it", "Save this app".
3. UNRELATED — Their message has nothing to do with the app being shown. Examples: "What's the weather?", "Help me write a function", "Search for Python docs".

User message: %q

Respond with exactly one word: REFINE, SAVE, or UNRELATED`, msg)

	response, err := llmFunc(ctx, prompt)
	if err != nil {
		return -1
	}

	answer := strings.ToUpper(strings.TrimSpace(response))
	// Extract the first word in case the LLM is verbose
	if idx := strings.IndexAny(answer, " \n\t.,"); idx > 0 {
		answer = answer[:idx]
	}

	switch answer {
	case "REFINE":
		return AppIntentRefine
	case "SAVE", "DONE":
		return AppIntentSave
	case "UNRELATED":
		return AppIntentUnrelated
	default:
		return -1
	}
}

// BuildAppRefinementContext builds a system prompt context string that injects the
// current app's source code and modification history. This is set as SessionContext
// on the SystemPromptBuilder so the LLM knows it's refining an existing app.
func BuildAppRefinementContext(app *ActiveApp) string {
	var sb strings.Builder

	sb.WriteString("## Active App Refinement\n\n")
	sb.WriteString(fmt.Sprintf("The user is refining a visual app called %q (version %d).\n", app.Title, app.Version))
	sb.WriteString("Apply the user's requested changes to the CURRENT source code below and output the COMPLETE updated component.\n")
	sb.WriteString("You MUST output the full component inside an ```astonish-app code fence — do NOT output a diff or partial snippet.\n")
	sb.WriteString("Preserve all existing functionality unless the user explicitly asks to remove it.\n\n")

	sb.WriteString("### Current Source Code\n\n")
	sb.WriteString("```jsx\n")
	sb.WriteString(app.Code)
	sb.WriteString("\n```\n\n")

	if len(app.Modifications) > 0 {
		sb.WriteString("### Previous Modifications\n\n")
		for i, mod := range app.Modifications {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, mod))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
