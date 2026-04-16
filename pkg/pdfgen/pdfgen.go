// Package pdfgen converts markdown content to PDF using goldmark and goldmark-pdf.
// It uses only built-in PDF fonts (Helvetica, Courier) so no external font files
// or network downloads are required.
package pdfgen

import (
	"bytes"
	"fmt"
	"html"
	"image/color"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	pdf "github.com/stephenafamo/goldmark-pdf"
)

// ConvertMarkdownToPDF converts markdown source bytes to PDF bytes.
// The resulting PDF uses built-in fonts (Helvetica for body, Courier for code)
// and requires no external dependencies or network access.
func ConvertMarkdownToPDF(source []byte) ([]byte, error) {
	// Preprocess: decode HTML entities and normalize Unicode characters
	// that the built-in PDF fonts (WinAnsiEncoding / Latin-1) cannot render.
	cleaned := preprocessMarkdown(string(source))

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM), // tables, strikethrough, autolinks
		goldmark.WithRenderer(
			pdf.New(
				// Use built-in PDF fonts — no network downloads needed.
				pdf.WithHeadingFont(pdf.FontHelvetica),
				pdf.WithBodyFont(pdf.FontHelvetica),
				pdf.WithCodeFont(pdf.FontCourier),
				pdf.WithLinkColor(color.RGBA{R: 0, G: 102, B: 204, A: 255}),
			),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(cleaned), &buf); err != nil {
		return nil, fmt.Errorf("markdown to PDF conversion failed: %w", err)
	}

	return buf.Bytes(), nil
}

// preprocessMarkdown decodes HTML entities and replaces Unicode characters
// that fall outside the Latin-1 range with ASCII equivalents. Built-in PDF
// fonts (Helvetica, Courier) use WinAnsiEncoding which only supports
// ISO 8859-1 / Latin-1 characters.
func preprocessMarkdown(s string) string {
	// Step 1: Decode HTML entities (&quot; → ", &amp; → &, etc.)
	// LLMs sometimes produce double- or triple-encoded entities
	// (e.g., &amp;amp; or &amp;quot;), so we loop until stable.
	for i := 0; i < 5; i++ {
		decoded := html.UnescapeString(s)
		if decoded == s {
			break
		}
		s = decoded
	}

	// Step 2: Replace common Unicode characters with Latin-1 safe equivalents.
	// These are frequently produced by LLMs and web content.
	replacer := strings.NewReplacer(
		// Smart quotes → straight quotes
		"\u201C", "\"", // " left double quotation mark
		"\u201D", "\"", // " right double quotation mark
		"\u2018", "'", // ' left single quotation mark
		"\u2019", "'", // ' right single quotation mark

		// Dashes
		"\u2014", "--", // — em dash
		"\u2013", "-", // – en dash
		"\u2012", "-", // ‒ figure dash

		// Ellipsis
		"\u2026", "...", // … horizontal ellipsis

		// Bullets and markers
		"\u2022", "*", // • bullet
		"\u2023", ">", // ‣ triangular bullet
		"\u25AA", "*", // ▪ black small square
		"\u25CB", "o", // ○ white circle
		"\u2043", "-", // ⁃ hyphen bullet

		// Spaces
		"\u00A0", " ", // non-breaking space
		"\u2002", " ", // en space
		"\u2003", " ", // em space
		"\u2009", " ", // thin space
		"\u200B", "", // zero-width space
		"\u200C", "", // zero-width non-joiner
		"\u200D", "", // zero-width joiner
		"\uFEFF", "", // byte order mark

		// Arrows
		"\u2192", "->", // → rightwards arrow
		"\u2190", "<-", // ← leftwards arrow
		"\u2194", "<->", // ↔ left right arrow
		"\u21D2", "=>", // ⇒ rightwards double arrow

		// Math and symbols
		"\u2260", "!=", // ≠ not equal to
		"\u2264", "<=", // ≤ less-than or equal to
		"\u2265", ">=", // ≥ greater-than or equal to
		"\u00D7", "x", // × multiplication sign
		"\u2212", "-", // − minus sign
		"\u00B1", "+/-", // ± plus-minus sign
		"\u221E", "inf", // ∞ infinity

		// Check marks and crosses
		"\u2713", "[x]", // ✓ check mark
		"\u2714", "[x]", // ✔ heavy check mark
		"\u2715", "[!]", // ✕ multiplication x
		"\u2716", "[!]", // ✖ heavy multiplication x
		"\u2717", "[ ]", // ✗ ballot x
		"\u2718", "[ ]", // ✘ heavy ballot x

		// Trademark and legal
		"\u2122", "(TM)", // ™ trade mark sign

		// Stars
		"\u2605", "*", // ★ black star
		"\u2606", "*", // ☆ white star

		// Other common LLM output chars
		"\u200E", "", // left-to-right mark
		"\u200F", "", // right-to-left mark
	)

	s = replacer.Replace(s)

	return s
}
