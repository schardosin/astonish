package llmerror

import (
	"regexp"
	"strconv"
)

// Matches Vertex / SAP AI Core messages like:
//
//	"Unable to submit request because it has a maxOutputTokens value of 64000
//	 but the supported range is from 1 (inclusive) to 32769 (exclusive)."
var maxOutputRangeRe = regexp.MustCompile(
	`(?i)maxOutputTokens\s+value\s+of\s+(\d+).*?supported\s+range\s+is\s+from\s+(\d+)\s+\((inclusive|exclusive)\)\s+to\s+(\d+)\s+\((inclusive|exclusive)\)`,
)

// ParseMaxOutputTokensLimit extracts the maximum allowed maxOutputTokens from
// a provider error body. Returns (max, true) when the body describes a
// supported range; otherwise (0, false).
//
// For an exclusive upper bound B, the allowed max is B-1.
// For an inclusive upper bound B, the allowed max is B.
func ParseMaxOutputTokensLimit(body string) (int, bool) {
	m := maxOutputRangeRe.FindStringSubmatch(body)
	if m == nil {
		return 0, false
	}
	upper, err := strconv.Atoi(m[4])
	if err != nil || upper <= 0 {
		return 0, false
	}
	upperKind := m[5]
	max := upper
	if upperKind == "exclusive" {
		max = upper - 1
	}
	if max <= 0 {
		return 0, false
	}
	return max, true
}
