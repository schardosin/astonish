package fleet

import (
	"context"
	"fmt"
	"log/slog"
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

// RouteCustomerMessage determines which agent should handle a customer message.
// This is a fast path that doesn't need an LLM call.
//
// If an agent was waiting for customer input, the message goes to that agent.
// Otherwise it goes to the entry point agent.
func RouteCustomerMessage(fleetCfg *FleetConfig, waitingAgent string) string {
	if waitingAgent != "" {
		return waitingAgent
	}
	return fleetCfg.GetEntryPoint()
}

// RoutingResult holds the outcome of an LLM routing decision.
type RoutingResult struct {
	Target string // agent key, "customer", "self", or "none"
	Reason string // brief explanation (for logging)
}

// routingTimeout is the max time allowed for a routing LLM call.
const routingTimeout = 15 * time.Second

// RouteWithLLM uses the LLM to determine who should act next after an agent
// posts a message. This handles all the nuance of natural conversation:
// acknowledgments, handoffs, multi-mentions, FYIs, etc.
//
// Returns a RoutingResult with Target set to one of:
//   - An agent key (e.g., "dev") meaning that agent should be activated next
//   - "customer" meaning the system should wait for customer input
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
			slog.Error("llm routing error, falling back to regex", "component", "fleet-router", "error", err)
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
	result := parseRoutingResponse(raw, senderKey, talksTo, msg, fleetCfg)

	// Post-processing: when the LLM says "customer" or "none" but the message
	// mentions agents outside the sender's talksTo, re-route through an
	// intermediary. This handles two common patterns:
	//
	// 1. "customer" case: An agent (e.g., PO) tries to hand off directly to a
	//    downstream agent (e.g., @dev) that isn't in their talksTo. Without
	//    this, the session stalls waiting for a customer that will never come.
	//
	// 2. "none" case: An agent (e.g., architect) declares their role is done
	//    but mentions an unreachable agent (e.g., @qa) as a handoff or FYI.
	//    The LLM interprets "I'm done" as "no action needed" and returns
	//    "none", but the @mention signals the conversation should continue.
	if result.Target == "customer" || result.Target == "none" {
		if reroute := rerouteViaIntermediary(msg, senderKey, fleetCfg); reroute != nil {
			return *reroute
		}
	}

	return result
}

// buildRoutingPrompt constructs the prompt for the routing LLM call.
func buildRoutingPrompt(sender, messageText string, talksTo []string) string {
	// Build valid targets list
	var targets []string
	for _, t := range talksTo {
		targets = append(targets, t)
	}

	var sb strings.Builder
	sb.WriteString("You are a message router for a team conversation. ")
	sb.WriteString("Given a message from a team member, determine who should act next.\n\n")

	sb.WriteString(fmt.Sprintf("Message from: @%s\n", sender))
	sb.WriteString(fmt.Sprintf("Message:\n\"\"\"\n%s\n\"\"\"\n\n", messageText))

	sb.WriteString(fmt.Sprintf("Valid targets that @%s can route to: %s\n", sender, strings.Join(targets, ", ")))
	sb.WriteString("Special values: \"customer\" (wait for customer input), \"self\" (sender still has the action), \"none\" (no one needs to act)\n\n")

	sb.WriteString("Determine who should act next. Consider:\n")
	sb.WriteString("- If the sender is handing off work or asking someone to do something, that person has the action.\n")
	sb.WriteString("- If the sender addresses multiple people, focus on who is being asked to TAKE ACTION, not who is just being informed.\n")
	sb.WriteString("- If the sender is acknowledging, thanking, or giving an FYI without requesting action, they likely still have the action (return \"self\").\n")
	sb.WriteString("- If the sender asks @customer a question or presents something for customer review, return \"customer\".\n")
	sb.WriteString("- If the sender says they are completely done and no one else needs to act, return \"none\".\n\n")

	sb.WriteString("Respond with ONLY a single word: one of the valid targets, \"customer\", \"self\", or \"none\".")

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
	case "customer":
		return RoutingResult{Target: "customer", Reason: "LLM decided: wait for customer"}
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
	slog.Warn("llm returned unexpected routing value, falling back to regex", "component", "fleet-router", "value", raw)
	return fallbackRoute(msg, fleetCfg)
}

// fallbackRoute uses regex-based mention parsing as a fallback when LLM routing fails.
// It returns the last valid agent mention, or "none" if no valid mentions are found.
func fallbackRoute(msg Message, fleetCfg *FleetConfig) RoutingResult {
	mentions := msg.Mentions
	if len(mentions) == 0 {
		mentions = ParseMentions(msg.Text)
	}

	// Check for @customer mention
	for _, m := range mentions {
		if m == "customer" {
			return RoutingResult{Target: "customer", Reason: "fallback: @customer mentioned"}
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

// rerouteViaIntermediary checks whether a message mentions agents that the
// sender cannot directly reach, and if so, finds an intermediary in the
// sender's talksTo list that CAN reach the mentioned agent.
//
// This handles a common pattern: the PO reviews work and says
// "@architect, great work. @dev, please implement this." The PO's talksTo
// is [customer, architect], so @dev is unreachable. Without re-routing, the
// router returns "customer" and the session stalls forever. With re-routing,
// we detect that @architect can reach @dev and route there instead.
//
// Returns nil if no re-route is needed (no unreachable mentions found).
func rerouteViaIntermediary(msg Message, senderKey string, fleetCfg *FleetConfig) *RoutingResult {
	mentions := msg.Mentions
	if len(mentions) == 0 {
		mentions = ParseMentions(msg.Text)
	}
	if len(mentions) == 0 {
		return nil
	}

	senderTargets := fleetCfg.GetTalksTo(senderKey)
	senderTargetSet := make(map[string]bool, len(senderTargets))
	for _, t := range senderTargets {
		senderTargetSet[t] = true
	}

	// Collect agents mentioned in the message that:
	// (a) are valid fleet agents (exist in the config)
	// (b) are NOT in the sender's talksTo (unreachable directly)
	// (c) are not the sender themselves
	// (d) are not "customer" (handled separately)
	var unreachable []string
	for _, m := range mentions {
		if m == senderKey || m == "customer" {
			continue
		}
		if _, isAgent := fleetCfg.Agents[m]; !isAgent {
			continue
		}
		if senderTargetSet[m] {
			continue // sender can already reach this agent
		}
		unreachable = append(unreachable, m)
	}

	if len(unreachable) == 0 {
		return nil
	}

	// For each unreachable agent, check if any of the sender's talksTo
	// targets can reach them (one-hop lookup). Pick the first intermediary
	// found, prioritizing targets earlier in the talksTo list (which
	// typically follows the communication flow order).
	for _, target := range unreachable {
		for _, intermediary := range senderTargets {
			if intermediary == "customer" {
				continue
			}
			if fleetCfg.CanTalkTo(intermediary, target) {
				slog.Info("re-routing via intermediary", "component", "fleet-router", "sender", senderKey, "target", target, "intermediary", intermediary)
				return &RoutingResult{
					Target: intermediary,
					Reason: fmt.Sprintf("re-route: @%s mentioned unreachable @%s, routing via @%s",
						senderKey, target, intermediary),
				}
			}
		}
	}

	// No intermediary found (target is more than one hop away).
	// Fall through to the original routing decision.
	return nil
}
