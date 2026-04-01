package agent

import (
	"log/slog"
	"strings"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// EphemeralKnowledgeCallback returns a BeforeModelCallback that injects
// auto-retrieved knowledge as an ephemeral Part in the last user message.
// This content is visible to the LLM but never persisted to session history,
// keeping the conversation history stable for provider KV-cache prefix matching.
//
// The injected Part is prepended to the last user Content's Parts array,
// placing it immediately before the user's actual message text.
//
// When debugMode is true, the callback logs injection details (token estimate,
// content type) to stdout. This output goes to server logs only — it is NOT
// persisted to the session.
func EphemeralKnowledgeCallback(relevantKnowledge string, debugMode bool) llmagent.BeforeModelCallback {
	if relevantKnowledge == "" {
		if debugMode {
			slog.Debug("ephemeral knowledge callback not created: no knowledge", "component", "chat")
		}
		return nil
	}

	injectionText := buildKnowledgeInjectionText(relevantKnowledge)
	if injectionText == "" {
		return nil
	}

	// Estimate token count (~4 chars per token is a reasonable approximation)
	estimatedTokens := len(injectionText) / 4

	if debugMode {
		slog.Debug("ephemeral knowledge callback created", "component", "chat", "contentType", "knowledge", "estimatedTokens", estimatedTokens)
	}

	return func(_ adkagent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
		if req == nil || len(req.Contents) == 0 {
			return nil, nil
		}

		// Find the last user-role Content in the request
		lastUserIdx := -1
		for i := len(req.Contents) - 1; i >= 0; i-- {
			if req.Contents[i] != nil && req.Contents[i].Role == "user" {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx < 0 {
			if debugMode {
				slog.Debug("ephemeral knowledge injection skipped: no user message in request", "component", "chat")
			}
			return nil, nil // no user message found
		}

		// Prepend the knowledge as a new Part before existing Parts.
		// This places it right before the user's actual text, which is
		// where models attend most strongly. Using a separate Part keeps
		// the injection cleanly separated from the user's message text.
		knowledgePart := &genai.Part{Text: injectionText}
		userContent := req.Contents[lastUserIdx]
		userContent.Parts = append([]*genai.Part{knowledgePart}, userContent.Parts...)

		if debugMode {
			slog.Debug("ephemeral knowledge injected into user message", "component", "chat", "estimatedTokens", estimatedTokens, "contentIndex", lastUserIdx)
		}

		return nil, nil // proceed with the modified request
	}
}

// buildKnowledgeInjectionText formats the knowledge into the text that will
// be injected into the user message.
func buildKnowledgeInjectionText(relevantKnowledge string) string {
	if relevantKnowledge == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Knowledge For This Task]\n\n")
	sb.WriteString("CRITICAL — You MUST apply the following knowledge when executing this task. ")
	sb.WriteString("It contains proven commands, specific flags, and workarounds that are KNOWN TO WORK ")
	sb.WriteString("from previous sessions. Use the exact commands and approaches described here:\n\n")
	sb.WriteString(relevantKnowledge)

	return sb.String()
}
