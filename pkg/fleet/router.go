package fleet

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// mentionRegex matches @mentions in text. Matches @word where word consists
// of letters, digits, underscores, or hyphens.
var mentionRegex = regexp.MustCompile(`@([a-zA-Z][a-zA-Z0-9_-]*)`)

// ParseMentions extracts @mentions from message text.
// Returns a deduplicated list of mentioned names (without the @ prefix).
func ParseMentions(text string) []string {
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var mentions []string
	for _, match := range matches {
		name := strings.ToLower(match[1])
		if !seen[name] {
			seen[name] = true
			mentions = append(mentions, name)
		}
	}
	return mentions
}

// RouteHumanMessage determines which agent should handle a human message.
// This is a fast path that doesn't need an LLM call.
//
// If an agent was waiting for human input, the message goes to that agent.
// Otherwise it goes to the entry point agent.
func RouteHumanMessage(fleetCfg *FleetConfig, waitingAgent string) string {
	if waitingAgent != "" {
		return waitingAgent
	}
	return fleetCfg.GetEntryPoint()
}

// RoutingResult holds the outcome of an LLM routing decision.
type RoutingResult struct {
	Target string // agent key, "human", "self", or "none"
	Reason string // brief explanation (for logging)
}

// routingTimeout is the max time allowed for a routing LLM call.
const routingTimeout = 15 * time.Second

// maxMessageLenForRouting is the max characters from a message to include in
// the routing prompt. Longer messages are truncated.
const maxMessageLenForRouting = 800

// RouteWithLLM uses the LLM to determine who should act next after an agent
// posts a message. This handles all the nuance of natural conversation:
// acknowledgments, handoffs, multi-mentions, FYIs, etc.
//
// Returns a RoutingResult with Target set to one of:
//   - An agent key (e.g., "dev") meaning that agent should be activated next
//   - "human" meaning the system should wait for customer input
//   - "self" meaning the sender still has the action (re-activate them)
//   - "none" meaning no one needs to act right now
//
// On error, falls back to regex-based last-mention routing.
func RouteWithLLM(ctx context.Context, msg Message, fleetCfg *FleetConfig, llm model.LLM) RoutingResult {
	senderKey := msg.Sender

	// Build the list of valid targets this sender can talk to
	talksTo := fleetCfg.GetTalksTo(senderKey)
	if len(talksTo) == 0 {
		return RoutingResult{Target: "none", Reason: "sender has no communication targets"}
	}

	// Build the routing prompt
	prompt := buildRoutingPrompt(senderKey, msg.Text, talksTo)

	// Call LLM with a short timeout
	routeCtx, cancel := context.WithTimeout(ctx, routingTimeout)
	defer cancel()

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			Temperature:     genai.Ptr(float32(0.0)),
			MaxOutputTokens: 20,
		},
	}

	var responseText strings.Builder
	for resp, err := range llm.GenerateContent(routeCtx, req, false) {
		if err != nil {
			log.Printf("[fleet-router] LLM error, falling back to regex: %v", err)
			return fallbackRoute(msg, fleetCfg)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	raw := strings.TrimSpace(responseText.String())
	return parseRoutingResponse(raw, senderKey, talksTo, msg, fleetCfg)
}

// buildRoutingPrompt constructs the prompt for the routing LLM call.
func buildRoutingPrompt(sender, messageText string, talksTo []string) string {
	// Truncate long messages
	text := messageText
	if len(text) > maxMessageLenForRouting {
		text = text[:maxMessageLenForRouting] + "..."
	}

	// Build valid targets list
	var targets []string
	for _, t := range talksTo {
		targets = append(targets, t)
	}

	var sb strings.Builder
	sb.WriteString("You are a message router for a team conversation. ")
	sb.WriteString("Given a message from a team member, determine who should act next.\n\n")

	sb.WriteString(fmt.Sprintf("Message from: @%s\n", sender))
	sb.WriteString(fmt.Sprintf("Message:\n\"\"\"\n%s\n\"\"\"\n\n", text))

	sb.WriteString(fmt.Sprintf("Valid targets that @%s can route to: %s\n", sender, strings.Join(targets, ", ")))
	sb.WriteString("Special values: \"human\" (wait for customer input), \"self\" (sender still has the action), \"none\" (no one needs to act)\n\n")

	sb.WriteString("Determine who should act next. Consider:\n")
	sb.WriteString("- If the sender is handing off work or asking someone to do something, that person has the action.\n")
	sb.WriteString("- If the sender addresses multiple people, focus on who is being asked to TAKE ACTION, not who is just being informed.\n")
	sb.WriteString("- If the sender is acknowledging, thanking, or giving an FYI without requesting action, they likely still have the action (return \"self\").\n")
	sb.WriteString("- If the sender asks @human a question or presents something for customer review, return \"human\".\n")
	sb.WriteString("- If the sender says they are completely done and no one else needs to act, return \"none\".\n\n")

	sb.WriteString("Respond with ONLY a single word: one of the valid targets, \"human\", \"self\", or \"none\".")

	return sb.String()
}

// parseRoutingResponse validates the LLM response and maps it to a RoutingResult.
func parseRoutingResponse(raw, senderKey string, talksTo []string, msg Message, fleetCfg *FleetConfig) RoutingResult {
	// Normalize: lowercase, strip quotes and punctuation
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.Trim(normalized, "\"'`.,!@")
	// Handle cases where LLM prefixes with @
	normalized = strings.TrimPrefix(normalized, "@")

	// Check special values
	switch normalized {
	case "human":
		return RoutingResult{Target: "human", Reason: "LLM decided: wait for customer"}
	case "self", senderKey:
		return RoutingResult{Target: "self", Reason: "LLM decided: sender still has the action"}
	case "none", "no one", "nobody", "no_one":
		return RoutingResult{Target: "none", Reason: "LLM decided: no action needed"}
	}

	// Check if it's a valid agent target
	for _, t := range talksTo {
		if normalized == t {
			return RoutingResult{Target: t, Reason: fmt.Sprintf("LLM decided: route to @%s", t)}
		}
	}

	// LLM returned something unexpected; fall back to regex
	log.Printf("[fleet-router] LLM returned unexpected value %q, falling back to regex", raw)
	return fallbackRoute(msg, fleetCfg)
}

// fallbackRoute uses regex-based mention parsing as a fallback when LLM routing fails.
// It returns the last valid agent mention, or "none" if no valid mentions are found.
func fallbackRoute(msg Message, fleetCfg *FleetConfig) RoutingResult {
	mentions := msg.Mentions
	if len(mentions) == 0 {
		mentions = ParseMentions(msg.Text)
	}

	// Check for @human mention
	for _, m := range mentions {
		if m == "human" {
			return RoutingResult{Target: "human", Reason: "fallback: @human mentioned"}
		}
	}

	// Return the last valid agent mention (last mention is typically the handoff target)
	for i := len(mentions) - 1; i >= 0; i-- {
		if fleetCfg.CanTalkTo(msg.Sender, mentions[i]) {
			return RoutingResult{
				Target: mentions[i],
				Reason: fmt.Sprintf("fallback: last valid mention @%s", mentions[i]),
			}
		}
	}

	return RoutingResult{Target: "none", Reason: "fallback: no valid routing signal"}
}
