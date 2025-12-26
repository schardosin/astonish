package demo

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadScript loads a demo script from a YAML file
func LoadScript(path string) (*DemoScript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read script file: %w", err)
	}

	var script DemoScript
	if err := yaml.Unmarshal(data, &script); err != nil {
		return nil, fmt.Errorf("failed to parse script: %w", err)
	}

	// Set defaults
	if script.Width == 0 {
		script.Width = 800
	}
	if script.Height == 0 {
		script.Height = 400
	}

	return &script, nil
}

// GenerateOptions configures HTML generation
type GenerateOptions struct {
	Title       string
	Width       int
	Height      int
	AutoPlay    bool
	TypeSpeed   int // Default typing speed in ms
	OutputDelay int // Delay between output lines
}

// DefaultOptions returns sensible defaults
func DefaultOptions() GenerateOptions {
	return GenerateOptions{
		Title:       "Astonish Demo",
		Width:       800,
		Height:      400,
		AutoPlay:    true,
		TypeSpeed:   30,
		OutputDelay: 400,
	}
}

// Generate creates a self-contained HTML file from events
func Generate(script *DemoScript, options GenerateOptions) string {
	// Use script values if available, otherwise use options
	title := options.Title
	if script.Title != "" {
		title = script.Title
	}
	width := options.Width
	if script.Width > 0 {
		width = script.Width
	}
	height := options.Height
	if script.Height > 0 {
		height = script.Height
	}

	// Convert events to JavaScript array
	eventsJS := eventsToJS(script.Events, options)

	return fmt.Sprintf(htmlTemplate, title, width, height, eventsJS)
}

// eventsToJS converts Go events to JavaScript array literal
func eventsToJS(events []Event, options GenerateOptions) string {
	js := "[\n"
	for i, e := range events {
		typeSpeed := e.TypeSpeed
		if typeSpeed == 0 && (e.Type == EventCommand || e.Type == EventUserInput) {
			typeSpeed = options.TypeSpeed
		}

		delayMs := int(e.Delay.Milliseconds())
		if delayMs == 0 {
			delayMs = options.OutputDelay
		}

		js += fmt.Sprintf(`    { type: '%s', text: %q, delay: %d, typeSpeed: %d }`,
			e.Type, e.Text, delayMs, typeSpeed)

		if i < len(events)-1 {
			js += ",\n"
		} else {
			js += "\n"
		}
	}
	js += "  ]"
	return js
}

// SaveHTML saves generated HTML to a file
func SaveHTML(html, path string) error {
	return os.WriteFile(path, []byte(html), 0644)
}

