package credentials

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

// SecretScanner detects potential secrets in user messages using a 3-layer
// hybrid approach: keyword-anchored heuristics, Shannon entropy analysis,
// and generic structural pattern matching. All layers are pure Go — no
// external ML dependencies.
//
// When a potential secret is detected, it is reported as a Detection with
// the matched text and the layer that caught it. The caller (PendingVault)
// handles replacement with <<<SECRET_N>>> tokens.
type SecretScanner struct {
	// EntropyThreshold is the minimum Shannon entropy (bits/char) for a
	// token to be flagged by the entropy layer. Default: 4.0.
	// Normal English text averages ~3.5; random passwords average ~5.0+.
	EntropyThreshold float64

	// MinTokenLength is the minimum character length for a token to be
	// checked by the entropy and structural layers. Default: 16.
	MinTokenLength int

	// Enabled controls whether the scanner runs. Default: true.
	Enabled bool
}

// Detection represents a single detected potential secret.
type Detection struct {
	Original string // The text that was detected
	Start    int    // Byte offset in the original text
	End      int    // Byte offset end (exclusive)
	Layer    string // Which layer caught it: "keyword", "entropy", "structural"
}

// NewSecretScanner creates a scanner with default thresholds.
func NewSecretScanner() *SecretScanner {
	return &SecretScanner{
		EntropyThreshold: 4.0,
		MinTokenLength:   16,
		Enabled:          true,
	}
}

// Scan analyzes text and returns all detected potential secrets.
// Detections are returned in order of appearance. Overlapping detections
// from different layers are deduplicated (keyword layer takes priority).
func (s *SecretScanner) Scan(text string) []Detection {
	if !s.Enabled || text == "" {
		return nil
	}

	var detections []Detection

	// Layer 1: Keyword-anchored heuristics
	detections = append(detections, s.scanKeywordAnchored(text)...)

	// Collect already-detected byte ranges to avoid overlap
	covered := make(map[int]bool)
	for _, d := range detections {
		for i := d.Start; i < d.End; i++ {
			covered[i] = true
		}
	}

	// Layer 2 & 3: Entropy + structural analysis on uncovered tokens
	for _, tok := range tokenize(text) {
		if covered[tok.start] {
			continue
		}
		if len(tok.value) < s.MinTokenLength {
			continue
		}
		if isSafePattern(tok.value) {
			continue
		}

		// Layer 2: Entropy
		if shannonEntropy(tok.value) >= s.EntropyThreshold {
			detections = append(detections, Detection{
				Original: tok.value,
				Start:    tok.start,
				End:      tok.start + len(tok.value),
				Layer:    "entropy",
			})
			continue
		}

		// Layer 3: Structural patterns (high char-class diversity)
		if len(tok.value) >= 20 && isHighDiversity(tok.value) {
			detections = append(detections, Detection{
				Original: tok.value,
				Start:    tok.start,
				End:      tok.start + len(tok.value),
				Layer:    "structural",
			})
		}
	}

	return detections
}

// --- Layer 1: Keyword-anchored heuristics ---

// keywordAnchorRe matches labels like "password=X", "token: Y", "secret = 'Z'"
// and captures the value part. Handles =, :, and common assignment patterns.
// The (?:["'] delimiter group is optional so both quoted and unquoted values
// are captured.
var keywordAnchorRe = regexp.MustCompile(
	`(?i)(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential[_-]?secret|client[_-]?secret|app[_-]?secret)` +
		`\s*[:=]\s*` +
		`(?:["']?)` + // optional opening quote
		`(\S+?)` + // the value (non-greedy, non-whitespace)
		`(?:["']?\s|["']?$)`, // closing quote + whitespace or end
)

func (s *SecretScanner) scanKeywordAnchored(text string) []Detection {
	var results []Detection
	for _, match := range keywordAnchorRe.FindAllStringSubmatchIndex(text, -1) {
		if len(match) < 4 {
			continue
		}
		valStart, valEnd := match[2], match[3]
		value := text[valStart:valEnd]

		// Skip very short values (likely not secrets) or placeholder tokens
		if len(value) < 4 {
			continue
		}
		// Skip already-wrapped secrets
		if strings.HasPrefix(value, "<<<") || strings.HasPrefix(value, "{{CREDENTIAL") {
			continue
		}
		// Skip common non-secret values
		if isCommonNonSecret(value) {
			continue
		}

		results = append(results, Detection{
			Original: value,
			Start:    valStart,
			End:      valEnd,
			Layer:    "keyword",
		})
	}
	return results
}

// isCommonNonSecret returns true for values that often appear after labels
// but are not secrets (e.g., variable references, placeholder instructions).
func isCommonNonSecret(s string) bool {
	lower := strings.ToLower(s)
	// Variable references like {password}, ${TOKEN}, $SECRET
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "${") || strings.HasPrefix(s, "$") {
		return true
	}
	// Placeholder instructions
	if strings.HasPrefix(lower, "<") && strings.HasSuffix(lower, ">") {
		return true
	}
	// Common filler words
	for _, skip := range []string{"null", "none", "empty", "undefined", "true", "false", "required", "optional", "your_", "my_"} {
		if strings.HasPrefix(lower, skip) {
			return true
		}
	}
	return false
}

