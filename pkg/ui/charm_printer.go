package ui

import (
	"fmt"
	"strings"

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
			Width(24).
			Align(lipgloss.Center).
			MarginLeft(0) // Margin handled by layout

	// Branching Styles
	conditionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Italic(true)
)

// Canvas represents a 2D grid of characters
type Canvas struct {
	grid   [][]string
	width  int
	height int
}

func NewCanvas(width, height int) *Canvas {
	grid := make([][]string, height)
	for i := range grid {
		grid[i] = make([]string, width)
		for j := range grid[i] {
			grid[i][j] = " "
		}
	}
	return &Canvas{grid: grid, width: width, height: height}
}

func (c *Canvas) Set(x, y int, char string) {
	if x >= 0 && x < c.width && y >= 0 && y < c.height {
		c.grid[y][x] = char
	}
}

// DrawRenderedString draws a multi-line string (potentially with ANSI) onto the canvas
func (c *Canvas) DrawRenderedString(x, y int, s string) {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if y+i < c.height && x < c.width {
			c.grid[y+i][x] = line
			
			// Clear subsequent cells covered by this string
			w := lipgloss.Width(line)
			for k := 1; k < w; k++ {
				if x+k < c.width {
					c.grid[y+i][x+k] = "" // Empty string to skip rendering
				}
			}
			// Explicitly clear the cell immediately after the string visual width
			if x+w < c.width {
				c.grid[y+i][x+w] = ""
			}
		}
	}
}

