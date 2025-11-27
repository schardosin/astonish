package launcher

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/tools"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ConsoleConfig contains configuration for the console launcher
type ConsoleConfig struct {
	AgentConfig    *config.AgentConfig
	ProviderName   string
	ModelName      string
	SessionService session.Service
}

// RunConsole runs the agent in console mode with agent-controlled flow
func RunConsole(ctx context.Context, cfg *ConsoleConfig) error {
	// Initialize LLM
	fmt.Println("Initializing LLM provider...")
	llm, err := provider.GetProvider(ctx, cfg.ProviderName, cfg.ModelName)
	if err != nil {
		fmt.Printf("ERROR: Failed to initialize provider '%s' with model '%s': %v\n", cfg.ProviderName, cfg.ModelName, err)
		return fmt.Errorf("failed to initialize provider: %w", err)
	}
	fmt.Printf("✓ Provider initialized: %s (model: %s)\n", cfg.ProviderName, cfg.ModelName)

	// Initialize tools
	fmt.Println("Initializing tools...")
	internalTools, err := tools.GetInternalTools()
	if err != nil {
		fmt.Printf("ERROR: Failed to initialize tools: %v\n", err)
		return fmt.Errorf("failed to initialize internal tools: %w", err)
	}
	fmt.Printf("✓ Tools initialized: %d tools available\n", len(internalTools))

	// Create Astonish agent
	fmt.Println("Creating agent...")
	astonishAgent := agent.NewAstonishAgent(cfg.AgentConfig, llm, internalTools)

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

	// Create session service
	sessionService := cfg.SessionService
	if sessionService == nil {
		sessionService = session.InMemoryService()
	}

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

	for {
		// Run the agent
		// Only print the AI prefix if we are actually going to print something from the AI
		// But we don't know yet.
		// For now, let's print it when we detect the first printable content.
		aiPrefixPrinted := false
		
		waitingForInput := false
		waitingForApproval := false
		prevText := ""
		
		// Track current node to determine visibility
		var currentNodeName string
		
		for event, err := range r.Run(ctx, userID, sess.ID(), userMsg, adkagent.RunConfig{
			StreamingMode: adkagent.StreamingModeSSE,
		}) {
			if err != nil {
				fmt.Printf("\nERROR: %v\n", err)
				return err
			}

			// Update current node from StateDelta if present
			if event.Actions.StateDelta != nil {
				if node, ok := event.Actions.StateDelta["current_node"].(string); ok {
					currentNodeName = node
				}
				
				// Check for approval state
				if awaitingVal, ok := event.Actions.StateDelta["temp:awaiting_approval"]; ok {
					if awaiting, ok := awaitingVal.(bool); ok && awaiting {
						waitingForApproval = true
					}
				}
			}

			if event.LLMResponse.Content == nil {
				continue
			}

			// Extract text from response
			text := ""
			for _, p := range event.LLMResponse.Content.Parts {
				text += p.Text
			}

			if text != "" {
				// Determine if we should print this text
				shouldPrint := false
				isSystemMsg := false
				
				// Check for System Messages (Always Print)
				if strings.Contains(text, "--- Node") {
					isSystemMsg = true
					shouldPrint = true
					// Colorize Node Header
					text = strings.ReplaceAll(text, "--- Node", fmt.Sprintf("%s--- Node", ColorCyan))
					text = strings.ReplaceAll(text, " ---", fmt.Sprintf(" ---%s", ColorReset))
				} else if strings.Contains(text, "[ℹ️ Info]") {
					isSystemMsg = true
					shouldPrint = true
					// Colorize Info
					text = strings.ReplaceAll(text, "[ℹ️ Info]", fmt.Sprintf("%s[ℹ️ Info]%s", ColorBlue, ColorReset))
				} else if strings.Contains(text, "╭───") || strings.Contains(text, "│ Tool:") {
					isSystemMsg = true
					shouldPrint = true
					// Tool box is already formatted, maybe just ensure it stands out?
					// For now, leave as is, it's distinct enough.
				} else if strings.Contains(text, "Do you approve this execution?") {
					isSystemMsg = true
					shouldPrint = true
					waitingForApproval = true
				} else {
					// Regular LLM Output: Check UserMessage config
					if currentNodeName != "" {
						for _, node := range cfg.AgentConfig.Nodes {
							if node.Name == currentNodeName {
								// If UserMessage is configured and not empty, allow printing
								if len(node.UserMessage) > 0 {
									shouldPrint = true
								}
								// Also always allow printing for "input" nodes (prompts)
								if node.Type == "input" {
									shouldPrint = true
								}
								break
							}
						}
					} else {
						// If we don't know the node yet (e.g. startup), default to print
						shouldPrint = true
					}
				}

				if shouldPrint {
					// Print AI prefix if this is the first regular AI message (not system)
					if !isSystemMsg && !aiPrefixPrinted {
						fmt.Printf("\n%sAI:%s ", ColorGreen, ColorReset)
						aiPrefixPrinted = true
					}

					// In SSE mode, print partial responses as they come
					if !event.IsFinalResponse() {
						fmt.Print(text)
						os.Stdout.Sync() // Force flush to show streaming
						prevText += text
					} else {
						// Only print final response if it doesn't match previously captured text
						if text != prevText {
							fmt.Print(text)
							os.Stdout.Sync() // Force flush
						}
						prevText = ""
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
		
		// If we're waiting for input OR approval, prompt the user
		if waitingForInput || waitingForApproval {
			fmt.Printf("\n\n%sYou:%s ", ColorYellow, ColorReset)
			
			userInput, err := reader.ReadString('\n')
			if err != nil {
				log.Fatal(err)
			}

			userInput = strings.TrimSpace(userInput)
			
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
