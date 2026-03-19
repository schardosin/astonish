package fleet

import (
	"sort"
	"strings"
	"time"
)

// Message represents a single message on the fleet communication channel.
// Messages are the fundamental unit of communication between agents and the customer.
type Message struct {
	ID         string            `json:"id"`
	Sender     string            `json:"sender"`                // "customer", or agent key (e.g., "po", "architect")
	Text       string            `json:"text"`                  // Message content
	ThreadKey  string            `json:"thread_key,omitempty"`  // DEPRECATED: pairwise key (kept for backward compat with old transcripts)
	MemoryKeys []string          `json:"memory_keys,omitempty"` // Agents whose memory contains this message (e.g., ["po","architect"])
	Artifacts  map[string]string `json:"artifacts,omitempty"`   // name -> file path (produced artifacts)
	Mentions   []string          `json:"mentions,omitempty"`    // Parsed @mentions from text
	Timestamp  time.Time         `json:"timestamp"`
	Metadata   map[string]any    `json:"metadata,omitempty"` // Additional context (e.g., tool calls made)
}

// MakeThreadKey returns a canonical pairwise thread key for two participants.
// DEPRECATED: Use MemoryKeys instead. Retained for backward compatibility with
// old transcripts and the migration path from pairwise to per-agent memory.
func MakeThreadKey(a, b string) string {
	pair := []string{a, b}
	sort.Strings(pair)
	return strings.Join(pair, "+")
}

// ResolveMemoryKeys returns the effective memory keys for a message.
// For new messages, MemoryKeys is authoritative. For old messages from
// before the per-agent memory model, it derives keys from the deprecated
// ThreadKey field (splitting on "+"). System messages (empty both fields)
// return nil — they are visible to all agents.
func (m *Message) ResolveMemoryKeys() []string {
	if len(m.MemoryKeys) > 0 {
		return m.MemoryKeys
	}
	if m.ThreadKey != "" {
		return strings.Split(m.ThreadKey, "+")
	}
	return nil // system/global — visible to all
}

// InAgentMemory returns true if this message belongs in the given agent's
// memory. A message is in an agent's memory if:
//   - The agent appears in MemoryKeys (or the derived ThreadKey split), OR
//   - The message has no memory keys at all (system/global message)
func (m *Message) InAgentMemory(agentKey string) bool {
	keys := m.ResolveMemoryKeys()
	if len(keys) == 0 {
		return true // system/global — visible to all
	}
	for _, k := range keys {
		if k == agentKey {
			return true
		}
	}
	return false
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