func (c *Canvas) Render() string {
	var sb strings.Builder
	for _, row := range c.grid {
		for _, cell := range row {
			sb.WriteString(cell)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// DrawLine draws an orthogonal line
func (c *Canvas) DrawLine(x1, y1, x2, y2 int, arrow bool) {
	// Simple Manhattan routing: Vertical -> Horizontal -> Vertical
	// Midpoint Y
	midY := (y1 + y2) / 2
	
	// Draw Vertical 1
	c.DrawVerticalLine(x1, y1, midY)
	
	// Draw Horizontal
	c.DrawHorizontalLine(x1, x2, midY)
	
	// Draw Vertical 2
	c.DrawVerticalLine(x2, midY, y2)
	
	// Corners
	c.Set(x1, midY, "┼") // Intersection
	c.Set(x2, midY, "┼")
	
	// Clean up corners
	if x1 == x2 {
		// Straight vertical line
		c.DrawVerticalLine(x1, y1, y2)
	} else {
		// Proper corners
		// y1 is top (source bottom), midY is below. So y1 < midY.
		// Source (x1, y1) -> Down to midY.
		
		// Corner 1 at (x1, midY)
		if midY > y1 { // Came from top
			if x2 > x1 { c.Set(x1, midY, "└") } else { c.Set(x1, midY, "┘") }
		} else { // Came from bottom (back edge?)
			if x2 > x1 { c.Set(x1, midY, "┌") } else { c.Set(x1, midY, "┐") }
		}
		
		// Corner 2 at (x2, midY)
		if y2 > midY { // Going down
			if x1 < x2 { c.Set(x2, midY, "┐") } else { c.Set(x2, midY, "┌") }
		} else { // Going up
			if x1 < x2 { c.Set(x2, midY, "┘") } else { c.Set(x2, midY, "└") }
		}
	}
	
	// Arrow head
	if arrow {
		c.Set(x2, y2-1, "↓") // Just above target
	}
}

func (c *Canvas) DrawVerticalLine(x, y1, y2 int) {
	start, end := y1, y2
	if y1 > y2 { start, end = y2, y1 }
	
	// DEBUG
	fmt.Printf("DEBUG: DrawVerticalLine x=%d y=%d-%d\n", x, start, end)

	for y := start; y <= end; y++ {
		c.Set(x, y, "│")
	}
}

// ... (rest of file)

	// 3. Layout Dimensions

func (c *Canvas) DrawHorizontalLine(x1, x2, y int) {
	start, end := x1, x2
	if x1 > x2 { start, end = x2, x1 }
	for x := start; x <= end; x++ {
		c.Set(x, y, "─")
	}
}

// RenderCharmFlow prints the flow using Column-Based Layout
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

	// 1. Build Graph Structure
	nodes := make(map[string]*config.Node)
	adj := make(map[string][]string)
	
	// Add START and END
	nodes["START"] = &config.Node{Name: "START", Type: "system"}
	nodes["END"] = &config.Node{Name: "END", Type: "system"}
	
	for i := range cfg.Nodes {
		n := &cfg.Nodes[i]
		nodes[n.Name] = n
	}
	
	for _, flow := range cfg.Flow {
		from := flow.From
		targets := []string{}
		if flow.To != "" {
			targets = append(targets, flow.To)
		}
		for _, edge := range flow.Edges {
			targets = append(targets, edge.To)
		}
		
		for _, to := range targets {
			adj[from] = append(adj[from], to)
		}
	}

	// 2. Identify Main Path (Longest Path from START to END)
	// Simple DFS to find longest path
	var mainPath []string
	var maxLen int
	
	var findLongestPath func(u string, path []string)
	findLongestPath = func(u string, path []string) {
		path = append(path, u)
		if u == "END" {
			if len(path) > maxLen {
				maxLen = len(path)
				mainPath = make([]string, len(path))
				copy(mainPath, path)
			}
			return
		}
		
		// Avoid cycles in path finding
		for _, v := range adj[u] {
			isVisited := false
			for _, p := range path {
				if p == v { isVisited = true; break }
			}
			if !isVisited {
				findLongestPath(v, path)
			}
		}
	}
	findLongestPath("START", []string{})
	
	// Map nodes to columns
	nodeCols := make(map[string]int)
	nodeRows := make(map[string]int)
	
	// Assign Main Path to Column 0
	mainPathSet := make(map[string]bool)
	for i, n := range mainPath {
		nodeCols[n] = 0
		nodeRows[n] = i * 2 // Space out rows
		mainPathSet[n] = true
	}
	
	// Assign other nodes to subsequent columns
	// Simple BFS/DFS to place remaining nodes
	// For now, place all non-main nodes in Col 1, 2, etc.
	// Or just place them based on their parent's column + 1
	
	// Let's iterate through all nodes and assign if not set
	// We need a topological sort or similar to respect flow direction
	// But since we have the main path, we can anchor others relative to it.
	
	// For simplicity in this iteration:
	// Any node not in Main Path gets Col 1.
	// Its row is determined by its parent in Main Path (or closest ancestor).
	
	for n := range nodes {
		if !mainPathSet[n] {
			nodeCols[n] = 1
			// Find parent
			parentRow := 0
			for p, children := range adj {
				for _, c := range children {
					if c == n {
						if r, ok := nodeRows[p]; ok {
							parentRow = r
						}
					}
				}
			}
			nodeRows[n] = parentRow + 1
		}
	}

	// DEBUG: Print Node Rows/Cols
	// fmt.Println("DEBUG NODE LAYOUT:")
	// for n, r := range nodeRows {
	// 	fmt.Printf("%s: Row=%d, Col=%d\n", n, r, nodeCols[n])
	// }
	// fmt.Println("-------------------")

	// 3. Layout Dimensions
	nodeWidth := 26
	nodeHeight := 3
	vGap := 0 // Compact layout
	hGap := 8 // Wider gap for loop lines
	
	maxCol := 0
	maxRow := 0
	for _, c := range nodeCols { if c > maxCol { maxCol = c } }
	for _, r := range nodeRows { if r > maxRow { maxRow = r } }
	
	// Reserve extra columns for loop lines on the right
	loopCol := maxCol + 1
	
	canvasWidth := (loopCol + 1) * (nodeWidth + hGap) + 20
	canvasHeight := (maxRow + 1) * (nodeHeight + vGap) + 5
	
	canvas := NewCanvas(canvasWidth, canvasHeight)
	
	// 4. Draw Nodes
	nodeCoords := make(map[string]struct{x, y int})
	
	for n, col := range nodeCols {
		row := nodeRows[n]
		
		// Center the Main Column (Col 0)
		// Actually, user wants Main Flow on LEFT.
		// So Col 0 starts at margin.
		
		x := 2 + col * (nodeWidth + hGap)
		y := 0 + row * (nodeHeight + vGap) // Reduced top margin from 2 to 0
		
		nodeCoords[n] = struct{x, y int}{x, y}
		
		nodeType := "unknown"
		if node, ok := nodes[n]; ok {
			nodeType = node.Type
		}
		
		box := renderNodeBox(n, nodeType)
		canvas.DrawRenderedString(x, y, box)
	}
	
	// 5. Draw Edges
	for u, children := range adj {
		startCoord, ok1 := nodeCoords[u]
		if !ok1 { continue }
		
		startPtX := startCoord.x + nodeWidth/2
		startPtY := startCoord.y + nodeHeight
		
		for _, v := range children {
			endCoord, ok2 := nodeCoords[v]
			if !ok2 { continue }
			
			endPtX := endCoord.x + nodeWidth/2
			endPtY := endCoord.y
			
			// Detect Loop (Back Edge)
			// If target row <= source row, it's a back edge (usually)
			// Or if target is in Main Path and source is descendant
			isLoop := nodeRows[v] <= nodeRows[u]
			
			if isLoop {
				// Draw Loop: Right -> Up -> Left
				// Use the Loop Column
				
				sX := startCoord.x + nodeWidth
				sY := startCoord.y + nodeHeight/2
				
				eX := endCoord.x + nodeWidth
				eY := endCoord.y + nodeHeight/2
				
				// Loop Line X
				loopLineX := 2 + loopCol * (nodeWidth + hGap)
				
				canvas.DrawHorizontalLine(sX, loopLineX, sY)
				canvas.DrawVerticalLine(loopLineX, eY, sY)
				canvas.DrawHorizontalLine(loopLineX, eX, eY)
				
				// Corners
				canvas.Set(loopLineX, sY, "┘")
				canvas.Set(loopLineX, eY, "┐")
				
				// Arrow
				canvas.Set(eX+1, eY, "<")
				
			} else {
				// Forward Edge
				// If same column, straight down
				if nodeCols[u] == nodeCols[v] {
					canvas.DrawLine(startPtX, startPtY, endPtX, endPtY, true)
				} else {
					// Different column: Side -> Down -> Side
					// Or Bottom -> Side -> Top
					canvas.DrawLine(startPtX, startPtY, endPtX, endPtY, true)
				}
			}
		}
	}
	
	fmt.Println(canvas.Render())
}

func renderNodeBox(name, nodeType string) string {
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

	if name == "END" {
		borderColor = lipgloss.Color("196") // Red
		icon = "🏁"
	}

	return nodeStyle.Copy().
		BorderForeground(borderColor).
		Render(fmt.Sprintf("%s %s", icon, name))
}
