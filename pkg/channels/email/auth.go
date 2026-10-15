package email

import (
	"net/mail"
	"strconv"
	"strings"

	emailpkg "github.com/SAP/astonish/pkg/email"
)

// Receiver identity is a configuration trust boundary, not a signature. The
// receiver MUST remove externally supplied headers claiming its authserv-id.
func authenticatedSender(msg *emailpkg.Message, trustedID string) (string, bool) {
	if msg == nil || !validDomain(trustedID) {
		return "", false
	}
	from := preservedValues(msg, "From")
	if len(from) != 1 {
		return "", false
	}
	// Conservatively reject group delimiters, even inside display names/comments.
	// net/mail also accepts single-member groups in ParseAddress.
	if strings.ContainsAny(from[0], ":;") {
		return "", false
	}
	mailbox, err := mail.ParseAddress(from[0])
	if err != nil {
		return "", false
	}
	address := strings.ToLower(mailbox.Address)
	at := strings.LastIndexByte(address, '@')
	if at <= 0 || !validDomain(address[at+1:]) {
		return "", false
	}
	values := preservedValues(msg, "Authentication-Results")
	if len(values) == 0 {
		return "", false
	}
	trustedCount := 0
	topPass := false
	for i, value := range values {
		id, pass, ok := parseAuthResults(value, address[at+1:])
		if !ok {
			return "", false
		}
		if strings.EqualFold(id, trustedID) {
			trustedCount++
			if i == 0 {
				topPass = pass
			}
		}
	}
	if trustedCount != 1 || !topPass {
		return "", false
	}
	return address, true
}

// providerTrustedSender extracts a sender only when the original message has one
// unambiguous, syntactically valid From mailbox. Authentication itself is
// delegated to the administrator's mailbox-protection policy.
func providerTrustedSender(msg *emailpkg.Message) (string, bool) {
	if msg == nil {
		return "", false
	}
	from := preservedValues(msg, "From")
	if len(from) != 1 || strings.ContainsAny(from[0], ":;") {
		return "", false
	}
	mailbox, err := mail.ParseAddress(from[0])
	if err != nil {
		return "", false
	}
	address := strings.ToLower(mailbox.Address)
	at := strings.LastIndexByte(address, '@')
	if at <= 0 || !validDomain(address[at+1:]) {
		return "", false
	}
	return address, true
}

func preservedValues(msg *emailpkg.Message, name string) []string {
	var values []string
	found := false
	// Providers canonicalize keys. Fail closed on manually supplied mixed-case
	// aliases as well: their relative occurrence order is unknowable.
	for key, v := range msg.HeaderValues {
		if strings.EqualFold(key, name) {
			if found {
				return nil
			}
			found = true
			values = v
		}
	}
	return values
}

func validDomain(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

type authToken struct {
	text   string
	quoted bool
}

// authTokens implements a conservative ASCII RFC 8601 subset: CFWS (including
// nested/escaped comments), quoted values, atoms, semicolons and equals signs.
// Unsupported or malformed syntax is rejected, never searched as substrings.
func authTokens(s string) ([]authToken, bool) {
	if len(s) > 65536 {
		return nil, false
	}
	var tokens []authToken
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case ' ', '\t':
			i++
		case '\r':
			if i+2 >= len(s) || s[i+1] != '\n' || (s[i+2] != ' ' && s[i+2] != '\t') {
				return nil, false
			}
			i += 2
		case '(':
			depth := 1
			i++
			for i < len(s) && depth > 0 {
				switch s[i] {
				case '\\':
					i++
					if i >= len(s) || s[i] < 32 || s[i] > 126 {
						return nil, false
					}
				case '(':
					depth++
					if depth > 16 {
						return nil, false
					}
				case ')':
					depth--
				case '\r':
					if i+2 >= len(s) || s[i+1] != '\n' || (s[i+2] != ' ' && s[i+2] != '\t') {
						return nil, false
					}
					i++
				default:
					if s[i] != '\t' && (s[i] < 32 || s[i] > 126) {
						return nil, false
					}
				}
				i++
			}
			if depth != 0 {
				return nil, false
			}
		case '"':
			i++
			var b strings.Builder
			closed := false
			for i < len(s) {
				c = s[i]
				i++
				if c == '"' {
					closed = true
					break
				}
				if c == '\\' {
					if i >= len(s) {
						return nil, false
					}
					c = s[i]
					i++
				}
				if c < 32 || c > 126 {
					return nil, false
				}
				b.WriteByte(c)
			}
			if !closed {
				return nil, false
			}
			tokens = append(tokens, authToken{b.String(), true})
		case ';', '=':
			tokens = append(tokens, authToken{text: string(c)})
			i++
		default:
			start := i
			for i < len(s) && !strings.ContainsRune(" \t\r\n();=\"", rune(s[i])) {
				if s[i] < 33 || s[i] > 126 || s[i] == '\\' {
					return nil, false
				}
				i++
			}
			if start == i {
				return nil, false
			}
			tokens = append(tokens, authToken{text: s[start:i]})
		}
	}
	return tokens, true
}

func authName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func parseAuthResults(s, domain string) (string, bool, bool) {
	t, ok := authTokens(s)
	if !ok || len(t) < 3 || t[0].quoted || !validDomain(t[0].text) {
		return "", false, false
	}
	id := t[0].text
	i := 1
	// Optional authres-version.
	if i < len(t) && t[i].text != ";" {
		if t[i].quoted {
			return id, false, false
		}
		if _, err := strconv.ParseUint(t[i].text, 10, 32); err != nil {
			return id, false, false
		}
		i++
	}
	if i >= len(t) || t[i].quoted || t[i].text != ";" {
		return id, false, false
	}
	i++
	if i+1 == len(t) && !t[i].quoted && strings.EqualFold(t[i].text, "none") {
		return id, false, true
	}
	passed := false
	dmarcSeen := false
	for i < len(t) {
		method := strings.ToLower(t[i].text)
		if t[i].quoted {
			return id, false, false
		}
		parts := strings.Split(method, "/")
		if !authName(parts[0]) || len(parts) > 2 {
			return id, false, false
		}
		if len(parts) == 2 {
			if _, err := strconv.ParseUint(parts[1], 10, 32); err != nil {
				return id, false, false
			}
		}
		method = parts[0]
		i++
		if i+1 >= len(t) || t[i].quoted || t[i].text != "=" || t[i+1].quoted || !authName(t[i+1].text) {
			return id, false, false
		}
		result := strings.ToLower(t[i+1].text)
		i += 2
		properties := map[string]string{}
		for i < len(t) && t[i].text != ";" {
			key := strings.ToLower(t[i].text)
			if t[i].quoted {
				return id, false, false
			}
			p := strings.Split(key, ".")
			if key != "reason" && (len(p) != 2 || !authName(p[0]) || !authName(p[1])) {
				return id, false, false
			}
			if i+2 >= len(t) || t[i+1].quoted || t[i+1].text != "=" || t[i+2].text == ";" || t[i+2].text == "=" || t[i+2].text == "" {
				return id, false, false
			}
			if _, exists := properties[key]; exists {
				return id, false, false
			}
			properties[key] = t[i+2].text
			i += 3
		}
		if method == "dmarc" {
			if dmarcSeen {
				return id, false, false
			}
			dmarcSeen = true
			passed = result == "pass" && validDomain(properties["header.from"]) && strings.EqualFold(properties["header.from"], domain)
		}
		if i < len(t) {
			if t[i].quoted {
				return id, false, false
			}
			i++
			if i == len(t) {
				return id, false, false
			}
		}
	}
	return id, passed, true
}
