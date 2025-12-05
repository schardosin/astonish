package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// --- COLORS ---
	// Using a slightly softer red for the border to be less aggressive
	colorRed    = lipgloss.Color("196") 
	colorOrange = lipgloss.Color("#FFA500")
	colorYellow = lipgloss.Color("226")
	colorWhite  = lipgloss.Color("252")
	colorGrey   = lipgloss.Color("240")
	
	// --- DIMENSIONS ---
	// Reduced width slightly to prevent border wrapping/leaking artifacts
	boxWidth     = 68
	contentWidth = boxWidth - 4 

	// --- STYLES ---

	// 1. Retry Style (Text Only, No Background)
	// Old: [ ORANGE ]
	// New: ⟳ Retry 1/3
	retryBadgeStyle = lipgloss.NewStyle().
			Foreground(colorOrange).
			Bold(true)

	retryMessageStyle = lipgloss.NewStyle().
			Foreground(colorGrey).
			PaddingLeft(1)

	// 2. Failure Box (Cleaner)
	errorBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorRed).
			Padding(0, 1).
			Width(boxWidth)

	// Headers
	errorHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorRed)

	// Content Text
	reasonStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Width(contentWidth)

	// Suggestion (Simpler, no icon)
	suggestionTitleStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true).
			MarginTop(1)

	suggestionBodyStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Width(contentWidth)

	// Raw Error (Dimmed, minimal)
	originalErrorTitleStyle = lipgloss.NewStyle().
			Foreground(colorGrey).
			Bold(true).
			MarginTop(1)

	originalErrorBodyStyle = lipgloss.NewStyle().
			Foreground(colorGrey).
			Width(contentWidth)
)

// RenderRetryBadge: Clean text-only line
// Output: ⟳ Retry 1/3: Rate limit exceeded
func RenderRetryBadge(attempt, maxRetries int, oneLiner string) string {
	badge := retryBadgeStyle.Render(fmt.Sprintf("⟳ Retry %d/%d:", attempt, maxRetries))
	message := retryMessageStyle.Render(oneLiner)
	return lipgloss.JoinHorizontal(lipgloss.Left, badge, message)
}

// RenderErrorBox: Simplified card without dividers
func RenderErrorBox(title, reason, suggestion, originalError string) string {
	// 1. Header (e.g. "FAILURE: Validation Error")
	header := errorHeaderStyle.Render(fmt.Sprintf("✕ %s", title))

	// 2. Build blocks (Vertical stack)
	blocks := []string{header, lipgloss.NewStyle().Height(1).Render("")}

	// Reason
	if reason != "" {
		blocks = append(blocks, reasonStyle.Render(reason))
	}

	// Suggestion
	if suggestion != "" {
		blocks = append(blocks, 
			suggestionTitleStyle.Render("Suggestion:"), // Removed emoji
			suggestionBodyStyle.Render(suggestion),
		)
	}

	// Original Error (Technical details)
	if originalError != "" {
		// Add a tiny vertical gap before the raw error to separate it visually
		blocks = append(blocks, 
			lipgloss.NewStyle().Height(1).Render(""),
			originalErrorTitleStyle.Render("Raw Error:"),
			originalErrorBodyStyle.Render(cleanError(originalError)),
		)
	}

	// 3. Render
	content := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return errorBoxStyle.Render(content)
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