package ui

import (
	"strings"
	"testing"
)

func TestRenderRetryBadge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		attempt    int
		maxRetries int
		oneLiner   string
		wantParts  []string
	}{
		{
			name:       "first_retry",
			attempt:    1,
			maxRetries: 3,
			oneLiner:   "rate limited",
			wantParts:  []string{"1/3", "rate limited"},
		},
		{
			name:       "last_retry",
			attempt:    3,
			maxRetries: 3,
			oneLiner:   "timeout",
			wantParts:  []string{"3/3", "timeout"},
		},
		{
			name:       "empty_message",
			attempt:    1,
			maxRetries: 5,
			oneLiner:   "",
			wantParts:  []string{"1/5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderRetryBadge(tt.attempt, tt.maxRetries, tt.oneLiner)
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("RenderRetryBadge(%d, %d, %q) missing %q, got %q",
						tt.attempt, tt.maxRetries, tt.oneLiner, part, got)
				}
			}
		})
	}
}

func TestRenderRetryBadge_ContainsRetryIcon(t *testing.T) {
	t.Parallel()
	got := RenderRetryBadge(1, 3, "test")
	// The retry icon is a unicode character
	if !strings.Contains(got, "Retry") {
		t.Errorf("expected 'Retry' text in badge, got %q", got)
	}
}

func TestRenderErrorBox_AllSections(t *testing.T) {
	t.Parallel()
	got := RenderErrorBox("Test Error", "Something broke", "Try again", "raw: connection refused")
	wantParts := []string{"Test Error", "Something broke", "Suggestion:", "Try again", "Raw Error:", "connection refused"}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Errorf("RenderErrorBox missing %q in output", part)
		}
	}
}

func TestRenderErrorBox_ReasonOnly(t *testing.T) {
	t.Parallel()
	got := RenderErrorBox("Err", "reason text", "", "")
	if !strings.Contains(got, "reason text") {
		t.Errorf("expected reason text in output, got %q", got)
	}
	if strings.Contains(got, "Suggestion:") {
		t.Errorf("should not have Suggestion section when empty")
	}
	if strings.Contains(got, "Raw Error:") {
		t.Errorf("should not have Raw Error section when empty")
	}
}

func TestRenderErrorBox_SuggestionOnly(t *testing.T) {
	t.Parallel()
	got := RenderErrorBox("Err", "", "do this instead", "")
	if !strings.Contains(got, "Suggestion:") {
		t.Errorf("expected 'Suggestion:' in output, got %q", got)
	}
	if !strings.Contains(got, "do this instead") {
		t.Errorf("expected suggestion text in output, got %q", got)
	}
}

func TestRenderErrorBox_OriginalErrorOnly(t *testing.T) {
	t.Parallel()
	got := RenderErrorBox("Err", "", "", "ECONNREFUSED")
	if !strings.Contains(got, "Raw Error:") {
		t.Errorf("expected 'Raw Error:' in output, got %q", got)
	}
	if !strings.Contains(got, "ECONNREFUSED") {
		t.Errorf("expected raw error text in output, got %q", got)
	}
}

func TestRenderErrorBox_StartsAndEndsWithNewline(t *testing.T) {
	t.Parallel()
	got := RenderErrorBox("Title", "reason", "", "")
	if !strings.HasPrefix(got, "\n") {
		t.Errorf("expected output to start with newline")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected output to end with newline")
	}
}

func TestRenderErrorBox_TitleWithIcon(t *testing.T) {
	t.Parallel()
	got := RenderErrorBox("Connection Failed", "", "", "")
	if !strings.Contains(got, "Connection Failed") {
		t.Errorf("expected title in output, got %q", got)
	}
}

func TestRenderMaxRetriesBox(t *testing.T) {
	t.Parallel()
	got := RenderMaxRetriesBox(5, "timeout after 30s")
	wantParts := []string{"Max Retries Reached", "5 attempts", "timeout after 30s"}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Errorf("RenderMaxRetriesBox missing %q in output, got %q", part, got)
		}
	}
}

func TestRenderMaxRetriesBox_EmptyError(t *testing.T) {
	t.Parallel()
	got := RenderMaxRetriesBox(3, "")
	if !strings.Contains(got, "3 attempts") {
		t.Errorf("expected '3 attempts' in output, got %q", got)
	}
	if strings.Contains(got, "Raw Error:") {
		t.Errorf("should not have Raw Error section when empty")
	}
}
