package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color definitions
var (
	// Purple theme for Agent
	PurpleColor = lipgloss.Color("#9D4EDD")
	CyanColor   = lipgloss.Color("#00D9FF")
	YellowColor = lipgloss.Color("#FFD700")
	GreenColor  = lipgloss.Color("#00FF00")
	
	// Define the border type once to ensure consistency
	roundedBorder = lipgloss.RoundedBorder()
	
	// Action box style with cyan borders
	ActionBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(CyanColor).
		Padding(0, 1).
		MarginTop(1).
		MarginBottom(1)
	
	// Action header style
	ActionHeaderStyle = lipgloss.NewStyle().
		Foreground(CyanColor).
		Bold(true)
)

// RenderAgentBox renders a message in a box with the title embedded in the top rounded border.
func RenderAgentBox(content string) string {
	if content == "" {
		return ""
	}
	content = strings.TrimSpace(content)

	// --- Styles ---

	// 1. The style for the title text itself (purple and bold)
	titleStyle := lipgloss.NewStyle().
		Foreground(PurpleColor).
		Bold(true)

	// 2. The style for the border lines (just the purple color)
	borderStyle := lipgloss.NewStyle().
		Foreground(PurpleColor)

	// 3. The style for the box content area.
	// IMPORTANT: We disable BorderTop because we will manually create it.
	boxBodyStyle := lipgloss.NewStyle().
		Border(roundedBorder).
		BorderForeground(PurpleColor).
		BorderTop(false). // <--- Key change: Turn off standard top border
		Padding(1, 2).    // Add nice padding inside
		Margin(0, 0, 1, 0) // Add margin bottom only

	// --- Rendering Steps ---

	// Step 1: Render the main body content box to determine its width.
	renderedBody := boxBodyStyle.Render(content)
	boxWidth := lipgloss.Width(renderedBody)

	// Step 2: Render the styled title with spaces around it.
	renderedTitle := titleStyle.Render(" 🤖 Agent ")
	titleWidth := lipgloss.Width(renderedTitle)

	// Step 3: Manually construct the top border line.
	// We calculate how many horizontal border characters go on the left and right of the title.

	// Get the raw border characters
	topLeftChar := roundedBorder.TopLeft
	topRightChar := roundedBorder.TopRight
	horizChar := roundedBorder.Top

	// Calculate lengths for horizontal segments
	// We want the title slightly offset from the left (e.g., 1 character in)
	leftGapSize := 1
	// The right gap is total width minus corners (2), left gap, and title width.
	rightGapSize := boxWidth - 2 - leftGapSize - titleWidth

	// Ensure rightGapSize isn't negative if title is very long
	if rightGapSize < 0 {
		rightGapSize = 0
	}

	// Build the top line string
	topLine := borderStyle.Render(topLeftChar) +
		borderStyle.Render(strings.Repeat(horizChar, leftGapSize)) +
		renderedTitle +
		borderStyle.Render(strings.Repeat(horizChar, rightGapSize)) +
		borderStyle.Render(topRightChar)

	// Step 4: Stack the manual top line exactly on top of the body box.
	return lipgloss.JoinVertical(lipgloss.Left, topLine, renderedBody)
}

// RenderActionBox renders an Action message in a cyan-bordered box with icon
func RenderActionBox(title string, content string) string {
	if content == "" {
		return ""
	}
	
	// Clean up the content
	content = strings.TrimSpace(content)
	
	// Create header with wrench icon
	header := ActionHeaderStyle.Render("🔧 " + title)
	
	// Combine header and content with a newline
	fullContent := header + "\n" + content
	
	// Render in the styled box
	return ActionBoxStyle.Render(fullContent)
}

// RenderSimpleBox renders content in a box with custom color
func RenderSimpleBox(content string, borderColor lipgloss.Color) string {
	if content == "" {
		return ""
	}
	
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		MarginTop(1).
		MarginBottom(1)
	
	return style.Render(strings.TrimSpace(content))
}
