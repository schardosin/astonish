package email

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// linkRe matches http/https URLs in text.
var linkRe = regexp.MustCompile(`https?://[^\s<>"'\)\]\}]+`)

// verifyPatterns are URL path/query substrings that suggest a verification link.
var verifyPatterns = []string{
	"verify", "confirm", "activate", "validate",
	"registration", "signup", "sign-up", "token",
	"auth/callback", "email-verification", "email_verification",
	"approve", "opt-in", "optin",
}

// verifySubjectPatterns are subject line substrings that suggest a verification email.
var verifySubjectPatterns = []string{
	"verify your email", "confirm your email", "confirm your account",
	"activate your account", "complete registration", "complete your registration",
	"email verification", "email confirmation", "verify your account",
	"please confirm", "please verify",
}

// ParsedBody holds the result of parsing a MIME email body.
type ParsedBody struct {
	Text        string
	HTML        string
	Attachments []AttachmentInfo
}

// ParseMIMEBody parses the body of an email message, extracting text, HTML,
// and attachment metadata. The msg parameter should have its body positioned
// at the start. maxBodyChars limits the size of text/html returned.
func ParseMIMEBody(msg *mail.Message, maxBodyChars int) (*ParsedBody, error) {
	if maxBodyChars <= 0 {
		maxBodyChars = 50000
	}

	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Fall back to reading the whole body as text
		body, readErr := io.ReadAll(io.LimitReader(msg.Body, int64(maxBodyChars)))
		if readErr != nil {
			return nil, readErr
		}
		return &ParsedBody{Text: string(body)}, nil
	}

	result := &ParsedBody{}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			body, _ := io.ReadAll(io.LimitReader(msg.Body, int64(maxBodyChars)))
			return &ParsedBody{Text: string(body)}, nil
		}
		if err := parseMultipart(msg.Body, boundary, result, maxBodyChars); err != nil {
			return result, nil // Return partial results on error
		}
	} else if strings.HasPrefix(mediaType, "text/html") {
		body, _ := io.ReadAll(io.LimitReader(msg.Body, int64(maxBodyChars)))
		result.HTML = string(body)
		result.Text = htmlToText(result.HTML)
	} else {
		// text/plain or unknown — treat as text
		body, _ := io.ReadAll(io.LimitReader(msg.Body, int64(maxBodyChars)))
		result.Text = string(body)
	}

	return result, nil
}

// parseMultipart recursively parses a multipart MIME body.
func parseMultipart(body io.Reader, boundary string, result *ParsedBody, maxChars int) error {
	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		partType := part.Header.Get("Content-Type")
		if partType == "" {
			partType = "text/plain"
		}
		partMediaType, partParams, _ := mime.ParseMediaType(partType)

		disposition := part.Header.Get("Content-Disposition")

		switch {
		case strings.HasPrefix(partMediaType, "multipart/"):
			partBoundary := partParams["boundary"]
			if partBoundary != "" {
				if err := parseMultipart(part, partBoundary, result, maxChars); err != nil {
					slog.Debug("nested multipart parse error", "boundary", partBoundary, "error", err)
				}
			}

		case isAttachment(disposition, part.FileName()):
			// Extract attachment metadata without reading the full body
			info := AttachmentInfo{
				Name:        part.FileName(),
				ContentType: partMediaType,
			}
			// Read to count size but discard content
			n, _ := io.Copy(io.Discard, part)
			info.Size = n
			result.Attachments = append(result.Attachments, info)

		case strings.HasPrefix(partMediaType, "text/html"):
			if result.HTML == "" {
				data, _ := io.ReadAll(io.LimitReader(part, int64(maxChars)))
				result.HTML = string(data)
				if result.Text == "" {
					result.Text = htmlToText(result.HTML)
				}
			}

		case strings.HasPrefix(partMediaType, "text/plain"):
			if result.Text == "" {
				data, _ := io.ReadAll(io.LimitReader(part, int64(maxChars)))
				result.Text = string(data)
			}

		default:
			// Other parts (images, etc.) treated as attachments
			if part.FileName() != "" {
				info := AttachmentInfo{
					Name:        part.FileName(),
					ContentType: partMediaType,
				}
				n, _ := io.Copy(io.Discard, part)
				info.Size = n
				result.Attachments = append(result.Attachments, info)
			} else {
				// Skip unknown inline parts
				_, _ = io.Copy(io.Discard, part)
			}
		}
	}
	return nil
}

// isAttachment returns true if the MIME part is an attachment.
func isAttachment(disposition, filename string) bool {
	if strings.Contains(strings.ToLower(disposition), "attachment") {
		return true
	}
	return filename != ""
}

