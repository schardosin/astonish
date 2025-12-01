package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/schardosin/astonish/pkg/config"
)

// Define styles
var (
	// Colors
	colorLLM    = lipgloss.Color("63")  // Blueish
	colorTool   = lipgloss.Color("214") // Orange
	colorInput  = lipgloss.Color("86")  // Cyan
	colorState  = lipgloss.Color("204") // Pink
	colorGray   = lipgloss.Color("240")
	
	// Node Box Style
	nodeStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Width(24).                  // Fixed width for alignment
			Align(lipgloss.Center).     // Center text within the box
			MarginLeft(3)               // Fixed left margin

	// Connector Styles
	arrowStyle = lipgloss.NewStyle().Foreground(colorGray).Bold(true).MarginLeft(16) // Align with center of box (3 margin + 1 border + 12 half-width)
	lineStyle  = lipgloss.NewStyle().Foreground(colorGray)
	
	// Branching Styles
	conditionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Italic(true)
)

// RenderCharmFlow prints the flow using Lipgloss styles
func RenderCharmFlow(cfg *config.AgentConfig) {
	fmt.Println()
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("63")).
		Padding(0, 1).
		Render(" 🔮 ASTONISH FLOW: " + cfg.Description + " ")
	fmt.Println(title)
	fmt.Println()

	// 1. Render Start
	printNode("START", "system", false)

	// 2. Walk the graph
	currentNode := "START"
	maxSteps := 20
	step := 0

	for step < maxSteps {
		// Find edges
		var nextNode string
		var edges []config.Edge
		var targetNode *config.Node

		found := false
		for _, flow := range cfg.Flow {
			if flow.From == currentNode {
				if flow.To != "" {
					nextNode = flow.To
				}
				edges = flow.Edges
				found = true
				break
			}
		}

		if !found && currentNode != "START" {
			break
		}

		// Handle Start Transition
		if currentNode == "START" {
			if nextNode != "" {
				printArrow()
				currentNode = nextNode
				targetNode = getNode(cfg, currentNode)
				printNode(currentNode, targetNode.Type, false)
				continue
			}
		}

		// Draw Edges/Branches
		if len(edges) > 0 {
			printBranching(edges)
			// Branching usually means logical divergence. 
			// For a linear visualizer, we stop here or pick the first path?
			// Let's print a "End of linear trace" indicator if it branches complexly
			fmt.Println(lipgloss.NewStyle().Foreground(colorGray).MarginLeft(6).Render("(Complex branching logic)"))
			break
		} else {
			printArrow()
		}

		if nextNode == "END" || nextNode == "" {
			printNode("END", "system", true)
			break
		}

		// Move next
		currentNode = nextNode
		targetNode = getNode(cfg, currentNode)
		
		nodeType := "unknown"
		if targetNode != nil {
			nodeType = targetNode.Type
		}
		
		printNode(currentNode, nodeType, false)
		step++
	}
	fmt.Println()
}

// --- Visual Helpers ---

func printNode(name, nodeType string, isEnd bool) {
	var borderColor lipgloss.Color
	var icon string

	switch nodeType {
	case "llm":
		borderColor = colorLLM
		icon = "🤖"
	case "tool":
		borderColor = colorTool
		icon = "🛠️ "
	case "input":
		borderColor = colorInput
		icon = "📥"
	case "update_state":
		borderColor = colorState
		icon = "💾"
	case "system":
		borderColor = lipgloss.Color("255")
		icon = "⚡"
	default:
		borderColor = colorGray
		icon = "📦"
	}

	if isEnd {
		borderColor = lipgloss.Color("196") // Red
		icon = "🏁"
	}

	// Render the box
	// Note: MarginLeft is already in the style
	box := nodeStyle.Copy().
		BorderForeground(borderColor).
		Render(fmt.Sprintf("%s %s", icon, name))

	fmt.Println(box)
}

func printArrow() {
	fmt.Println(arrowStyle.Render("↓"))
}

func printBranching(edges []config.Edge) {
	// Draw a little tree for branches
	// Align with center: 16 spaces
	fmt.Println(lineStyle.Render("                │"))
	
	for i, edge := range edges {
		connector := "├──"
		if i == len(edges)-1 {
			connector = "└──"
		}
		
		// Truncate condition if too long
		cond := edge.Condition
		if len(cond) > 30 {
			cond = cond[:27] + "..."
		}
		
		// 16 spaces indentation for the tree structure
		line := fmt.Sprintf("                %s %s ➜ %s", connector, conditionStyle.Render("["+cond+"]"), edge.To)
		fmt.Println(lineStyle.Render(line))
	}
}

func getNode(cfg *config.AgentConfig, name string) *config.Node {
	for i := range cfg.Nodes {
		if cfg.Nodes[i].Name == name {
			return &cfg.Nodes[i]
		}
	}
	return nil
}
