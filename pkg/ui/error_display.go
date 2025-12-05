package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ANSI color codes
const (
	ansiRed    = "\033[1;38;5;196m"
	ansiOrange = "\033[38;5;208m"
	ansiYellow = "\033[38;5;226m"
	ansiGrey   = "\033[38;5;240m"
	ansiReset  = "\033[0m"
)

var (
	// Lipgloss styles for retry badge (kept for consistency)
	colorOrange = lipgloss.Color("#FFA500")
	colorGrey   = lipgloss.Color("240")
	
	retryBadgeStyle = lipgloss.NewStyle().
			Foreground(colorOrange).
			Bold(true)

	retryMessageStyle = lipgloss.NewStyle().
			Foreground(colorGrey).
			PaddingLeft(1)
)

// RenderRetryBadge: Clean text-only line
// Output: ⟳ Retry 1/3: Rate limit exceeded
func RenderRetryBadge(attempt, maxRetries int, oneLiner string) string {
	badge := retryBadgeStyle.Render(fmt.Sprintf("⟳ Retry %d/%d:", attempt, maxRetries))
	message := retryMessageStyle.Render(oneLiner)
	return lipgloss.JoinHorizontal(lipgloss.Left, badge, message)
}

// RenderErrorBox: Simple text block with ANSI colors
func RenderErrorBox(title, reason, suggestion, originalError string) string {
	var output strings.Builder
	
	// Empty line at the beginning for visual separation
	output.WriteString("\n")
	
	// 1. Header with red color
	output.WriteString(fmt.Sprintf("%s✕ %s%s\n", ansiRed, title, ansiReset))
	output.WriteString("\n")
	
	// 2. Reason (white/default color, no special formatting needed)
	if reason != "" {
		output.WriteString(fmt.Sprintf("%s\n", reason))
	}
	
	// 3. Suggestion (yellow)
	if suggestion != "" {
		output.WriteString(fmt.Sprintf("\n%sSuggestion:%s\n", ansiYellow, ansiReset))
		output.WriteString(fmt.Sprintf("%s%s%s\n", ansiYellow, suggestion, ansiReset))
	}
	
	// 4. Raw Error (grey, dimmed)
	if originalError != "" {
		output.WriteString(fmt.Sprintf("\n%sRaw Error:%s\n", ansiGrey, ansiReset))
		output.WriteString(fmt.Sprintf("%s%s%s\n", ansiGrey, cleanError(originalError), ansiReset))
	}
	
	// Empty line at the end for visual separation
	output.WriteString("\n")
	
	return output.String()
}

func RenderMaxRetriesBox(attempts int, originalError string) string {
	return RenderErrorBox(
		"Max Retries Reached", 
		fmt.Sprintf("Stopped after %d attempts. The error persisted.", attempts),
		"",
		originalError,
	)
}

func cleanError(err string) string {
	return strings.TrimSpace(err)
}