// htmlToText does a basic conversion of HTML to plain text by stripping tags.
func htmlToText(html string) string {
	// Remove script blocks
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	text := scriptRe.ReplaceAllString(html, "")

	// Remove style blocks
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	text = styleRe.ReplaceAllString(text, "")

	// Replace <br>, <p>, <div>, <li> with newlines
	blockRe := regexp.MustCompile(`(?i)<(?:br|/p|/div|/li|/tr|/h[1-6])[^>]*>`)
	text = blockRe.ReplaceAllString(text, "\n")

	// Strip remaining tags
	tagRe := regexp.MustCompile(`<[^>]*>`)
	text = tagRe.ReplaceAllString(text, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Collapse multiple blank lines
	blankRe := regexp.MustCompile(`\n{3,}`)
	text = blankRe.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// ExtractLinks extracts all http/https URLs from text and HTML content.
func ExtractLinks(text, html string) []string {
	seen := make(map[string]bool)
	var links []string

	// Extract from text
	for _, match := range linkRe.FindAllString(text, -1) {
		clean := cleanURL(match)
		if !seen[clean] {
			seen[clean] = true
			links = append(links, clean)
		}
	}

	// Extract from HTML (href attributes)
	if html != "" {
		hrefRe := regexp.MustCompile(`href=["']?(https?://[^"'\s>]+)["']?`)
		for _, match := range hrefRe.FindAllStringSubmatch(html, -1) {
			if len(match) > 1 {
				clean := cleanURL(match[1])
				if !seen[clean] {
					seen[clean] = true
					links = append(links, clean)
				}
			}
		}
	}

	return links
}

// ClassifyVerificationLinks filters a list of URLs to find likely verification/confirmation links.
// The subject parameter helps improve accuracy (verification email subjects are recognizable).
// The senderDomain is used to prefer links from the same domain as the sender.
func ClassifyVerificationLinks(links []string, subject, senderDomain string) []string {
	// Check if subject suggests a verification email
	subjectLower := strings.ToLower(subject)
	isVerifyEmail := false
	for _, pat := range verifySubjectPatterns {
		if strings.Contains(subjectLower, pat) {
			isVerifyEmail = true
			break
		}
	}

	var result []string
	for _, link := range links {
		linkLower := strings.ToLower(link)

		// Check URL path/query for verification patterns
		hasPattern := false
		for _, pat := range verifyPatterns {
			if strings.Contains(linkLower, pat) {
				hasPattern = true
				break
			}
		}

		// Check for long token-like query parameters (>20 chars of hex/base64)
		hasLongToken := hasLongTokenParam(link)

		// A link is classified as verification if:
		// 1. It matches a verification pattern, OR
		// 2. The subject suggests verification AND the link has a long token parameter
		if hasPattern || (isVerifyEmail && hasLongToken) {
			result = append(result, link)
		}
	}

	// If we found nothing but the subject suggests verification, include links
	// from the sender's domain that have long tokens
	if len(result) == 0 && isVerifyEmail && senderDomain != "" {
		for _, link := range links {
			if strings.Contains(strings.ToLower(link), strings.ToLower(senderDomain)) && hasLongTokenParam(link) {
				result = append(result, link)
			}
		}
	}

	return result
}

// hasLongTokenParam checks if a URL has a query parameter value that looks like
// a one-time token (long string of hex, base64, or random characters).
func hasLongTokenParam(url string) bool {
	idx := strings.Index(url, "?")
	if idx < 0 {
		// Check path segments for long hex/base64 tokens
		parts := strings.Split(url, "/")
		for _, p := range parts {
			if len(p) >= 20 && isTokenLike(p) {
				return true
			}
		}
		return false
	}
	query := url[idx+1:]
	for _, param := range strings.Split(query, "&") {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) == 2 && len(kv[1]) >= 20 && isTokenLike(kv[1]) {
			return true
		}
	}
	return false
}

// isTokenLike returns true if a string looks like a random token.
func isTokenLike(s string) bool {
	if len(s) < 20 {
		return false
	}
	// Count alphanumeric + common token chars
	tokenChars := 0
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			tokenChars++
		}
	}
	return float64(tokenChars)/float64(len(s)) > 0.85
}

// cleanURL removes trailing punctuation that's commonly captured by URL regex.
func cleanURL(u string) string {
	u = strings.TrimRight(u, ".,;:!?)>]}")
	return u
}

// ExtractSenderDomain extracts the domain from an email address.
func ExtractSenderDomain(from string) string {
	// Handle "Name <email@domain.com>" format
	addr, err := mail.ParseAddress(from)
	if err == nil {
		from = addr.Address
	}
	parts := strings.Split(from, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// BuildMessageID creates a unique Message-ID header value.
func BuildMessageID(domain string) string {
	if domain == "" {
		domain = "astonish.local"
	}
	return fmt.Sprintf("<%s@%s>", randomID(), domain)
}

// randomID returns a unique ID for Message-ID headers.
func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d.%s", time.Now().UnixNano(), hex.EncodeToString(b))
}
