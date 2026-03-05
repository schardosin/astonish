package fleet

import (
	"time"
)

// Message represents a single message on the fleet communication channel.
// Messages are the fundamental unit of communication between agents and the human.
type Message struct {
	ID        string            `json:"id"`
	Sender    string            `json:"sender"`              // "human", or agent key (e.g., "po", "architect")
	Text      string            `json:"text"`                // Message content
	Artifacts map[string]string `json:"artifacts,omitempty"` // name -> file path (produced artifacts)
	Mentions  []string          `json:"mentions,omitempty"`  // Parsed @mentions from text
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]any    `json:"metadata,omitempty"` // Additional context (e.g., tool calls made)
}

// IsFromHuman returns true if the message was sent by the human.
func (m *Message) IsFromHuman() bool {
	return m.Sender == "human"
}

// IsFromAgent returns true if the message was sent by an agent (not human, not system).
func (m *Message) IsFromAgent() bool {
	return m.Sender != "human" && m.Sender != "system"
}

// IsSystem returns true if the message is a system message (e.g., nudge, error).
func (m *Message) IsSystem() bool {
	return m.Sender == "system"
}

// MentionsHuman returns true if the message contains an @human mention.
func (m *Message) MentionsHuman() bool {
	for _, mention := range m.Mentions {
		if mention == "human" {
			return true
		}
	}
	return false
}

// MentionsAgent returns the first non-human agent mentioned, or empty string.
func (m *Message) MentionsAgent() string {
	for _, mention := range m.Mentions {
		if mention != "human" {
			return mention
		}
	}
	return ""
}
