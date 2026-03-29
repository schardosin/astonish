// Package drill provides the deterministic step execution engine for Astonish.
// A "drill" is an AI-composed, mechanically-replayed sequence of tool calls
// with assertions and reporting. Drills are used for tests, health checks,
// deployment verification, and any repeatable multi-step automation.
package drill

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
)

// AssertionResult holds the outcome of a single assertion evaluation.
type AssertionResult struct {
	Passed   bool   `json:"passed"`
	Type     string `json:"type"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message,omitempty"`
}

// Evaluate runs a single assertion against the provided content string.
// For most assertion types this is a pure, deterministic check (no LLM).
// The "semantic" type returns a placeholder — it requires external LLM evaluation.
func Evaluate(assert *config.AssertConfig, content string) *AssertionResult {
	if assert == nil {
		return &AssertionResult{Passed: true, Message: "no assertion defined"}
	}

	result := &AssertionResult{
		Type:     assert.Type,
		Expected: assert.Expected,
		Actual:   truncateStr(content, 1000),
	}

	switch assert.Type {
	case "contains":
		result.Passed = strings.Contains(content, assert.Expected)
		if !result.Passed {
			result.Message = fmt.Sprintf("expected output to contain %q", assert.Expected)
		}

	case "not_contains":
		result.Passed = !strings.Contains(content, assert.Expected)
		if !result.Passed {
			result.Message = fmt.Sprintf("expected output NOT to contain %q", assert.Expected)
		}

	case "regex":
		re, err := regexp.Compile(assert.Expected)
		if err != nil {
			result.Passed = false
			result.Message = fmt.Sprintf("invalid regex pattern: %v", err)
		} else {
			result.Passed = re.MatchString(content)
			if !result.Passed {
				result.Message = fmt.Sprintf("output did not match pattern %q", assert.Expected)
			}
		}

	case "exit_code":
		expected, err := strconv.Atoi(assert.Expected)
		if err != nil {
			result.Passed = false
			result.Message = fmt.Sprintf("invalid expected exit code: %q", assert.Expected)
		} else {
			actual, err := strconv.Atoi(strings.TrimSpace(content))
			if err != nil {
				result.Passed = false
				result.Message = fmt.Sprintf("could not parse exit code from output: %q", truncateStr(content, 100))
			} else {
				result.Passed = actual == expected
				if !result.Passed {
					result.Message = fmt.Sprintf("expected exit code %d, got %d", expected, actual)
				}
			}
		}

	case "element_exists":
		result.Passed = strings.Contains(content, assert.Expected)
		if !result.Passed {
			result.Message = fmt.Sprintf("element with text %q not found in accessibility snapshot", assert.Expected)
		}

	case "semantic":
		// Semantic assertions require LLM evaluation — not available in Phase 1.
		// The caller must handle this type separately.
		result.Passed = false
		result.Message = "semantic assertion requires LLM evaluation (not available in deterministic runner)"

	default:
		result.Passed = false
		result.Message = fmt.Sprintf("unknown assertion type: %q", assert.Type)
	}

	return result
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
