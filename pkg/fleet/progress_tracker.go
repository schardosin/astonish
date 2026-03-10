package fleet

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProgressTracker maintains a structured record of key session milestones
// (approvals, completions, handoffs) that persists across session recoveries.
//
// The problem it solves: after a daemon restart, the conversation thread may
// contain hundreds of messages that get truncated in the agent's context window.
// Critical decisions (approvals, completed work, handoff instructions) get
// buried in 200-char summaries and agents re-do work or re-request approvals.
//
// The tracker is injected into every agent's system prompt, separate from the
// thread context, so agents always see the current project state regardless
// of how long the thread has grown.
type ProgressTracker struct {
	mu         sync.RWMutex
	milestones []Milestone
}

// Milestone represents a significant event in the session lifecycle.
type Milestone struct {
	Type      MilestoneType `json:"type"`
	Agent     string        `json:"agent"`   // Who triggered it
	Summary   string        `json:"summary"` // Human-readable description
	Timestamp time.Time     `json:"timestamp"`
	Details   string        `json:"details,omitempty"` // Optional extra context
}

// MilestoneType categorizes milestones.
type MilestoneType string

const (
	MilestoneApproval   MilestoneType = "approval"   // Human approved a deliverable
	MilestoneHandoff    MilestoneType = "handoff"    // Work handed from one agent to another
	MilestoneDelivery   MilestoneType = "delivery"   // Agent produced a deliverable
	MilestoneCompletion MilestoneType = "completion" // A phase or task completed
	MilestoneError      MilestoneType = "error"      // Session error/interruption
	MilestoneResume     MilestoneType = "resume"     // Session resumed after restart
)

// NewProgressTracker creates an empty progress tracker.
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{}
}

// AddMilestone records a new milestone.
func (pt *ProgressTracker) AddMilestone(m Milestone) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}
	pt.milestones = append(pt.milestones, m)
}

// GetMilestones returns a copy of all milestones.
func (pt *ProgressTracker) GetMilestones() []Milestone {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	result := make([]Milestone, len(pt.milestones))
	copy(result, pt.milestones)
	return result
}

