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
				// Disable HTML escaping — goldmark-pdf defaults to escaping &, ", <, >
				// as &amp;, &quot;, etc. which is wrong for PDF text streams.
				pdf.WithEscapeHTML(false),
				// Tighten line spacing. The default Normal style has Spacing: 3 (total
				// line height 15pt with 12pt font). The library uses Size+Spacing as the
				// BR gap between list items, paragraphs, etc. — which creates large gaps
				// between bullet points. Setting Spacing to 0 gives a compact layout.
				tightSpacing(),
			),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(cleaned), &buf); err != nil {
		return nil, fmt.Errorf("markdown to PDF conversion failed: %w", err)
	}

	return buf.Bytes(), nil
}

// tightSpacing returns a goldmark-pdf option that reduces inter-element spacing.
// The default Normal style has Spacing: 3 which the library uses as BR(Size+Spacing)
// between list items, paragraphs, and other block elements. This creates excessive
// gaps between bullet points. Setting to 0 means list items are spaced by just the
// font size (12pt), while paragraphs still get double spacing (entering + leaving BR).
func tightSpacing() pdf.Option {
	return pdf.OptionFunc(func(c *pdf.Config) {
		c.Styles.Normal.Spacing = 0
		c.Styles.H1.Spacing = 3
		c.Styles.H2.Spacing = 3
		c.Styles.H3.Spacing = 2
		c.Styles.H4.Spacing = 2
		c.Styles.H5.Spacing = 2
		c.Styles.H6.Spacing = 2
		c.Styles.THeader.Spacing = 1
		c.Styles.TBody.Spacing = 1
	})
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

	// Step 3: Strip any remaining characters outside the Latin-1 range (> U+00FF).
	// This catches emoji (🚀, ⚠️, 🔧, etc.) and other Unicode symbols that the
	// built-in PDF fonts cannot render. Without this, they appear as mojibake
	// (e.g., ð\x9f\x9a\x80 for 🚀). We strip rather than replace because there
	// is no meaningful ASCII equivalent for most emoji.
	s = stripNonLatin1(s)

	return s
}

// stripNonLatin1 removes all runes outside the Latin-1 range (U+0000–U+00FF)
// from the string. This includes emoji, CJK characters, and other multi-byte
// Unicode that built-in PDF fonts cannot render.
func stripNonLatin1(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r <= 0x00FF {
			b.WriteRune(r)
		}
	}
	return b.String()
}
