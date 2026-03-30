package agent

import (
	"regexp"
	"strings"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// thinkTagPattern matches <think>...</think> and <thinking>...</thinking> blocks
// (including content spanning multiple lines). Used for non-streaming contexts
// (e.g., trace reconstruction) where the full text is available at once.
var thinkTagPattern = regexp.MustCompile(`(?s)<(?:think|thinking)>.*?</(?:think|thinking)>`)

// openThinkTags lists the opening tags we recognise as chain-of-thought markers.
var openThinkTags = []string{"<think>", "<thinking>"}

// closeThinkTags lists the corresponding closing tags, same index as openThinkTags.
var closeThinkTags = []string{"</think>", "</thinking>"}

// thinkTagFilter is a stateful streaming filter that strips <think>/<thinking>
// blocks from LLM text that arrives in small chunks (one event per token group).
//
// A regex cannot work here because a single <think>…</think> block is typically
// split across dozens of streaming events.  Instead we track whether we are
// currently "inside" a think block and buffer partial tag matches so that no
// tag fragment leaks into the output.
type thinkTagFilter struct {
	inside bool   // true while we are between an open and close tag
	buf    string // buffered bytes that *might* be the start of a tag
}

// Feed processes a chunk of streamed text and returns the portion that should
// be shown to the user (empty string means the chunk was suppressed).
// The second return value is true when the filter consumed or suppressed any
// think-tag content during this call, which lets the caller distinguish
// whitespace remnants of stripping from legitimate whitespace.
func (f *thinkTagFilter) Feed(chunk string) (string, bool) {
	var out strings.Builder
	stripped := false
	// Prepend anything buffered from the previous chunk.
	input := f.buf + chunk
	f.buf = ""

	for len(input) > 0 {
		if f.inside {
			stripped = true // suppressing content inside a think block
			// We are inside a think block — look for a closing tag.
			idx := f.indexOfAnyClose(input)
			if idx == -1 {
				// No closing tag found yet.  Check if the tail of input
				// could be the start of a closing tag (e.g. "</thi").
				prefixLen := f.longestClosingPrefix(input)
				if prefixLen > 0 {
					f.buf = input[len(input)-prefixLen:]
				}
				// Everything is suppressed (still inside).
				return out.String(), stripped
			}
			// Found a closing tag — skip past it.
			closeTag := f.matchingCloseAt(input, idx)
			input = input[idx+len(closeTag):]
			f.inside = false
			continue
		}

		// We are outside a think block — look for an opening tag.
		idx, tag := f.indexOfAnyOpen(input)
		if idx == -1 {
			// No opening tag found.  But the tail might be a partial
			// opening tag (e.g., "<thin"), so buffer that part.
			prefixLen := f.longestOpeningPrefix(input)
			if prefixLen > 0 {
				out.WriteString(input[:len(input)-prefixLen])
				f.buf = input[len(input)-prefixLen:]
			} else {
				out.WriteString(input)
			}
			return out.String(), stripped
		}
		// Emit everything before the opening tag.
		out.WriteString(input[:idx])
		input = input[idx+len(tag):]
		f.inside = true
		stripped = true
	}
	return out.String(), stripped
}

// indexOfAnyClose returns the byte index in s where any closing tag starts, or -1.
func (f *thinkTagFilter) indexOfAnyClose(s string) int {
	best := -1
	for _, tag := range closeThinkTags {
		if i := strings.Index(s, tag); i != -1 && (best == -1 || i < best) {
			best = i
		}
	}
	return best
}

// matchingCloseAt returns which closing tag starts at s[idx:].
func (f *thinkTagFilter) matchingCloseAt(s string, idx int) string {
	for _, tag := range closeThinkTags {
		if strings.HasPrefix(s[idx:], tag) {
			return tag
		}
	}
	return closeThinkTags[0] // fallback
}

// indexOfAnyOpen returns the byte index and the matching opening tag, or -1.
func (f *thinkTagFilter) indexOfAnyOpen(s string) (int, string) {
	bestIdx := -1
	bestTag := ""
	for _, tag := range openThinkTags {
		if i := strings.Index(s, tag); i != -1 && (bestIdx == -1 || i < bestIdx) {
			bestIdx = i
			bestTag = tag
		}
	}
	return bestIdx, bestTag
}

// longestOpeningPrefix returns the length of the longest suffix of s that
// is a proper prefix of one of the opening tags (e.g., "<thi" is a prefix
// of "<think>").  Returns 0 if no suffix matches.
func (f *thinkTagFilter) longestOpeningPrefix(s string) int {
	// The longest opening tag is "<thinking>" (10 chars), so we only need to
	// check the last 9 chars at most (a proper prefix is shorter than the tag).
	maxCheck := 9
	if len(s) < maxCheck {
		maxCheck = len(s)
	}
	for l := maxCheck; l >= 1; l-- {
		suffix := s[len(s)-l:]
		for _, tag := range openThinkTags {
			if strings.HasPrefix(tag, suffix) {
				return l
			}
		}
	}
	return 0
}

// longestClosingPrefix returns the length of the longest suffix of s that
// is a proper prefix of one of the closing tags.
func (f *thinkTagFilter) longestClosingPrefix(s string) int {
	maxCheck := 11 // "</thinking>" is 12 chars; proper prefix is at most 11
	if len(s) < maxCheck {
		maxCheck = len(s)
	}
	for l := maxCheck; l >= 1; l-- {
		suffix := s[len(s)-l:]
		for _, tag := range closeThinkTags {
			if strings.HasPrefix(tag, suffix) {
				return l
			}
		}
	}
	return 0
}

// filterEventThinkContent uses the streaming filter to strip think-tag content
// and also drops parts flagged with the structured Thought field.
func filterEventThinkContent(f *thinkTagFilter, event *session.Event) {
	if event == nil {
		return
	}
	content := event.LLMResponse.Content
	if content == nil {
		return
	}
	cleaned := make([]*genai.Part, 0, len(content.Parts))
	for _, part := range content.Parts {
		// Drop parts flagged as chain-of-thought by the provider.
		if part.Thought {
			continue
		}
		if part.Text != "" {
			filtered, stripped := f.Feed(part.Text)
			if filtered == "" {
				continue
			}
			// Only drop whitespace-only remnants when the filter actually
			// stripped think-tag content from this chunk.  Legitimate
			// whitespace (e.g., "\n\n" between markdown sections) must
			// pass through to preserve formatting.
			if stripped && strings.TrimSpace(filtered) == "" {
				continue
			}
			part = &genai.Part{
				Text:                filtered,
				InlineData:          part.InlineData,
				FileData:            part.FileData,
				FunctionCall:        part.FunctionCall,
				FunctionResponse:    part.FunctionResponse,
				ExecutableCode:      part.ExecutableCode,
				CodeExecutionResult: part.CodeExecutionResult,
			}
		}
		cleaned = append(cleaned, part)
	}
	event.LLMResponse.Content = &genai.Content{
		Parts: cleaned,
		Role:  content.Role,
	}
}