// FormatForPrompt produces a structured text summary of the session progress
// suitable for injection into an agent's system prompt. Returns empty string
// if there are no milestones.
func (pt *ProgressTracker) FormatForPrompt() string {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if len(pt.milestones) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Session Progress (DO NOT REDO COMPLETED WORK)\n\n")
	sb.WriteString("The following milestones have been recorded in this session.\n")
	sb.WriteString("This is authoritative. Do NOT re-request approvals that are already granted.\n")
	sb.WriteString("Do NOT re-create deliverables that are already completed.\n")
	sb.WriteString("Pick up from where the last milestone indicates.\n\n")

	for _, m := range pt.milestones {
		icon := milestoneIcon(m.Type)
		sb.WriteString(fmt.Sprintf("- %s **[%s]** @%s: %s", icon, m.Type, m.Agent, m.Summary))
		if m.Details != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", m.Details))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func milestoneIcon(t MilestoneType) string {
	switch t {
	case MilestoneApproval:
		return "APPROVED"
	case MilestoneHandoff:
		return "HANDOFF"
	case MilestoneDelivery:
		return "DELIVERED"
	case MilestoneCompletion:
		return "COMPLETED"
	case MilestoneError:
		return "ERROR"
	case MilestoneResume:
		return "RESUMED"
	default:
		return "EVENT"
	}
}

// AnalyzeMessageForMilestones examines an agent message and extracts any
// milestone events from it. This is called after each agent activation to
// automatically track progress.
//
// The detection is intentionally conservative (high-precision, low-recall)
// to avoid false positives. It looks for strong signals:
// - "@customer" + "approved"/"approval" in a message from PO (approval request answered)
// - Handoff patterns: agent addressing another agent with action verbs
// - Completion markers: "all tests pass", "implementation complete", etc.
func AnalyzeMessageForMilestones(msg Message) []Milestone {
	var milestones []Milestone

	text := strings.ToLower(msg.Text)
	sender := msg.Sender

	// Skip system messages and very short messages
	if sender == "system" || sender == "customer" || len(text) < 20 {
		return nil
	}

	// Detect approvals (human approved something through the agent's confirmation)
	if containsApprovalSignal(text) {
		milestones = append(milestones, Milestone{
			Type:      MilestoneApproval,
			Agent:     sender,
			Summary:   extractApprovalSummary(msg.Text),
			Timestamp: msg.Timestamp,
		})
	}

	// Detect deliveries to human
	if containsDeliverySignal(text, msg.Mentions) {
		milestones = append(milestones, Milestone{
			Type:      MilestoneDelivery,
			Agent:     sender,
			Summary:   extractDeliverySummary(msg.Text),
			Timestamp: msg.Timestamp,
		})
	}

	// Detect handoffs between agents
	if handoff := detectHandoff(msg); handoff != nil {
		milestones = append(milestones, *handoff)
	}

	// Detect completion markers
	if containsCompletionSignal(text) {
		milestones = append(milestones, Milestone{
			Type:      MilestoneCompletion,
			Agent:     sender,
			Summary:   extractCompletionSummary(msg.Text),
			Timestamp: msg.Timestamp,
		})
	}

	return milestones
}

// containsApprovalSignal checks if the message indicates an approval was granted.
func containsApprovalSignal(text string) bool {
	approvalPhrases := []string{
		"approved",
		"approval granted",
		"qa approved",
		"customer approved",
		"both documents are approved",
		"requirements are approved",
		"architecture is approved",
		"thank you for the approval",
		"please proceed",
	}
	for _, phrase := range approvalPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// containsDeliverySignal checks if the message presents something to @customer.
func containsDeliverySignal(text string, mentions []string) bool {
	hasCustomerMention := false
	for _, m := range mentions {
		if m == "customer" {
			hasCustomerMention = true
			break
		}
	}
	if !hasCustomerMention {
		return false
	}

	deliveryPhrases := []string{
		"here's the summary",
		"here is the summary",
		"i've written",
		"i've completed",
		"i've updated",
		"here's what",
		"for your review",
		"for approval",
		"presenting",
		"deliverable",
	}
	for _, phrase := range deliveryPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// containsCompletionSignal checks if the message indicates work completion.
func containsCompletionSignal(text string) bool {
	completionPhrases := []string{
		"all tests pass",
		"implementation complete",
		"all steps done",
		"all 15 steps done",
		"all defects resolved",
		"0 failures",
		"qa approved",
		"committed and pushed",
	}
	for _, phrase := range completionPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// detectHandoff checks if the message is handing work to another agent.
func detectHandoff(msg Message) *Milestone {
	text := strings.ToLower(msg.Text)

	// Look for patterns like "@dev, please implement" or "handing off to @architect"
	handoffPhrases := []string{
		"handing off to",
		"hand this off to",
		"please implement",
		"please start",
		"please proceed with",
		"your turn",
		"over to you",
	}

	for _, phrase := range handoffPhrases {
		if strings.Contains(text, phrase) {
			// Find which agent is being handed to
			for _, mention := range msg.Mentions {
				if mention != "customer" && mention != msg.Sender {
					return &Milestone{
						Type:      MilestoneHandoff,
						Agent:     msg.Sender,
						Summary:   fmt.Sprintf("Handed off to @%s", mention),
						Timestamp: msg.Timestamp,
						Details:   extractHandoffContext(msg.Text),
					}
				}
			}
			break
		}
	}

	return nil
}

// extractApprovalSummary extracts a brief description of what was approved.
func extractApprovalSummary(text string) string {
	// Take the first 150 chars or first sentence, whichever is shorter
	return truncateToSentence(text, 150)
}

// extractDeliverySummary extracts a brief description of what was delivered.
func extractDeliverySummary(text string) string {
	return truncateToSentence(text, 150)
}

// extractCompletionSummary extracts a brief description of what was completed.
func extractCompletionSummary(text string) string {
	return truncateToSentence(text, 150)
}

// extractHandoffContext extracts brief context about the handoff.
func extractHandoffContext(text string) string {
	return truncateToSentence(text, 100)
}

// truncateToSentence returns the first sentence of text, up to maxLen chars.
func truncateToSentence(text string, maxLen int) string {
	// Collapse whitespace
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxLen {
		return text
	}

	// Try to break at a sentence boundary within maxLen
	cutoff := text[:maxLen]
	for _, sep := range []string{". ", "! ", "? "} {
		if idx := strings.LastIndex(cutoff, sep); idx > 30 {
			return cutoff[:idx+1]
		}
	}

	return cutoff + "..."
}

// AnalyzeCustomerMessageForMilestones checks if a customer message grants approval.
// Customer messages are special because they represent customer decisions.
func AnalyzeCustomerMessageForMilestones(msg Message) []Milestone {
	text := strings.ToLower(msg.Text)

	approvalSignals := []string{
		"approved",
		"approve",
		"lgtm",
		"looks good",
		"please proceed",
		"go ahead",
		"ship it",
		"merge it",
		"accepted",
	}

	for _, signal := range approvalSignals {
		if strings.Contains(text, signal) {
			return []Milestone{{
				Type:      MilestoneApproval,
				Agent:     "customer",
				Summary:   truncateToSentence(msg.Text, 150),
				Timestamp: msg.Timestamp,
			}}
		}
	}

	return nil
}
