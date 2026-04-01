package agent

import "regexp"

// curlyPlaceholder matches {variable} patterns that ADK's InjectSessionState
// would try to resolve as session state keys. We escape them to <variable>.
var curlyPlaceholder = regexp.MustCompile(`\{([^{}]+)\}`)

// EscapeCurlyPlaceholders replaces {variable} patterns with <variable> to
// prevent ADK's session state resolver from treating them as state keys.
func EscapeCurlyPlaceholders(s string) string {
	return curlyPlaceholder.ReplaceAllString(s, "<$1>")
}
