package fleet

import (
	"sort"
	"strings"
	"time"
)

// Message represents a single message on the fleet communication channel.
// Messages are the fundamental unit of communication between agents and the customer.
type Message struct {
	ID        string            `json:"id"`
	Sender    string            `json:"sender"`               // "customer", or agent key (e.g., "po", "architect")
	Text      string            `json:"text"`                 // Message content
	ThreadKey string            `json:"thread_key,omitempty"` // Pairwise conversation thread (e.g., "dev+po"); empty = system/global
	Artifacts map[string]string `json:"artifacts,omitempty"`  // name -> file path (produced artifacts)
	Mentions  []string          `json:"mentions,omitempty"`   // Parsed @mentions from text
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]any    `json:"metadata,omitempty"` // Additional context (e.g., tool calls made)
}

// MakeThreadKey returns a canonical pairwise thread key for two participants.
// The key is deterministic: participants are sorted alphabetically and joined
// with "+". For example, MakeThreadKey("po", "dev") returns "dev+po".
//
// This ensures that both directions of a conversation (A→B and B→A) map to
// the same thread, regardless of who initiates.
func MakeThreadKey(a, b string) string {
	pair := []string{a, b}
	sort.Strings(pair)
	return strings.Join(pair, "+")
}

// IsFromCustomer returns true if the message was sent by the customer.
func (m *Message) IsFromCustomer() bool {
	return m.Sender == "customer"
}

// IsFromAgent returns true if the message was sent by an agent (not customer, not system).
func (m *Message) IsFromAgent() bool {
	return m.Sender != "customer" && m.Sender != "system"
}

// IsSystem returns true if the message is a system message (e.g., nudge, error).
func (m *Message) IsSystem() bool {
	return m.Sender == "system"
}

// MentionsCustomer returns true if the message contains an @customer mention.
func (m *Message) MentionsCustomer() bool {
	for _, mention := range m.Mentions {
		if mention == "customer" {
			return true
		}
	}
	return false
}

// MentionsAgent returns the first non-customer agent mentioned, or empty string.
func (m *Message) MentionsAgent() string {
	for _, mention := range m.Mentions {
		if mention != "customer" {
			return mention
		}
	}
	return ""
}
