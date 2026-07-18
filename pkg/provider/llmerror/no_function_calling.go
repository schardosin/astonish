package llmerror

import "strings"

// IsNoFunctionCalling reports whether a provider error body indicates the
// model rejects function calling / tools (Vertex / SAP AI Core).
func IsNoFunctionCalling(body string) bool {
	return strings.Contains(strings.ToLower(body), "does not support function calling")
}
