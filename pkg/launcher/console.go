package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	"github.com/schardosin/astonish/pkg/ui"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	tea "github.com/charmbracelet/bubbletea"
)

// ConsoleConfig contains configuration for the console launcher
type ConsoleConfig struct {
	AgentConfig    *config.AgentConfig
	AppConfig      *config.AppConfig
	ProviderName   string
	ModelName      string
	SessionService session.Service
	DebugMode      bool
}

// RunConsole runs the agent in console mode with agent-controlled flow
func RunConsole(ctx context.Context, cfg *ConsoleConfig) error {
	// Suppress default logger (used by ADK for "unknown agent" warnings)
	// Only suppress if NOT in debug mode
	if !cfg.DebugMode {
		log.SetOutput(io.Discard)
	}
	
	// Initialize LLM
	if cfg.DebugMode {
		fmt.Println("Initializing LLM provider...")
	}
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName, cfg.AppConfig)
	if err != nil {
		fmt.Printf("ERROR: Failed to initialize provider '%s' with model '%s': %v\n", cfg.ProviderName, cfg.ModelName, err)
		return fmt.Errorf("failed to initialize provider: %w", err)
	}
	if cfg.DebugMode {
		fmt.Printf("✓ Provider initialized: %s (model: %s)\n", cfg.ProviderName, cfg.ModelName)
	}

	// Initialize internal tools
	if cfg.DebugMode {
		fmt.Println("Initializing internal tools...")
	}
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		fmt.Printf("ERROR: Failed to initialize tools: %v\n", err)
		return fmt.Errorf("failed to initialize internal tools: %w", err)
	}
	if cfg.DebugMode {
		fmt.Printf("✓ Internal tools initialized: %d tools available\n", len(internalTools))
	}

	// Initialize MCP tools
	if cfg.DebugMode {
		fmt.Println("Initializing MCP servers...")
	}
	
	mcpManager, err := mcp.NewManager()
	var mcpToolsets []tool.Toolset
	if err != nil {
		if cfg.DebugMode {
			fmt.Printf("Warning: Failed to create MCP manager: %v\n", err)
		}
	} else {
		if err := mcpManager.InitializeToolsets(ctx); err != nil {
			if cfg.DebugMode {
				fmt.Printf("Warning: Failed to initialize MCP toolsets: %v\n", err)
			}
		} else {
			mcpToolsets = mcpManager.GetToolsets()
			if cfg.DebugMode {
				if len(mcpToolsets) > 0 {
					fmt.Printf("✓ MCP servers initialized: %d server(s)\n", len(mcpToolsets))
				} else {
					fmt.Println("✓ No MCP servers configured")
				}
			}
		}
	}

	// Create session service
	sessionService := cfg.SessionService
	if sessionService == nil {
		sessionService = session.InMemoryService()
	}

	// Create Astonish agent with internal tools
	// MCP toolsets will be passed directly to llmagent when creating nodes
	if cfg.DebugMode {
		fmt.Println("Creating agent...")
	}
	astonishAgent := agent.NewAstonishAgentWithToolsets(cfg.AgentConfig, llm, internalTools, mcpToolsets)
	astonishAgent.DebugMode = cfg.DebugMode
	astonishAgent.SessionService = sessionService

	// Create ADK agent wrapper
	adkAgent, err := adkagent.New(adkagent.Config{
		Name:        "astonish_agent",
		Description: cfg.AgentConfig.Description,
		Run:         astonishAgent.Run,
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create ADK agent: %v\n", err)
		return fmt.Errorf("failed to create ADK agent: %w", err)
	}
	if cfg.DebugMode {
		fmt.Println("✓ Agent created")
	}



	// Create session
	if cfg.DebugMode {
		fmt.Println("Creating session...")
	}
	userID, appName := "console_user", "astonish"
	resp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create session: %v\n", err)
		return fmt.Errorf("failed to create session: %w", err)
	}
	if cfg.DebugMode {
		fmt.Println("✓ Session created")
	}

	sess := resp.Session

	// Create runner
	if cfg.DebugMode {
		fmt.Println("Creating runner...")
	}
	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create runner: %v\n", err)
		return fmt.Errorf("failed to create runner: %w", err)
	}
	if cfg.DebugMode {
		fmt.Println("✓ Runner created")
	}



	// ANSI color codes
	const (
		ColorReset  = "\033[0m"
		ColorGreen  = "\033[32m"
		ColorYellow = "\033[33m"
		ColorBlue   = "\033[34m"
		ColorCyan   = "\033[36m"
		ColorGray   = "\033[90m"
	)



	// Start with empty message to let agent initialize and show first prompt
	var userMsg *genai.Content

	// Track current node to determine visibility across turns
	var currentNodeName string

	// Buffer for handling fragmented streaming output
	var lineBuffer string
	var textBuffer strings.Builder // Buffer for accumulating text to be rendered as markdown
	var inToolBlock bool
	var inToolBox bool

	// Spinner state
	var spinnerProgram *tea.Program
	var spinnerDone chan struct{}
	var currentSpinnerText string

	stopSpinner := func(markDone bool) {
		if spinnerProgram != nil {
			spinnerProgram.Quit()
			if spinnerDone != nil {
				<-spinnerDone
			}
			spinnerProgram = nil
			spinnerDone = nil
			
			if markDone && currentSpinnerText != "" {
				fmt.Printf("✓ %s\n", currentSpinnerText)
			}
			currentSpinnerText = ""
		}
	}

	startSpinner := func(text string) {
		stopSpinner(true) // Mark previous as done before starting new one
		currentSpinnerText = text
		spinnerDone = make(chan struct{})
		model := ui.NewSpinner(text)
		spinnerProgram = tea.NewProgram(model, tea.WithInput(nil))
		go func() {
			spinnerProgram.Run()
			close(spinnerDone)
		}()
	}

	for {
		// Reset state flags at start of turn
		inToolBox = false
		inToolBlock = false

		// Run the agent
		// Only print the AI prefix if we are actually going to print something from the AI
		aiPrefixPrinted := false
		
		// State tracking for input nodes
		isInputNode := false
		isOutputNode := false
		waitingForInput := false
		waitingForApproval := false
		var approvalOptions []string
		var inputOptions []string
		
		// Declare suppression variables here so they are accessible throughout the loop and after
		suppressStreaming := false
		var userMessageFields []string

		
		for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, adkagent.RunConfig{
			StreamingMode: adkagent.StreamingModeSSE,
		}) {
			if err != nil {
				fmt.Printf("\nERROR: %v\n", err)
				return err
			}

			// Debug logging for tool calls and responses
			if cfg.DebugMode && event.LLMResponse.Content != nil {
				// Flush text buffer before debug output
				if textBuffer.Len() > 0 {
					rendered := ui.SmartRender(textBuffer.String())
					if !aiPrefixPrinted {
						fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
						aiPrefixPrinted = true
					}
					fmt.Print(rendered)
					textBuffer.Reset()
				}

				for _, part := range event.LLMResponse.Content.Parts {
					if part.FunctionCall != nil {
						argsJSON, _ := json.MarshalIndent(part.FunctionCall.Args, "", "  ")
						fmt.Printf("\n%s[DEBUG] Tool Call: %s%s\nArguments:\n%s\n", ColorCyan, part.FunctionCall.Name, ColorReset, string(argsJSON))
					}
					if part.FunctionResponse != nil {
						respJSON, _ := json.MarshalIndent(part.FunctionResponse.Response, "", "  ")
						fmt.Printf("\n%s[DEBUG] Tool Response: %s%s\nResult:\n%s\n", ColorCyan, part.FunctionResponse.Name, ColorReset, string(respJSON))
					}
				}
			}

			// Check if we should suppress streaming output based on UserMessage OR OutputModel config
			// suppressStreaming is now declared outside the loop
			
			// Check if we should suppress streaming output based on UserMessage OR OutputModel config
			// suppressStreaming is now declared outside the loop and updated in the node change block below.

			// Check for user_message display marker first
			if event.Actions.StateDelta != nil {
				if _, hasMarker := event.Actions.StateDelta["_user_message_display"]; hasMarker {
					// This is a user_message event - print Agent prefix before the text
					stopSpinner(true)
					
					if !aiPrefixPrinted {
						fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
						aiPrefixPrinted = true
					}
					
					// The text content will be printed by the normal text processing below
					// We just needed to ensure the prefix is printed first
				}
				
				// Check for spinner text update
				if spinnerText, ok := event.Actions.StateDelta["_spinner_text"].(string); ok {
					startSpinner(spinnerText)
				}
			}
			
			// Update current node from StateDelta if present
			if event.Actions.StateDelta != nil {
				if node, ok := event.Actions.StateDelta["current_node"].(string); ok {
					// Only process if node actually changed
					if node != currentNodeName {
						// Flush buffer if we were streaming
						if !suppressStreaming {
							// Stop spinner before printing flush content
							stopSpinner(true)
							
							// Flush lineBuffer
							if lineBuffer != "" {
								rendered := ui.SmartRender(lineBuffer)
								fmt.Print(rendered)
								if !strings.HasSuffix(rendered, "\n") {
									fmt.Println()
								}
								lineBuffer = ""
							}
							
							// Flush textBuffer
							if textBuffer.Len() > 0 {
								rendered := ui.SmartRender(textBuffer.String())
								if rendered != "" {
									if !aiPrefixPrinted {
										fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
										aiPrefixPrinted = true
									}
									fmt.Print(rendered)
								}
								textBuffer.Reset()
							}
						}

						// If we were suppressing, clear the line buffer to prevent leakage of partial lines from the previous node
						if suppressStreaming {
							lineBuffer = ""
						}
						
						currentNodeName = node
						// Re-evaluate suppression when node changes
						suppressStreaming = false
						userMessageFields = nil
						
						// Determine if this is an input node or parallel node and setup suppression
						isInputNode = false
						isOutputNode = false
						isParallel := false
						hasUserMessage := false
						hasOutputModel := false
						
						for _, n := range cfg.AgentConfig.Nodes {
							if n.Name == currentNodeName {
								if n.Type == "input" {
									isInputNode = true
									suppressStreaming = true
								} else if n.Type == "output" {
									isOutputNode = true
									suppressStreaming = false
								} else {
									if n.Parallel != nil {
										isParallel = true
									}
									
									hasUserMessage = len(n.UserMessage) > 0
									hasOutputModel = len(n.OutputModel) > 0
									
									if hasUserMessage {
										suppressStreaming = true
										userMessageFields = n.UserMessage
									} else if hasOutputModel {
										suppressStreaming = true
									}
								}
								if cfg.DebugMode {
									fmt.Printf("[DEBUG] Node changed to '%s'. SuppressStreaming: %v, IsParallel: %v\n", currentNodeName, suppressStreaming, isParallel)
								}
								break
							}
						}
						
						// Manage Spinner
						if isInputNode || isParallel {
							stopSpinner(true)
						} else {
							startSpinner(fmt.Sprintf("Processing %s...", currentNodeName))
						}
					} else if spinnerProgram == nil && event.LLMResponse.Content == nil {
						// Node hasn't changed, but spinner was stopped (e.g. by tool box)
						// Restart it if we are not about to print content
						startSpinner(fmt.Sprintf("Processing %s...", currentNodeName))
					}
				}


				// Check for approval state
				if awaitingVal, ok := event.Actions.StateDelta["awaiting_approval"]; ok {
					if awaiting, ok := awaitingVal.(bool); ok && awaiting {
						waitingForApproval = true
					}
				}
				
				// Check for approval options
				if optsVal, ok := event.Actions.StateDelta["approval_options"]; ok {
					if opts, ok := optsVal.([]string); ok {
						approvalOptions = opts
					} else if optsInterface, ok := optsVal.([]interface{}); ok {
						for _, v := range optsInterface {
							approvalOptions = append(approvalOptions, fmt.Sprintf("%v", v))
						}
					}
				}
				
				// Check for input options
				if optsVal, ok := event.Actions.StateDelta["input_options"]; ok {
					if opts, ok := optsVal.([]string); ok {
						inputOptions = opts
					} else if optsInterface, ok := optsVal.([]interface{}); ok {
						for _, v := range optsInterface {
							inputOptions = append(inputOptions, fmt.Sprintf("%v", v))
						}
					}
				}

				// Check for UserMessage fields in StateDelta and print them if found
				if len(userMessageFields) > 0 {
					// Check if any fields are present in this event
					hasUserMessageContent := false
					for _, field := range userMessageFields {
						if _, ok := event.Actions.StateDelta[field]; ok {
							hasUserMessageContent = true
							break
						}
					}
					
					// If we have user_message content, ensure Agent prefix is printed first
					if hasUserMessageContent {
						// Stop spinner before printing
						stopSpinner(true)
						
						// Print Agent prefix if not already printed
						if !aiPrefixPrinted {
							fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
							aiPrefixPrinted = true
						}
						
						// Now print each field
						for _, field := range userMessageFields {
							if val, ok := event.Actions.StateDelta[field]; ok {
								// Format the value for display
								var displayStr string
								switch v := val.(type) {
								case string:
									displayStr = v
								default:
									// Use YAML-like formatting for everything else
									displayStr = ui.FormatAsYamlLike(v, 0)
								}

								// Render and print immediately
								if displayStr != "" {
									// For output nodes, we want to ensure it's visible
									// If it's a list, render it nicely
									if strings.Contains(displayStr, "\n- ") {
										fmt.Printf("\n%s\n", displayStr)
									} else {
										// Standard output
										rendered := ui.SmartRender(displayStr)
										fmt.Print(rendered)
										// Ensure newline at end if not present
										if !strings.HasSuffix(rendered, "\n") {
											fmt.Println()
										}
									}
								}
							}
						}
						
						// CRITICAL: Clear text buffer to prevent duplicate display
						// Since we've already displayed the user_message content from StateDelta,
						// we don't want the text content from the event to be displayed again
						textBuffer.Reset()
						lineBuffer = ""
					}
				}
			}

			if event.LLMResponse.Content == nil {
				continue
			}

			// Extract text from response
			chunk := ""
			for _, p := range event.LLMResponse.Content.Parts {
				chunk += p.Text
			}

			if chunk != "" {
				lineBuffer += chunk
				
				// Process complete lines
				for {
					newlineIdx := strings.Index(lineBuffer, "\n")
					if newlineIdx == -1 {
						break
					}
					
					line := lineBuffer[:newlineIdx+1] // Include newline
					lineBuffer = lineBuffer[newlineIdx+1:]
					
					// Check for Tool Block Start (Internal XML)
					if strings.Contains(line, "<tool_use>") {
						inToolBlock = true
					}
					
					// Check for Tool Box Start (Visual UI)
					if strings.Contains(line, "╭") {
						inToolBox = true
					}
					
					shouldPrint := false
					isSystemMsg := false
					
					if inToolBlock {
						shouldPrint = false
					} else if inToolBox {
						isSystemMsg = true
						shouldPrint = true
						// Manual coloring removed - relying on lipgloss styles from agent
					} else if strings.Contains(line, "--- Node") {
						isSystemMsg = true
						shouldPrint = true
						// Colorize Node Header
						line = strings.ReplaceAll(line, "--- Node", fmt.Sprintf("%s--- Node", ColorCyan))
						line = strings.ReplaceAll(line, " ---", fmt.Sprintf(" ---%s", ColorReset))
					} else if strings.Contains(line, "[ℹ️ Info]") {
						isSystemMsg = true
						shouldPrint = true
						// Colorize Info
						line = strings.ReplaceAll(line, "[ℹ️ Info]", fmt.Sprintf("%s[ℹ️ Info]%s", ColorBlue, ColorReset))
					} else if strings.Contains(line, "Do you approve this execution?") {
						isSystemMsg = true
						shouldPrint = true
						waitingForApproval = true
					} else if strings.Contains(line, "> Yes") || strings.Contains(line, "  No") {
						isSystemMsg = true
						shouldPrint = true
					} else {
						// Regular LLM Output: Check suppression
						// If it's an input node, we WANT to process (buffer) the text so we can use it as the prompt,
						// even though we suppress streaming printing.
						if !suppressStreaming || isInputNode {
							shouldPrint = true
						}
					}
					
					// Check for Tool Block End
					if strings.Contains(line, "</tool_use>") {
						inToolBlock = false
					}
					
					// Check for Tool Box End
					if strings.Contains(line, "╰") {
						inToolBox = false
					}

					if shouldPrint {
						// Only mark as done if it's NOT a system message (e.g. tool box, approval)
						// If it IS a system message, we just want to pause/clear it temporarily
						stopSpinner(!isSystemMsg)
						
						if isSystemMsg {
							// FLUSH TEXT BUFFER before printing system message
							if textBuffer.Len() > 0 {
								rendered := ui.SmartRender(textBuffer.String())
								if rendered != "" {
									if !aiPrefixPrinted {
										fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
										aiPrefixPrinted = true
									}
									fmt.Print(rendered)
								}
								textBuffer.Reset()
							}
							
							// Print System Message
							toPrint := line
							// If we are suppressing streaming, but the line contains a tool box start,
							// we must trim any text before the tool box to prevent leakage,
							// BUT we must preserve the ANSI color codes that immediately precede the box.
							if suppressStreaming && strings.Contains(line, "╭") {
								// Regex to find the tool box start and any immediately preceding ANSI color codes
								// This matches: (optional ANSI codes) followed by "╭"
								re := regexp.MustCompile(`((?:\x1b\[[0-9;]*m)*)╭`)
								loc := re.FindStringIndex(line)
								if loc != nil {
									// loc[0] is the start of the match (including the ANSI codes)
									toPrint = line[loc[0]:]
								}
							}
							fmt.Print(toPrint)
							
							// Reset prefix state for next AI output (since we interrupted with system msg)
							aiPrefixPrinted = false
						} else {
							// Buffer regular text
							textBuffer.WriteString(line)
							
							// Flush immediately for streaming effect
							// Note: SmartRender might be less effective on single lines for things like tables,
							// but it's necessary for real-time feedback.
							if !suppressStreaming && textBuffer.Len() > 0 {
								var rendered string
								if isOutputNode {
									// For output nodes, bypass SmartRender to preserve formatting (e.g. JSON)
									rendered = textBuffer.String()
								} else {
									rendered = ui.SmartRender(textBuffer.String())
								}
								
								if rendered != "" {
									if !aiPrefixPrinted {
										fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
										aiPrefixPrinted = true
									}
									fmt.Print(rendered)
								}
								textBuffer.Reset()
							}
						}
					}
				}
			}

			// Check if we're at an input node
			if currentNodeName != "" {
				for _, node := range cfg.AgentConfig.Nodes {
					if node.Name == currentNodeName && node.Type == "input" {
						waitingForInput = true
						break
					}
				}
			}
		}
		
		// Flush any remaining content in lineBuffer
		if lineBuffer != "" {
			// Only flush if NOT suppressed OR if it's an input node (to capture prompt)
			if !suppressStreaming || isInputNode {
				textBuffer.WriteString(lineBuffer)
			}
			lineBuffer = ""
		}
		
		// Flush text buffer at the end of the turn
		// If we are waiting for input, we CAPTURE the text buffer as the prompt instead of printing it
		if waitingForInput || waitingForApproval {
			// Capture prompt from text buffer
			var title, description string
			if textBuffer.Len() > 0 {
				// SmartRender returns text with ANSI color codes. We need to strip them
				// to get clean text for the title/description and status badge.
				rawRendered := ui.SmartRender(textBuffer.String())
				ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
				cleanText := ansiRegex.ReplaceAllString(rawRendered, "")
				promptText := strings.TrimSpace(cleanText)
				
				parts := strings.SplitN(promptText, "\n", 2)
				if len(parts) > 0 {
					title = strings.TrimSpace(parts[0])
				}
				if len(parts) > 1 {
					// Custom cleaning to preserve indentation while removing leading/trailing empty lines
					// This fixes alignment issues when Glamour adds indentation to lists
					descLines := strings.Split(parts[1], "\n")
					start := 0
					for start < len(descLines) && strings.TrimSpace(descLines[start]) == "" {
						start++
					}
					end := len(descLines)
					for end > start && strings.TrimSpace(descLines[end-1]) == "" {
						end--
					}
					if start < end {
						description = strings.Join(descLines[start:end], "\n")
					}
				}
				
				// Clear text buffer as we've consumed it
				textBuffer.Reset()
			}
			
			// Stop spinner before showing input
			stopSpinner(true)
			
			// Show input dialog
			if waitingForApproval {
				// Use approval options if available
				opts := []string{"Yes", "No"}
				if len(approvalOptions) > 0 {
					opts = approvalOptions
				}
				
				// Default title if empty
				if title == "" {
					title = "Approval Required"
				}
				
				selection, err := ui.ReadSelection(opts, title, description)
				if err != nil {
					return err
				}
				
				// Send selection back to agent
				if selection == "Yes" {
					fmt.Println(ui.RenderStatusBadge("Command approved", true))
				} else {
					fmt.Println(ui.RenderStatusBadge("Command rejected", false))
				}
				userMsg = genai.NewContentFromText(selection, genai.RoleUser)
				continue
				
				// Reset state
				waitingForApproval = false
				approvalOptions = nil
			} else {
				// Regular input
				// Default title if empty
				if title == "" {
					title = "Input Required"
				}
				
				// Check if we have options for selection
				if len(inputOptions) > 0 {
					selection, err := ui.ReadSelection(inputOptions, title, description)
					if err != nil {
						return err
					}
					// Strip trailing colon from title for cleaner display
					displayTitle := strings.TrimSuffix(title, ":")
					fmt.Println(ui.RenderStatusBadge(fmt.Sprintf("%s: %s", displayTitle, selection), true))
					userMsg = genai.NewContentFromText(selection, genai.RoleUser)
					continue
				} else {
					// Free text input
					input, err := ui.ReadInput(title, description)
					if err != nil {
						return err
					}
					// Strip trailing colon from title for cleaner display
					displayTitle := strings.TrimSuffix(title, ":")
					fmt.Println(ui.RenderStatusBadge(fmt.Sprintf("%s: %s", displayTitle, input), true))
					userMsg = genai.NewContentFromText(input, genai.RoleUser)
					continue
				}
				
				// Reset state
				waitingForInput = false
				inputOptions = nil
			}
		} else {
			// Normal flush at end of turn
			if textBuffer.Len() > 0 {
				// Only print if not suppressed
				if !suppressStreaming {
					stopSpinner(true)
					var rendered string
					if isOutputNode {
						rendered = textBuffer.String()
					} else {
						rendered = ui.SmartRender(textBuffer.String())
					}
					if rendered != "" {
						if !aiPrefixPrinted {
							fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
							aiPrefixPrinted = true
						}
						fmt.Print(rendered)
						if !strings.HasSuffix(rendered, "\n") {
							fmt.Println()
						}
					}
				}
				textBuffer.Reset()
			}
		}
		
		// If we broke out of the loop (e.g. END node), stop spinner
		if currentNodeName == "END" {
			stopSpinner(true)
			if cfg.DebugMode {
				fmt.Println("[DEBUG] Reached END node, exiting main loop")
			}
			break
		}
		
		// Agent completed without needing input
	}
	return nil
}