// --- Layer 2: Shannon entropy ---

// shannonEntropy calculates the Shannon entropy in bits per character.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}

	freq := make(map[rune]int)
	total := 0
	for _, r := range s {
		freq[r]++
		total++
	}

	entropy := 0.0
	for _, count := range freq {
		p := float64(count) / float64(total)
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}

// --- Layer 3: Structural patterns ---

// isHighDiversity returns true if the string has high character-class diversity,
// which is typical of generated secrets (mixing upper, lower, digits, special).
// Requires at least 3 of 4 character classes present and a significant mix ratio.
func isHighDiversity(s string) bool {
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case !unicode.IsSpace(r):
			hasSpecial = true
		}
	}

	classes := 0
	if hasUpper {
		classes++
	}
	if hasLower {
		classes++
	}
	if hasDigit {
		classes++
	}
	if hasSpecial {
		classes++
	}

	return classes >= 3
}

// --- Safe pattern exclusions ---

var (
	// UUIDs: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	// URLs without embedded credentials (no @ before the host)
	urlRe = regexp.MustCompile(`^https?://[^@\s]+$`)

	// File paths (unix or windows)
	filePathRe = regexp.MustCompile(`^[/~.]|^[A-Za-z]:\\`)

	// Email addresses
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

	// IPv6 addresses (contains multiple colons)
	ipv6Re = regexp.MustCompile(`^[0-9a-fA-F:]+$`)

	// Docker image digests
	dockerDigestRe = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

	// Semantic version strings (v1.2.3, 1.2.3-beta.4)
	semverRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

	// Pure hex strings of known hash lengths (MD5=32, SHA-1=40, SHA-256=64, SHA-512=128)
	knownHashLengths = map[int]bool{32: true, 40: true, 64: true, 128: true}
	hexRe            = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// isSafePattern returns true if the token matches a known non-secret pattern.
func isSafePattern(s string) bool {
	// Already-wrapped secrets or credential placeholders
	if strings.HasPrefix(s, "<<<") || strings.Contains(s, "{{CREDENTIAL") {
		return true
	}
	// UUIDs
	if uuidRe.MatchString(s) {
		return true
	}
	// URLs without credentials
	if urlRe.MatchString(s) {
		return true
	}
	// File paths
	if filePathRe.MatchString(s) {
		return true
	}
	// Email addresses
	if emailRe.MatchString(s) {
		return true
	}
	// IPv6 addresses (multiple colons)
	if strings.Count(s, ":") >= 2 && ipv6Re.MatchString(s) {
		return true
	}
	// Docker image digests
	if dockerDigestRe.MatchString(s) {
		return true
	}
	// Semantic versions
	if semverRe.MatchString(s) {
		return true
	}
	// Known hash lengths (hex strings of exactly MD5/SHA-1/SHA-256/SHA-512 length)
	if hexRe.MatchString(s) && knownHashLengths[len(s)] {
		return true
	}
	// KEY=VALUE tokens where VALUE is a safe pattern (e.g., OS_AUTH_URL=https://...)
	if eqIdx := strings.Index(s, "="); eqIdx > 0 && eqIdx < len(s)-1 {
		value := s[eqIdx+1:]
		if isSafeValue(value) {
			return true
		}
	}
	return false
}

// isSafeValue checks if a value (the part after = in KEY=VALUE) is safe.
func isSafeValue(s string) bool {
	if urlRe.MatchString(s) {
		return true
	}
	if filePathRe.MatchString(s) {
		return true
	}
	if emailRe.MatchString(s) {
		return true
	}
	if uuidRe.MatchString(s) {
		return true
	}
	if hexRe.MatchString(s) && knownHashLengths[len(s)] {
		return true
	}
	return false
}

// --- Tokenizer ---

type textToken struct {
	value string
	start int
}

// tokenize splits text into whitespace-delimited tokens, preserving byte
// offsets. Tokens are trimmed of common wrapping punctuation (quotes,
// brackets) so that a quoted secret like "sk-abc123" is evaluated as
// sk-abc123.
func tokenize(text string) []textToken {
	var tokens []textToken
	start := -1
	for i, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				tokens = append(tokens, textToken{
					value: text[start:i],
					start: start,
				})
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		tokens = append(tokens, textToken{
			value: text[start:],
			start: start,
		})
	}

	// Strip common wrapping punctuation (but NOT {} which are meaningful
	// for credential placeholders like {{CREDENTIAL:...}})
	for i, tok := range tokens {
		trimmed := strings.Trim(tok.value, `"'`+"`"+"()[]")
		if trimmed != tok.value && len(trimmed) > 0 {
			// Adjust start offset for leading chars stripped
			offset := strings.Index(tok.value, trimmed)
			tokens[i] = textToken{
				value: trimmed,
				start: tok.start + offset,
			}
		}
	}

	return tokens
}
