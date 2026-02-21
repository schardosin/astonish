package tools

import (
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	"google.golang.org/adk/tool"
)

const (
	readPDFMaxPages    = 500    // safety limit
	readPDFMaxFileSize = 50e6   // 50MB
	readPDFMaxChars    = 100000 // 100K characters default output limit
)

// ReadPDFArgs are the arguments for the read_pdf tool.
type ReadPDFArgs struct {
	Path     string `json:"path" jsonschema:"Path to a local PDF file, or an HTTP/HTTPS URL to fetch"`
	MaxPages int    `json:"max_pages,omitempty" jsonschema:"Maximum number of pages to extract (default: all)"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"Maximum characters to return (default 100000)"`
}

// ReadPDFResult is the result of the read_pdf tool.
type ReadPDFResult struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
	Truncated bool   `json:"truncated"`
	Length    int    `json:"length"`
	Warning   string `json:"warning,omitempty"`
}

// ReadPDF extracts text content from a PDF file or URL.
func ReadPDF(_ tool.Context, args ReadPDFArgs) (ReadPDFResult, error) {
	if args.Path == "" {
		return ReadPDFResult{}, fmt.Errorf("path is required")
	}

	maxPages := args.MaxPages
	if maxPages <= 0 {
		maxPages = readPDFMaxPages
	}

	maxChars := args.MaxChars
	if maxChars <= 0 {
		maxChars = readPDFMaxChars
	}

	// Determine if this is a URL or local file
	localPath, tempFile, err := resolvePDFPath(args.Path)
	if err != nil {
		return ReadPDFResult{}, err
	}
	if tempFile != "" {
		defer os.Remove(tempFile)
	}

	// Open the PDF
	f, reader, err := pdf.Open(localPath)
	if err != nil {
		return ReadPDFResult{}, fmt.Errorf("failed to open PDF: %w", err)
	}
	defer f.Close()

	totalPages := reader.NumPage()
	pagesToRead := totalPages
	if pagesToRead > maxPages {
		pagesToRead = maxPages
	}

	// Extract text page by page
	var sb strings.Builder
	for i := 1; i <= pagesToRead; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// Skip pages that fail to extract
			sb.WriteString(fmt.Sprintf("\n--- Page %d (extraction failed) ---\n", i))
			continue
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		if pagesToRead > 1 {
			sb.WriteString(fmt.Sprintf("\n--- Page %d ---\n", i))
		}
		sb.WriteString(text)
		sb.WriteString("\n")

		// Early exit if we've already exceeded max chars
		if sb.Len() > maxChars {
			break
		}
	}

	content := sb.String()
	truncated := false
	warning := ""

	if len(content) > maxChars {
		content = content[:maxChars]
		content += "\n\n[Content truncated. Original text exceeded the limit.]"
		truncated = true
	}

	if pagesToRead < totalPages {
		warning = fmt.Sprintf("Only %d of %d pages were extracted (max_pages limit).", pagesToRead, totalPages)
	}

	return ReadPDFResult{
		Text:      content,
		PageCount: totalPages,
		Truncated: truncated,
		Length:    len(content),
		Warning:   warning,
	}, nil
}

// resolvePDFPath handles both local file paths and URLs.
// For URLs, it downloads to a temp file and returns the temp path.
// Returns (localPath, tempFilePath, error). tempFilePath is non-empty only for URLs.
func resolvePDFPath(path string) (string, string, error) {
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return downloadPDFToTemp(path)
	}

	// Reject other URL-like schemes early
	if strings.Contains(path, "://") {
		return "", "", fmt.Errorf("only http and https URLs are supported, got scheme %q", strings.SplitN(path, "://", 2)[0])
	}

	// Local file — verify it exists and check extension
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("path is a directory, not a file")
	}
	if info.Size() > int64(readPDFMaxFileSize) {
		return "", "", fmt.Errorf("file too large (%d bytes, max %.0f)", info.Size(), readPDFMaxFileSize)
	}
	if !strings.HasSuffix(lower, ".pdf") {
		return "", "", fmt.Errorf("file does not have .pdf extension: %s", filepath.Base(path))
	}

	return path, "", nil
}

// downloadPDFToTemp downloads a PDF from a URL to a temporary file.
func downloadPDFToTemp(url string) (string, string, error) {
	// SSRF check (reuse from web_fetch)
	parsed, err := parseAndValidateURL(url)
	if err != nil {
		return "", "", err
	}

	if err := checkSSRF(parsed.Hostname()); err != nil {
		return "", "", err
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch PDF: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d fetching PDF", resp.StatusCode)
	}

	// Limit download size
	limitedReader := io.LimitReader(resp.Body, int64(readPDFMaxFileSize))

	tmp, err := os.CreateTemp("", "astonish-pdf-*.pdf")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, limitedReader); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("failed to download PDF: %w", err)
	}
	tmp.Close()

	return tmp.Name(), tmp.Name(), nil
}

// parseAndValidateURL validates URL scheme for PDF downloads.
func parseAndValidateURL(rawURL string) (*nurl.URL, error) {
	parsed, err := nurl.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs are supported, got %q", parsed.Scheme)
	}
	return parsed, nil
}
