package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
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
)

// ConsoleConfig contains configuration for the console launcher
type ConsoleConfig struct {
	AgentConfig    *config.AgentConfig
	ProviderName   string
	ModelName      string
	SessionService session.Service
	DebugMode      bool
}

// RunConsole runs the agent in console mode with agent-controlled flow
func RunConsole(ctx context.Context, cfg *ConsoleConfig) error {
	// Suppress default logger (used by ADK for "unknown agent" warnings)
	log.SetOutput(io.Discard)
	
	// Initialize LLM
	fmt.Println("Initializing LLM provider...")
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName)
	if err != nil {
		fmt.Printf("ERROR: Failed to initialize provider '%s' with model '%s': %v\n", cfg.ProviderName, cfg.ModelName, err)
		return fmt.Errorf("failed to initialize provider: %w", err)
	}
	fmt.Printf("✓ Provider initialized: %s (model: %s)\n", cfg.ProviderName, cfg.ModelName)

	// Initialize internal tools
	fmt.Println("Initializing internal tools...")
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		fmt.Printf("ERROR: Failed to initialize tools: %v\n", err)
		return fmt.Errorf("failed to initialize internal tools: %w", err)
	}
	fmt.Printf("✓ Internal tools initialized: %d tools available\n", len(internalTools))

	// Initialize MCP tools
	fmt.Println("Initializing MCP servers...")
	
	mcpManager, err := mcp.NewManager()
	var mcpToolsets []tool.Toolset
	if err != nil {
		fmt.Printf("Warning: Failed to create MCP manager: %v\n", err)
	} else {
		if err := mcpManager.InitializeToolsets(ctx); err != nil {
			fmt.Printf("Warning: Failed to initialize MCP toolsets: %v\n", err)
		} else {
			mcpToolsets = mcpManager.GetToolsets()
			if len(mcpToolsets) > 0 {
				fmt.Printf("✓ MCP servers initialized: %d server(s)\n", len(mcpToolsets))
			} else {
				fmt.Println("✓ No MCP servers configured")
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
	fmt.Println("Creating agent...")
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
	fmt.Println("✓ Agent created")



	// Create session
	fmt.Println("Creating session...")
	userID, appName := "console_user", "astonish"
	resp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create session: %v\n", err)
		return fmt.Errorf("failed to create session: %w", err)
	}
	fmt.Println("✓ Session created")

	sess := resp.Session

	// Create runner
	fmt.Println("Creating runner...")
	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create runner: %v\n", err)
		return fmt.Errorf("failed to create runner: %w", err)
	}
	fmt.Println("✓ Runner created")
	fmt.Println("\n" + strings.Repeat("=", 50))

	reader := bufio.NewReader(os.Stdin)

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

	for {
		// Run the agent
		// Only print the AI prefix if we are actually going to print something from the AI
		aiPrefixPrinted := false
		
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
				break
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
			
			if currentNodeName != "" {
				for _, node := range cfg.AgentConfig.Nodes {
					if node.Name == currentNodeName {
						// Smart Suppression:
						// 1. If UserMessage is configured, suppress streaming and show specific fields.
						// 2. If OutputModel is configured (but no UserMessage), suppress streaming (silent data node).
						// 3. If NEITHER is configured, allow streaming (chat node).
						// 4. EXCEPTION: Input nodes should always show their prompt (even if they have OutputModel).
						
						if node.Type == "input" {
							suppressStreaming = false
						} else {
							hasUserMessage := len(node.UserMessage) > 0
							hasOutputModel := len(node.OutputModel) > 0
							
							if hasUserMessage {
								suppressStreaming = true
								userMessageFields = node.UserMessage
							} else if hasOutputModel {
								suppressStreaming = true
								// No user message fields to show, just suppress
							}
						}
						break
					}
				}
			}

			// Update current node from StateDelta if present
			if event.Actions.StateDelta != nil {
				if node, ok := event.Actions.StateDelta["current_node"].(string); ok {
					// If we were suppressing, clear the line buffer to prevent leakage of partial lines from the previous node
					if suppressStreaming {
						lineBuffer = ""
					}
					
					currentNodeName = node
					// Re-evaluate suppression when node changes
					suppressStreaming = false
					userMessageFields = nil
					for _, n := range cfg.AgentConfig.Nodes {
						if n.Name == currentNodeName {
							// Smart Suppression logic (same as above)
							if n.Type == "input" {
								suppressStreaming = false
							} else {
								hasUserMessage := len(n.UserMessage) > 0
								hasOutputModel := len(n.OutputModel) > 0
								
								if hasUserMessage {
									suppressStreaming = true
									userMessageFields = n.UserMessage
								} else if hasOutputModel {
									suppressStreaming = true
								}
							}
							if cfg.DebugMode {
								fmt.Printf("[DEBUG] Node changed to '%s'. SuppressStreaming: %v\n", currentNodeName, suppressStreaming)
							}
							break
						}
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
					for _, field := range userMessageFields {
						if val, ok := event.Actions.StateDelta[field]; ok {
							// Format the value for display
							var displayStr string
							switch v := val.(type) {
							case string:
								displayStr = v
							case []string:
								// For lists, format as a bulleted list
								var sb strings.Builder
								for _, item := range v {
									sb.WriteString(fmt.Sprintf("- %s\n", item))
								}
								displayStr = sb.String()
							case []interface{}:
								// For generic lists
								var sb strings.Builder
								for _, item := range v {
									sb.WriteString(fmt.Sprintf("- %v\n", item))
								}
								displayStr = sb.String()
							default:
								// Fallback to JSON representation for complex objects
								jsonBytes, err := json.MarshalIndent(v, "", "  ")
								if err == nil {
									displayStr = string(jsonBytes)
								} else {
									displayStr = fmt.Sprintf("%v", v)
								}
							}

							// Render and print immediately
							if displayStr != "" {
								rendered := ui.SmartRender(displayStr)
								if !aiPrefixPrinted {
									fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
									aiPrefixPrinted = true
								}
								fmt.Print(rendered)
							}
						}
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
						
						// Colorize Tool Box
						if strings.Contains(line, "╭") || strings.Contains(line, "╰") {
							// Borders: Cyan
							line = fmt.Sprintf("%s%s%s", ColorCyan, strings.TrimSuffix(line, "\n"), ColorReset) + "\n"
						} else {
							// Content: Cyan borders, Yellow text
							// Re-construct the line
							content := line
							content = strings.ReplaceAll(content, "│", fmt.Sprintf("%s│%s", ColorCyan, ColorYellow))
							line = content
							// Ensure line ends with reset
							if strings.HasSuffix(line, "\n") {
								line = strings.TrimSuffix(line, "\n") + ColorReset + "\n"
							} else {
								line += ColorReset
							}
						}
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
						if !suppressStreaming {
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
							fmt.Print(line)
							
							// Reset prefix state for next AI output (since we interrupted with system msg)
							aiPrefixPrinted = false
						} else {
							// Buffer regular text
							textBuffer.WriteString(line)
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
			// Only flush if NOT suppressed
			if !suppressStreaming {
				textBuffer.WriteString(lineBuffer)
			}
			lineBuffer = ""
		}
		
		// Flush text buffer at the end of the turn
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
		
		// FLUSH TEXT BUFFER at end of turn
		if textBuffer.Len() > 0 {
			rendered := ui.SmartRender(textBuffer.String())
			if !aiPrefixPrinted {
				fmt.Printf("\n%sAgent:%s\n", ColorGreen, ColorReset)
				aiPrefixPrinted = true
			}
			fmt.Print(rendered)
			textBuffer.Reset()
		}
		
		// If we're waiting for input OR approval, prompt the user
		if waitingForInput || waitingForApproval {
			var userInput string
			var err error
			
			// Check if we have options for selection
			if len(approvalOptions) > 0 {
				// Use interactive selection for approval
				fmt.Println("\n[?] Do you approve this execution?:")
				selectedIdx, err := ui.ReadSelection(approvalOptions)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
					os.Exit(1)
				}
				userInput = approvalOptions[selectedIdx]
				fmt.Printf("You: %s\n", userInput)
				approvalOptions = nil // Clear for next iteration
			} else if len(inputOptions) > 0 {
				// Use interactive selection for input
				fmt.Println("\nSelect an option:")
				selectedIdx, err := ui.ReadSelection(inputOptions)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
					os.Exit(1)
				}
				userInput = inputOptions[selectedIdx]
				fmt.Printf("You: %s\n", userInput)
				inputOptions = nil // Clear for next iteration
			} else {
				// Free text input
				fmt.Printf("\n\n%sYou:%s ", ColorYellow, ColorReset)
				userInput, err = reader.ReadString('\n')
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
					os.Exit(1)
				}
				userInput = strings.TrimSpace(userInput)
			}
			
			// Create user message
			userMsg = genai.NewContentFromText(userInput, genai.RoleUser)
		} else {
			// Agent completed without needing input
			fmt.Println()
			break
		}
	}
	return nil
}
