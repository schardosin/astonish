package email

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	fencedCodeBlockRe  = regexp.MustCompile("(?s)```(\\w*)\\n?(.*?)```")
	inlineCodeRe       = regexp.MustCompile("`([^`]+)`")
	boldRe             = regexp.MustCompile(`\*\*(.+?)\*\*`)
	starItalicRe       = regexp.MustCompile(`(?:^|[^*<])\*([^*<>\n]+?)\*`)
	underscoreItalicRe = regexp.MustCompile(`(?:^|[^\w])_([^_]+?)_(?:[^\w]|$)`)
	headingLineRe      = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	unorderedListRe    = regexp.MustCompile(`^[-*]\s+(.+)$`)
	orderedListRe      = regexp.MustCompile(`^\d+\.\s+(.+)$`)
	tableRowLineRe     = regexp.MustCompile(`^\|(.+)\|$`)
	excessBlankRe      = regexp.MustCompile(`\n{3,}`)
)

const (
	tableStyle = `border-collapse:collapse;width:100%;margin:12px 0;`
	thStyle    = `border:1px solid #ddd;padding:8px 10px;text-align:left;background:#f5f5f5;font-weight:600;`
	tdStyle    = `border:1px solid #ddd;padding:8px 10px;text-align:left;vertical-align:top;`
	preStyle   = `background:#f6f8fa;padding:12px;border-radius:4px;overflow-x:auto;font-size:13px;`
	codeStyle  = `background:#f6f8fa;padding:1px 4px;border-radius:3px;font-size:0.9em;`
)

// MarkdownToHTML converts markdown to HTML suitable for email clients.
// Unlike telegram.MarkdownToHTML, this emits real tables, headings, and lists.
// Raw HTML in the input is escaped; only generated tags are trusted.
func MarkdownToHTML(text string) string {
	if text == "" {
		return ""
	}

	// Protect fenced code blocks first so block parsing does not see their lines.
	var codeBlocks []string
	text = fencedCodeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := fencedCodeBlockRe.FindStringSubmatch(match)
		lang := parts[1]
		code := strings.TrimRight(parts[2], "\n")
		escaped := html.EscapeString(code)
		var block string
		if lang != "" {
			block = fmt.Sprintf(
				`<pre style="%s"><code class="language-%s">%s</code></pre>`,
				preStyle, html.EscapeString(lang), escaped,
			)
		} else {
			block = fmt.Sprintf(`<pre style="%s">%s</pre>`, preStyle, escaped)
		}
		placeholder := fmt.Sprintf("\x00CODE%d\x00", len(codeBlocks))
		codeBlocks = append(codeBlocks, block)
		return placeholder
	})

	lines := strings.Split(text, "\n")
	var out strings.Builder
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Restored code-block placeholder on its own line.
		if strings.HasPrefix(trimmed, "\x00CODE") && strings.HasSuffix(trimmed, "\x00") {
			writeBlock(&out, trimmed)
			i++
			continue
		}

		if m := headingLineRe.FindStringSubmatch(trimmed); m != nil {
			level := len(m[1])
			if level > 6 {
				level = 6
			}
			fmt.Fprintf(&out, "<h%d style=\"margin:16px 0 8px;line-height:1.3;\">%s</h%d>\n",
				level, formatInline(m[2]), level)
			i++
			continue
		}

		if tableRowLineRe.MatchString(trimmed) {
			var rows [][]string
			for i < len(lines) {
				rowTrim := strings.TrimSpace(lines[i])
				if !tableRowLineRe.MatchString(rowTrim) {
					break
				}
				if isTableSeparator(rowTrim) {
					i++
					continue
				}
				rows = append(rows, splitTableCells(rowTrim))
				i++
			}
			if len(rows) > 0 {
				out.WriteString(renderTable(rows))
			}
			continue
		}

		if unorderedListRe.MatchString(trimmed) {
			out.WriteString("<ul style=\"margin:8px 0;padding-left:24px;\">\n")
			for i < len(lines) {
				itemTrim := strings.TrimSpace(lines[i])
				m := unorderedListRe.FindStringSubmatch(itemTrim)
				if m == nil {
					break
				}
				fmt.Fprintf(&out, "<li style=\"margin:4px 0;\">%s</li>\n", formatInline(m[1]))
				i++
			}
			out.WriteString("</ul>\n")
			continue
		}

		if orderedListRe.MatchString(trimmed) {
			out.WriteString("<ol style=\"margin:8px 0;padding-left:24px;\">\n")
			for i < len(lines) {
				itemTrim := strings.TrimSpace(lines[i])
				m := orderedListRe.FindStringSubmatch(itemTrim)
				if m == nil {
					break
				}
				fmt.Fprintf(&out, "<li style=\"margin:4px 0;\">%s</li>\n", formatInline(m[1]))
				i++
			}
			out.WriteString("</ol>\n")
			continue
		}

		if trimmed == "" {
			i++
			continue
		}

		// Paragraph: gather consecutive non-blank, non-block lines.
		var para []string
		for i < len(lines) {
			l := lines[i]
			t := strings.TrimSpace(l)
			if t == "" ||
				headingLineRe.MatchString(t) ||
				tableRowLineRe.MatchString(t) ||
				unorderedListRe.MatchString(t) ||
				orderedListRe.MatchString(t) ||
				(strings.HasPrefix(t, "\x00CODE") && strings.HasSuffix(t, "\x00")) {
				break
			}
			para = append(para, t)
			i++
		}
		if len(para) > 0 {
			fmt.Fprintf(&out, "<p style=\"margin:8px 0;\">%s</p>\n",
				formatInline(strings.Join(para, " ")))
		}
	}

	result := out.String()

	// Restore code-block placeholders that landed inside paragraphs or alone.
	for i, block := range codeBlocks {
		placeholder := fmt.Sprintf("\x00CODE%d\x00", i)
		escapedPlaceholder := html.EscapeString(placeholder)
		result = strings.ReplaceAll(result, escapedPlaceholder, block)
		result = strings.ReplaceAll(result, placeholder, block)
		// Placeholders inside <p>…</p> become awkward; unwrap if the whole
		// paragraph is only the block.
		result = strings.ReplaceAll(result,
			fmt.Sprintf(`<p style="margin:8px 0;">%s</p>`, block), block)
	}

	result = excessBlankRe.ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

