package email

import (
	"net/mail"
	"strings"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		html     string
		expected int // number of unique links
	}{
		{
			name:     "plain text URLs",
			text:     "Visit https://example.com and http://test.org/path",
			html:     "",
			expected: 2,
		},
		{
			name:     "HTML href extraction",
			text:     "",
			html:     `<a href="https://example.com/verify?token=abc123">Click here</a>`,
			expected: 1,
		},
		{
			name:     "dedup across text and HTML",
			text:     "Click https://example.com/verify",
			html:     `<a href="https://example.com/verify">Verify</a>`,
			expected: 1,
		},
		{
			name:     "no links",
			text:     "Hello world, no links here.",
			html:     "",
			expected: 0,
		},
		{
			name:     "multiple links with trailing punctuation",
			text:     "See https://example.com/a, or https://example.com/b.",
			html:     "",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := ExtractLinks(tt.text, tt.html)
			if len(links) != tt.expected {
				t.Errorf("expected %d links, got %d: %v", tt.expected, len(links), links)
			}
		})
	}
}

func TestClassifyVerificationLinks(t *testing.T) {
	tests := []struct {
		name         string
		links        []string
		subject      string
		senderDomain string
		expected     int
	}{
		{
			name: "verification URL patterns",
			links: []string{
				"https://reddit.com/verify/abc123",
				"https://reddit.com/r/funny",
				"https://example.com/confirm?token=longtoken1234567890abcdef",
			},
			subject:      "Welcome to Reddit",
			senderDomain: "reddit.com",
			expected:     2, // verify and confirm URLs
		},
		{
			name: "long token in verify email subject",
			links: []string{
				"https://example.com/action?t=abcdefghijklmnopqrstuvwxyz1234567890",
			},
			subject:      "Verify your email address",
			senderDomain: "example.com",
			expected:     1,
		},
		{
			name: "no verification links",
			links: []string{
				"https://example.com/",
				"https://example.com/about",
				"https://example.com/help",
			},
			subject:      "Monthly Newsletter",
			senderDomain: "example.com",
			expected:     0,
		},
		{
			name: "activation link",
			links: []string{
				"https://accounts.google.com/activate?token=abc123456789012345",
			},
			subject:      "Complete your Google signup",
			senderDomain: "google.com",
			expected:     1,
		},
		{
			name: "email-verification path",
			links: []string{
				"https://github.com/email-verification/abc123def456",
			},
			subject:      "Please verify your email",
			senderDomain: "github.com",
			expected:     1,
		},
		{
			name: "fallback to sender domain with long token",
			links: []string{
				"https://noreply.example.com/u/check?id=a1b2c3d4e5f6g7h8i9j0k1l2m3n4",
			},
			subject:      "Confirm your account",
			senderDomain: "example.com",
			expected:     1, // Sender domain match + long token + verify subject
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyVerificationLinks(tt.links, tt.subject, tt.senderDomain)
			if len(result) != tt.expected {
				t.Errorf("expected %d verification links, got %d: %v", tt.expected, len(result), result)
			}
		})
	}
}

func TestExtractSenderDomain(t *testing.T) {
	tests := []struct {
		from     string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"John Doe <john@example.com>", "example.com"},
		{"noreply@reddit.com", "reddit.com"},
		{"Reddit <noreply@reddit.com>", "reddit.com"},
	}

	for _, tt := range tests {
		t.Run(tt.from, func(t *testing.T) {
			domain := ExtractSenderDomain(tt.from)
			if domain != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, domain)
			}
		})
	}
}

func TestBuildMessageID(t *testing.T) {
	id := BuildMessageID("example.com")
	if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, ">") {
		t.Errorf("Message-ID should be wrapped in angle brackets: %s", id)
	}
	if !strings.Contains(id, "@example.com") {
		t.Errorf("Message-ID should contain domain: %s", id)
	}

	// Should generate unique IDs
	id2 := BuildMessageID("example.com")
	if id == id2 {
		t.Errorf("Message-IDs should be unique: %s == %s", id, id2)
	}
}

func TestHtmlToText(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		contains string
	}{
		{
			name:     "strips tags",
			html:     "<p>Hello <b>world</b></p>",
			contains: "Hello world",
		},
		{
			name:     "strips scripts",
			html:     "<p>Hello</p><script>alert('xss')</script><p>World</p>",
			contains: "Hello",
		},
		{
			name:     "decodes entities",
			html:     "a &amp; b &lt; c",
			contains: "a & b < c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := htmlToText(tt.html)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestParseMIMEBody_PlainText(t *testing.T) {
	raw := "Content-Type: text/plain\r\n\r\nHello, this is a test email."
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
		return
	}

	parsed, err := ParseMIMEBody(msg, 50000)
	if err != nil {
		t.Fatal(err)
		return
	}

	if parsed.Text != "Hello, this is a test email." {
		t.Errorf("unexpected text: %q", parsed.Text)
	}
	if parsed.HTML != "" {
		t.Errorf("expected no HTML, got %q", parsed.HTML)
	}
}

func TestParseMIMEBody_HTML(t *testing.T) {
	raw := "Content-Type: text/html\r\n\r\n<html><body><p>Hello <b>world</b></p></body></html>"
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
		return
	}

	parsed, err := ParseMIMEBody(msg, 50000)
	if err != nil {
		t.Fatal(err)
		return
	}

	if parsed.HTML == "" {
		t.Error("expected HTML content")
	}
	if !strings.Contains(parsed.Text, "Hello") {
		t.Errorf("expected text fallback from HTML, got %q", parsed.Text)
	}
}

func TestIsTokenLike(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abcdefghijklmnopqrstuvwxyz", true},
		{"abc123def456ghi789jkl0", true},
		{"a1b2-c3d4_e5f6.g7h8i9j0", true},
		{"short", false},
		{"has spaces and stuff!", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isTokenLike(tt.input)
			if result != tt.expected {
				t.Errorf("isTokenLike(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
