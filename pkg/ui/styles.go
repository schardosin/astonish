package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderToolBox renders a styled box for tool execution approval.
func RenderToolBox(toolName string, args map[string]interface{}) string {
	// Define styles
	boxStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")). // Purple border
		Padding(0, 2).                          // Reduced vertical padding
		Width(60)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")). // Cyan
		Bold(true)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")) // Gray

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")) // White

	// Build content
	var content strings.Builder
	
	content.WriteString(fmt.Sprintf("%s %s", titleStyle.Render("Tool:"), valueStyle.Render(toolName)))
	
	if len(args) > 0 {
		content.WriteString("\n\n") // Spacer
		content.WriteString(titleStyle.Render("** Arguments **"))
		content.WriteString("\n")
		
		for k, v := range args {
			valStr := fmt.Sprintf("%v", v)
			// Truncate long values
			if len(valStr) > 200 {
				valStr = valStr[:197] + "..."
			}
			content.WriteString(fmt.Sprintf("%s: %s\n", keyStyle.Render(k), valueStyle.Render(valStr)))
		}
	}

	return boxStyle.Render(strings.TrimSpace(content.String())) + "\n"
}

// RenderStatusBadge renders a styled status badge (e.g. "✓ Command approved")
func RenderStatusBadge(text string, success bool) string {
	var icon string
	var iconColor lipgloss.Color

	if success {
		icon = "✓"
		iconColor = lipgloss.Color("42") // Green
	} else {
		icon = "✗"
		iconColor = lipgloss.Color("196") // Red
	}

	checkStyle := lipgloss.NewStyle().Foreground(iconColor).SetString(icon)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Grey text

	return checkStyle.String() + " " + textStyle.Render(text)
}