func writeBlock(out *strings.Builder, s string) {
	out.WriteString(s)
	out.WriteByte('\n')
}

func isTableSeparator(row string) bool {
	inner := strings.Trim(row, "|")
	for _, cell := range strings.Split(inner, "|") {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		for _, r := range cell {
			if r != '-' && r != ':' && r != ' ' {
				return false
			}
		}
	}
	return true
}

func splitTableCells(row string) []string {
	inner := strings.Trim(row, "|")
	raw := strings.Split(inner, "|")
	cells := make([]string, 0, len(raw))
	for _, c := range raw {
		cells = append(cells, strings.TrimSpace(c))
	}
	return cells
}

func renderTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<table style="%s">`+"\n", tableStyle)

	header := rows[0]
	b.WriteString("<thead><tr>\n")
	for _, cell := range header {
		fmt.Fprintf(&b, `<th style="%s">%s</th>`+"\n", thStyle, formatInline(cell))
	}
	b.WriteString("</tr></thead>\n")

	if len(rows) > 1 {
		b.WriteString("<tbody>\n")
		for _, row := range rows[1:] {
			b.WriteString("<tr>\n")
			// Pad/truncate to header width for stable columns.
			for i := 0; i < len(header); i++ {
				cell := ""
				if i < len(row) {
					cell = row[i]
				}
				fmt.Fprintf(&b, `<td style="%s">%s</td>`+"\n", tdStyle, formatInline(cell))
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody>\n")
	}

	b.WriteString("</table>\n")
	return b.String()
}

// formatInline escapes HTML then applies bold/italic/inline-code.
func formatInline(text string) string {
	var inlineCodes []string
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := inlineCodeRe.FindStringSubmatch(match)
		escaped := html.EscapeString(parts[1])
		block := fmt.Sprintf(`<code style="%s">%s</code>`, codeStyle, escaped)
		placeholder := fmt.Sprintf("\x00INLINE%d\x00", len(inlineCodes))
		inlineCodes = append(inlineCodes, block)
		return placeholder
	})

	text = html.EscapeString(text)
	text = boldRe.ReplaceAllString(text, "<strong>$1</strong>")

	text = starItalicRe.ReplaceAllStringFunc(text, func(match string) string {
		idx := strings.Index(match, "*")
		prefix := match[:idx]
		inner := match[idx+1 : len(match)-1]
		return prefix + "<em>" + inner + "</em>"
	})

	text = underscoreItalicRe.ReplaceAllStringFunc(text, func(match string) string {
		idx := strings.Index(match, "_")
		lastIdx := strings.LastIndex(match, "_")
		if idx == lastIdx {
			return match
		}
		prefix := match[:idx]
		inner := match[idx+1 : lastIdx]
		suffix := match[lastIdx+1:]
		return prefix + "<em>" + inner + "</em>" + suffix
	})

	for i, code := range inlineCodes {
		placeholder := fmt.Sprintf("\x00INLINE%d\x00", i)
		escapedPlaceholder := html.EscapeString(placeholder)
		text = strings.Replace(text, escapedPlaceholder, code, 1)
		text = strings.Replace(text, placeholder, code, 1)
	}

	return text
}
