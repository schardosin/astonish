// Package pdfgen — chrome.go provides high-quality PDF generation by rendering
// markdown as styled HTML in a headless Chrome instance via go-rod.
//
// This produces professional output with full Unicode/emoji support, proper CSS
// typography, and print-quality layout. It requires a running browser instance
// managed by the browser.Manager.
package pdfgen

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldhtml "github.com/yuin/goldmark/renderer/html"
)

// BrowserProvider is a minimal interface for obtaining a rod Browser instance.
// This is satisfied by browser.Manager's GetOrLaunch method, keeping pdfgen
// decoupled from the browser package.
type BrowserProvider interface {
	GetOrLaunch() (*rod.Browser, error)
}

// ConvertMarkdownToPDFChrome converts markdown source bytes to a high-quality
// PDF using headless Chrome. It renders the markdown as styled HTML, then uses
// Chrome's Page.printToPDF for professional output with full Unicode, emoji,
// and CSS support.
//
// The browser parameter provides access to a running Chrome instance. If nil
// or if the browser is unavailable, returns an error.
func ConvertMarkdownToPDFChrome(source []byte, browser BrowserProvider) ([]byte, error) {
	if browser == nil {
		return nil, fmt.Errorf("no browser provider available")
	}

	// Step 1: Convert markdown to HTML using goldmark.
	htmlContent, err := markdownToHTML(source)
	if err != nil {
		return nil, fmt.Errorf("markdown to HTML conversion failed: %w", err)
	}

	// Step 2: Wrap in a full HTML document with CSS styling.
	fullHTML := wrapInHTMLTemplate(htmlContent)

	// Step 3: Render to PDF using Chrome.
	return renderHTMLToPDF(browser, fullHTML)
}

// markdownToHTML converts markdown bytes to an HTML string using goldmark.
func markdownToHTML(source []byte) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(
			goldhtml.WithUnsafe(), // allow raw HTML in markdown
		),
	)

	var buf bytes.Buffer
	if err := md.Convert(source, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderHTMLToPDF uses a headless Chrome page to print HTML to PDF.
func renderHTMLToPDF(bp BrowserProvider, html string) ([]byte, error) {
	b, err := bp.GetOrLaunch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	// Create a dedicated page for PDF rendering — do not use the user's
	// active page, as SetDocumentContent would destroy their session.
	pg, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("failed to create browser page: %w", err)
	}
	defer pg.Close()

	// Inject the HTML content.
	if err := pg.SetDocumentContent(html); err != nil {
		return nil, fmt.Errorf("failed to set document content: %w", err)
	}

	// Wait for the page to be ready (fonts loaded, layout complete).
	if err := pg.WaitLoad(); err != nil {
		return nil, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Wait for all fonts (including emoji) to finish loading before printing.
	// Chrome's WaitLoad fires on DOMContentLoaded/load events, but font loading
	// (especially color emoji fonts like NotoColorEmoji) happens asynchronously.
	// Without this, emoji may render as blank or tofu in the PDF.
	_, err = pg.Eval(`() => document.fonts.ready`)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for fonts: %w", err)
	}

	// Wait for mermaid diagrams (if any) to finish rendering. The inline
	// script in the HTML template sets window.__mermaidDone = true once all
	// ```mermaid code blocks have been converted to SVG. We poll briefly
	// to avoid blocking indefinitely if mermaid fails to load.
	_, err = pg.Eval(`() => new Promise((resolve) => {
		if (window.__mermaidDone) { resolve(true); return; }
		let tries = 0;
		const iv = setInterval(() => {
			tries++;
			if (window.__mermaidDone || tries > 100) { clearInterval(iv); resolve(true); }
		}, 50);
	})`)
	if err != nil {
		// Non-fatal: diagrams may not render but the rest of the PDF is fine.
		fmt.Printf("warning: mermaid wait failed: %v\n", err)
	}

	// Print to PDF with sensible margins and background printing.
	marginV := 0.6  // inches (~1.5cm) top/bottom
	marginH := 0.75 // inches (~1.9cm) left/right
	reader, err := pg.PDF(&proto.PagePrintToPDF{
		PrintBackground: true,
		MarginTop:       &marginV,
		MarginBottom:    &marginV,
		MarginLeft:      &marginH,
		MarginRight:     &marginH,
	})
	if err != nil {
		return nil, fmt.Errorf("Chrome PDF generation failed: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF data: %w", err)
	}

	return data, nil
}

// wrapInHTMLTemplate wraps an HTML fragment in a complete HTML document with
// CSS styling optimized for print/PDF output. Includes mermaid.js for
// rendering mermaid fenced code blocks as SVG diagrams.
func wrapInHTMLTemplate(body string) string {
	return fmt.Sprintf("<!DOCTYPE html>\n"+
		"<html lang=\"en\">\n<head>\n<meta charset=\"UTF-8\">\n"+
		"<style>\n%s\n</style>\n"+
		"<script src=\"https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js\"></script>\n"+
		"<script>\n%s\n</script>\n"+
		"</head>\n<body>\n%s\n</body>\n</html>",
		pdfCSS, mermaidInitScript, body)
}