// htmlTemplate is the self-contained HTML template
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s</title>
  <style>
    :root {
      --slate-50: #f8fafc;
      --slate-100: #f1f5f9;
      --slate-200: #e2e8f0;
      --slate-300: #cbd5e1;
      --slate-400: #94a3b8;
      --slate-500: #64748b;
      --slate-600: #475569;
      --slate-700: #334155;
      --slate-800: #1e293b;
      --slate-900: #0f172a;
      --purple-400: #c084fc;
      --cyan-400: #22d3ee;
      --orange-400: #fb923c;
      --green-400: #4ade80;
      --green-500: #22c55e;
      --yellow-400: #facc15;
      --yellow-500: #eab308;
      --red-500: #ef4444;
      --font-mono: 'SF Mono', 'Menlo', 'Monaco', 'Consolas', monospace;
    }

    * {
      margin: 0;
      padding: 0;
      box-sizing: border-box;
    }

    body {
      background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 2rem;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    }

    .terminal {
      width: %dpx;
      max-width: 100%%;
      text-align: left;
      background: var(--slate-900);
      border-radius: 12px;
      overflow: hidden;
      box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
    }

    .terminal-header {
      background: var(--slate-800);
      padding: 0.75rem 1rem;
      display: flex;
      gap: 0.5rem;
    }

    .terminal-dot {
      width: 12px;
      height: 12px;
      border-radius: 50%%;
    }

    .terminal-dot-red { background: var(--red-500); }
    .terminal-dot-yellow { background: var(--yellow-500); }
    .terminal-dot-green { background: var(--green-500); }

    .terminal-body {
      padding: 1.25rem;
      font-family: var(--font-mono);
      font-size: 0.875rem;
      color: var(--slate-300);
      line-height: 1.7;
      min-height: %dpx;
    }

    .prompt {
      color: var(--green-400);
      font-weight: bold;
    }

    .terminal-cmd {
      color: white;
    }

    .terminal-output {
      color: var(--slate-400);
    }

    .terminal-success {
      color: var(--green-400);
    }

    .terminal-agent-label {
      color: var(--yellow-400);
      font-weight: bold;
    }

    .terminal-agent-text {
      color: var(--slate-300);
    }

    .terminal-prompt-text {
      color: var(--yellow-400);
    }

    .terminal-user-input {
      color: var(--purple-400);
    }

    .cursor {
      display: inline-block;
      width: 8px;
      height: 1.2em;
      background: var(--green-400);
      margin-left: 2px;
      animation: blink 1s step-end infinite;
      vertical-align: text-bottom;
    }

    @keyframes blink {
      0%%, 100%% { opacity: 1; }
      50%% { opacity: 0; }
    }

    .tool-box {
      display: inline-block;
      border: 1px solid var(--slate-600);
      border-radius: 8px;
      margin: 0.5rem 0;
      padding: 0.75rem 1rem;
      background: var(--slate-800);
      min-width: 280px;
      max-width: 400px;
    }

    .tool-box-header {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      margin-bottom: 0.5rem;
      padding-bottom: 0.5rem;
      border-bottom: 1px solid var(--slate-600);
    }

    .tool-box-icon {
      font-size: 1rem;
    }

    .tool-box-name {
      color: var(--orange-400);
      font-weight: bold;
    }

    .tool-box-params {
      color: var(--slate-400);
      font-size: 0.8rem;
    }

    .tool-box-params .param-key {
      color: var(--slate-500);
    }

    .tool-box-params .param-value {
      color: var(--cyan-400);
    }
  </style>
</head>
<body>
  <div class="terminal">
    <div class="terminal-header">
      <span class="terminal-dot terminal-dot-red"></span>
      <span class="terminal-dot terminal-dot-yellow"></span>
      <span class="terminal-dot terminal-dot-green"></span>
    </div>
    <div class="terminal-body" id="terminal-demo"></div>
  </div>

  <script>
    const events = %s;

    async function typeWriter(text, element, speed = 30, cssClass = 'terminal-cmd') {
      return new Promise(resolve => {
        let i = 0;
        const span = document.createElement('span');
        span.className = cssClass;
        element.appendChild(span);
        
        const cursor = document.createElement('span');
        cursor.className = 'cursor';
        element.appendChild(cursor);

        function type() {
          if (i < text.length) {
            span.textContent += text.charAt(i);
            i++;
            setTimeout(type, speed);
          } else {
            cursor.remove();
            resolve();
          }
        }
        type();
      });
    }

    async function runDemo() {
      const terminal = document.getElementById('terminal-demo');
      terminal.innerHTML = '';
      
      for (const event of events) {
        // Wait for delay
        if (event.delay > 0) {
          await new Promise(r => setTimeout(r, event.delay));
        }

        const div = document.createElement('div');
        div.style.marginBottom = '0.3rem';
        terminal.appendChild(div);

        switch(event.type) {
          case 'command':
            div.innerHTML = '<span class="prompt">$</span> ';
            await typeWriter(event.text, div, event.typeSpeed || 30, 'terminal-cmd');
            break;
          
          case 'output':
            div.className = 'terminal-output';
            div.textContent = event.text;
            break;
          
          case 'success':
            div.className = 'terminal-success';
            div.textContent = event.text;
            break;
          
          case 'agent':
            div.innerHTML = '<span class="terminal-agent-label">Agent:</span>';
            break;
          
          case 'agent_text':
            div.className = 'terminal-agent-text';
            div.textContent = event.text;
            break;
          
          case 'tool_box':
            div.style.marginBottom = '0.5rem';
            div.innerHTML = event.text; // Pre-formatted HTML for the tool box
            break;
          
          case 'prompt':
            div.className = 'terminal-prompt-text';
            div.textContent = event.text;
            break;
          
          case 'user_input':
            div.innerHTML = '<span class="prompt">></span> ';
            await typeWriter(event.text, div, event.typeSpeed || 50, 'terminal-user-input');
            break;
        }
      }
    }

    // Auto-start on page load
    window.addEventListener('load', runDemo);
  </script>
</body>
</html>
`

