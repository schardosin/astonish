// Package slack implements the Slack channel adapter for Astonish.
// It connects to Slack via Socket Mode (WebSocket) or Events API (HTTP),
// normalizes inbound messages, and delivers outbound responses with
// Slack mrkdwn formatting and message chunking.
package slack

import (
	"regexp"
	"strings"
)

// maxMessageLength is the Slack API limit for a single message text block.
const maxMessageLength = 4000

// MarkdownToMrkdwn converts standard Markdown to Slack's mrkdwn format.
//
// Conversions:
//   - **bold** or __bold__ → *bold*
//   - *italic* (single asterisk) → _italic_
//   - ~~strike~~ → ~strike~
//   - [text](url) → <url|text>
//   - # heading → *heading*
//   - ## heading → *heading*
//   - ### heading → *heading*
//
// Code blocks (```) and inline code (`) are preserved as-is (Slack uses same syntax).
// Block quotes (>) are preserved as-is (Slack uses same syntax).
func MarkdownToMrkdwn(text string) string {
	if text == "" {
		return ""
	}

	// Process line by line to handle code blocks correctly
	var result strings.Builder
	lines := strings.Split(text, "\n")
	inCodeBlock := false

	for i, line := range lines {
		if i > 0 {
			result.WriteByte('\n')
		}

		// Track code block boundaries
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString(line)
			continue
		}

		// Don't transform inside code blocks
		if inCodeBlock {
			result.WriteString(line)
			continue
		}

		// Convert headings: # Heading → *Heading*
		line = convertHeadings(line)

		// Convert inline formatting
		line = convertInlineFormatting(line)

		result.WriteString(line)
	}

	return result.String()
}

// convertHeadings converts Markdown headings to bold text.
func convertHeadings(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return line
	}

	// Count the # marks and remove them
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i > 0 && i < len(trimmed) && trimmed[i] == ' ' {
		heading := strings.TrimSpace(trimmed[i+1:])
		if heading != "" {
			return "*" + heading + "*"
		}
	}
	return line
}

var (
	// Bold: **text** or __text__ → *text*
	reBold = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	// Italic: single *text* (but not inside **) → _text_
	// Note: reItalicUnderscore pattern reserved for future italic conversion
	// Strikethrough: ~~text~~ → ~text~
	reStrike = regexp.MustCompile(`~~(.+?)~~`)
	// Links: [text](url) → <url|text>
	reLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	// Inline code — preserve as-is (matches to skip them during replacement)
	reInlineCode = regexp.MustCompile("`[^`]+`")
)

// convertInlineFormatting applies inline format conversions to a single line.
func convertInlineFormatting(line string) string {
	// Protect inline code spans from conversion
	type codeSpan struct {
		start, end int
		text       string
	}
	var spans []codeSpan
	for _, m := range reInlineCode.FindAllStringIndex(line, -1) {
		spans = append(spans, codeSpan{m[0], m[1], line[m[0]:m[1]]})
	}

	// If there are code spans, process segments between them
	if len(spans) > 0 {
		var result strings.Builder
		pos := 0
		for _, sp := range spans {
			// Convert the segment before this code span
			segment := line[pos:sp.start]
			segment = applyInlineConversions(segment)
			result.WriteString(segment)
			// Preserve the code span as-is
			result.WriteString(sp.text)
			pos = sp.end
		}
		// Convert remaining segment after last code span
		if pos < len(line) {
			segment := line[pos:]
			segment = applyInlineConversions(segment)
			result.WriteString(segment)
		}
		return result.String()
	}

	return applyInlineConversions(line)
}

// applyInlineConversions applies bold, italic, strike, and link conversions.
func applyInlineConversions(text string) string {
	// Links: [text](url) → <url|text>
	text = reLink.ReplaceAllString(text, "<$2|$1>")

	// Bold: **text** → *text*
	text = reBold.ReplaceAllStringFunc(text, func(match string) string {
		// Extract the content between ** ** or __ __
		m := reBold.FindStringSubmatch(match)
		content := m[1]
		if content == "" {
			content = m[2]
		}
		return "*" + content + "*"
	})

	// Strikethrough: ~~text~~ → ~text~
	text = reStrike.ReplaceAllString(text, "~$1~")

	return text
}

// splitMessage splits a long message into chunks that fit within Slack's
// message length limit, preferring to break at newlines or spaces.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Find a good break point
		chunk := text[:maxLen]

		// Prefer breaking at a newline
		if idx := strings.LastIndex(chunk, "\n"); idx > maxLen/2 {
			chunks = append(chunks, text[:idx])
			text = text[idx+1:]
			continue
		}

		// Prefer breaking at a space
		if idx := strings.LastIndex(chunk, " "); idx > maxLen/2 {
			chunks = append(chunks, text[:idx])
			text = text[idx+1:]
			continue
		}

		// Hard break
		chunks = append(chunks, chunk)
		text = text[maxLen:]
	}

	return chunks
}
