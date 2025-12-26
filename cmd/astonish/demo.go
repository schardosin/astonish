package astonish

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/schardosin/astonish/pkg/demo"
)

func handleDemoCommand(args []string) error {
	demoFlags := flag.NewFlagSet("demo", flag.ExitOnError)
	scriptPath := demoFlags.String("script", "", "Path to demo script YAML file")
	outputPath := demoFlags.String("output", "", "Output HTML file path (default: demo.html)")
	width := demoFlags.Int("width", 800, "Terminal width in pixels")
	height := demoFlags.Int("height", 400, "Terminal height in pixels")
	title := demoFlags.String("title", "Astonish Demo", "Demo title")
	genTemplate := demoFlags.Bool("template", false, "Generate a template script file")

	demoFlags.Usage = func() {
		printDemoUsage()
	}

	if err := demoFlags.Parse(args); err != nil {
		return err
	}

	// Generate template if requested
	if *genTemplate {
		return generateTemplateScript(*outputPath)
	}

	if *scriptPath == "" {
		printDemoUsage()
		return fmt.Errorf("--script is required")
	}

	// Load the script
	script, err := demo.LoadScript(*scriptPath)
	if err != nil {
		return fmt.Errorf("failed to load script: %w", err)
	}

	// Override script values with CLI flags if provided
	if *width != 800 || script.Width == 0 {
		script.Width = *width
	}
	if *height != 400 || script.Height == 0 {
		script.Height = *height
	}

	// Generate HTML
	options := demo.GenerateOptions{
		Title:       *title,
		Width:       script.Width,
		Height:      script.Height,
		AutoPlay:    true,
		TypeSpeed:   30,
		OutputDelay: 400,
	}

	html := demo.Generate(script, options)

	// Determine output path
	output := *outputPath
	if output == "" {
		output = "demo.html"
	}

	// Handle relative paths
	if !filepath.IsAbs(output) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		output = filepath.Join(cwd, output)
	}

	// Write the file
	if err := demo.SaveHTML(html, output); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	fmt.Printf("✓ Demo generated: %s\n", output)
	fmt.Println("  Open in a browser to view the animation")

	return nil
}

func generateTemplateScript(outputPath string) error {
	template := `title: "My Astonish Demo"
width: 900
height: 500
events:
  - type: command
    text: "astonish agents run my_flow"
    delay: 500ms
  - type: output
    text: "✓ Processing..."
    delay: 800ms
  - type: prompt
    text: "What would you like to search for?"
    delay: 500ms
  - type: user_input
    text: "hello world"
    delay: 1000ms
    type_speed: 50
  - type: output
    text: ""
    delay: 200ms
  - type: output
    text: "Agent:"
    delay: 200ms
  - type: output
    text: "• Here is the first result"
    delay: 300ms
  - type: output
    text: "• Here is the second result"
    delay: 300ms
  - type: output
    text: ""
    delay: 200ms
  - type: output
    text: "✓ Processing END..."
    delay: 400ms
`

	output := outputPath
	if output == "" {
		output = "demo-template.yaml"
	}

	if !filepath.IsAbs(output) {
		cwd, _ := os.Getwd()
		output = filepath.Join(cwd, output)
	}

	if err := os.WriteFile(output, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}

	fmt.Printf("✓ Template created: %s\n", output)
	fmt.Println("  Edit the file, then run: astonish demo --script " + filepath.Base(output) + " --output demo.html")
	return nil
}

func printDemoUsage() {
	fmt.Println(`Usage: astonish demo [options]

Generate animated HTML terminal demos (like the homepage terminal).

Options:
  --script <path>   Path to demo script YAML file (required)
  --output <path>   Output HTML file path (default: demo.html)
  --width <px>      Terminal width in pixels (default: 800)
  --height <px>     Terminal height in pixels (default: 400)
  --title <text>    Demo title
  --template        Generate a template script file to edit

Script Format (YAML):
  title: "My Demo"
  width: 900
  height: 500
  events:
    - type: command
      text: "astonish agents run my_flow"
      delay: 500ms
    - type: output
      text: "Processing..."
      delay: 400ms
    - type: prompt
      text: "Enter your query:"
      delay: 300ms
    - type: user_input
      text: "hello world"
      delay: 1s
      type_speed: 50

Event Types:
  command     - Typed character-by-character with cursor
  output      - Appears instantly (agent output)
  prompt      - Input prompt styling (yellow)
  user_input  - Typed response (purple, slower)

Examples:
  astonish demo --template --output my-demo.yaml   # Create template
  astonish demo --script my-demo.yaml              # Generate HTML`)
}