// mermaidInitScript is the inline JavaScript that converts
// <pre><code class="language-mermaid">...</code></pre> blocks (produced by
// goldmark from fenced mermaid code blocks) into rendered SVG diagrams.
// It runs on DOMContentLoaded and sets window.__mermaidDone when complete,
// which the Go code polls for before printing to PDF.
const mermaidInitScript = `
document.addEventListener('DOMContentLoaded', async function() {
  var codeBlocks = document.querySelectorAll('code.language-mermaid');
  if (codeBlocks.length === 0) {
    window.__mermaidDone = true;
    return;
  }

  mermaid.initialize({
    startOnLoad: false,
    theme: 'neutral',
    fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    securityLevel: 'loose'
  });

  try {
    for (var i = 0; i < codeBlocks.length; i++) {
      var codeEl = codeBlocks[i];
      var preEl = codeEl.parentElement;
      var source = codeEl.textContent || '';
      var id = 'mermaid-pdf-' + i;

      var result = await mermaid.render(id, source.trim());
      var container = document.createElement('div');
      container.className = 'mermaid-diagram';
      container.innerHTML = result.svg;

      if (preEl && preEl.tagName === 'PRE') {
        preEl.replaceWith(container);
      } else {
        codeEl.replaceWith(container);
      }
    }
  } catch (e) {
    console.error('Mermaid rendering failed:', e);
  }

  window.__mermaidDone = true;
});
`

// pdfCSS is the embedded stylesheet for PDF output. It provides clean
// typography, compact list spacing, styled tables, and code blocks.
const pdfCSS = `
/* Reset and base */
*, *::before, *::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

/* Page layout */
@page {
  size: A4;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji";
  font-size: 11pt;
  line-height: 1.5;
  color: #1a1a1a;
  max-width: 100%;
}

/* Headings */
h1, h2, h3, h4, h5, h6 {
  margin-top: 1.2em;
  margin-bottom: 0.4em;
  line-height: 1.25;
  color: #111;
  page-break-after: avoid;
}

h1 { font-size: 22pt; border-bottom: 2px solid #e0e0e0; padding-bottom: 0.3em; }
h2 { font-size: 18pt; border-bottom: 1px solid #e8e8e8; padding-bottom: 0.2em; }
h3 { font-size: 14pt; }
h4 { font-size: 12pt; }
h5 { font-size: 11pt; font-weight: 600; }
h6 { font-size: 10pt; font-weight: 600; color: #555; }

/* First heading: no top margin */
body > h1:first-child,
body > h2:first-child,
body > h3:first-child {
  margin-top: 0;
}

/* Paragraphs */
p {
  margin-top: 0.5em;
  margin-bottom: 0.5em;
}

/* Lists — compact spacing */
ul, ol {
  margin-top: 0.3em;
  margin-bottom: 0.5em;
  padding-left: 1.8em;
}

li {
  margin-bottom: 0.15em;
  line-height: 1.45;
}

li > ul, li > ol {
  margin-top: 0.1em;
  margin-bottom: 0.1em;
}

/* Tables */
table {
  border-collapse: collapse;
  width: 100%;
  margin-top: 0.5em;
  margin-bottom: 0.8em;
  font-size: 10pt;
  page-break-inside: auto;
}

th, td {
  border: 1px solid #d0d0d0;
  padding: 6px 10px;
  text-align: left;
  vertical-align: top;
}

th {
  background-color: #f5f5f5;
  font-weight: 600;
  color: #333;
}

tr:nth-child(even) {
  background-color: #fafafa;
}

/* Code */
code {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, Courier, monospace;
  font-size: 0.9em;
  background-color: #f4f4f4;
  padding: 0.15em 0.35em;
  border-radius: 3px;
  color: #c7254e;
}

pre {
  background-color: #f6f6f6;
  border: 1px solid #e0e0e0;
  border-radius: 5px;
  padding: 12px 16px;
  margin-top: 0.5em;
  margin-bottom: 0.8em;
  overflow-x: auto;
  page-break-inside: avoid;
}

pre code {
  background: none;
  padding: 0;
  border-radius: 0;
  color: inherit;
  font-size: 9pt;
  line-height: 1.45;
}

/* Blockquotes */
blockquote {
  border-left: 4px solid #d0d0d0;
  margin: 0.5em 0;
  padding: 0.4em 1em;
  color: #555;
  background-color: #fafafa;
}

blockquote p {
  margin: 0.2em 0;
}

/* Horizontal rules */
hr {
  border: none;
  border-top: 1px solid #e0e0e0;
  margin: 1.2em 0;
}

/* Links */
a {
  color: #0066cc;
  text-decoration: none;
}

/* Bold and emphasis */
strong { font-weight: 600; }
em { font-style: italic; }

/* Images */
img {
  max-width: 100%;
  height: auto;
}

/* Print-specific */
@media print {
  body { color: #000; }
  a { color: #0066cc; }
  h1, h2, h3 { page-break-after: avoid; }
  table, pre, blockquote { page-break-inside: avoid; }
}

/* Mermaid diagrams */
.mermaid-diagram {
  display: flex;
  justify-content: center;
  margin: 0.8em 0;
  padding: 16px;
  page-break-inside: avoid;
}

.mermaid-diagram svg {
  max-width: 100%;
  height: auto;
}
`
