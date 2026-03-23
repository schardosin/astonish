package agent

import (
	"fmt"
	"regexp"
	"time"

	"google.golang.org/genai"
)

// userTimestampRe matches the timestamp prefix injected by NewTimestampedUserContent.
// Format: "[2026-03-20 14:30:05 UTC]\n"
var userTimestampRe = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \w+\]\n`)

// NewTimestampedUserContent creates a user message with an absolute timestamp
// prepended. The timestamp is baked in at creation time and never changes,
// keeping the conversation history stable for provider KV-cache reuse.
//
// Format: [2026-03-20 14:30:05 UTC]\n<text>
//
// Because every provider (Anthropic, OpenAI, Gemini) caches based on a
// prefix match of the message array, the key requirements are:
//   - The system prompt must be 100% static (no per-turn date/time).
//   - Historical messages must be immutable once created.
//
// Placing the timestamp in each user message satisfies both: the system
// prompt never changes, and each historical message — including its
// timestamp — is frozen as part of the cacheable prefix.
func NewTimestampedUserContent(text string) *genai.Content {
	ts := time.Now().UTC().Format("2006-01-02 15:04:05 MST")
	stamped := fmt.Sprintf("[%s]\n%s", ts, text)
	return genai.NewContentFromText(stamped, genai.RoleUser)
}

// StripTimestamp removes the timestamp prefix injected by NewTimestampedUserContent.
// Use this when the flow engine needs the raw user text for programmatic purposes
// (state storage, approval checks) rather than passing it to the LLM.
func StripTimestamp(text string) string {
	return userTimestampRe.ReplaceAllString(text, "")
}
