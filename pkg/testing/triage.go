package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/common"
	"google.golang.org/genai"
)

// TriageAgent uses an LLM with tool access to investigate test failures.
// It follows the same ADK agent loop pattern as the chat agent but with a
// focused system prompt that guides the LLM through a diagnostic workflow.
type TriageAgent struct {
	llm         model.LLM
	executor    ToolExecutor
	artifactMgr *ArtifactManager
	maxTurns    int
	verbose     bool
}

// NewTriageAgent creates a triage agent with the given LLM and tool executor.
func NewTriageAgent(llm model.LLM, executor ToolExecutor, am *ArtifactManager, verbose bool) *TriageAgent {
	return &TriageAgent{
		llm:         llm,
		executor:    executor,
		artifactMgr: am,
		maxTurns:    20, // max tool-call turns before forcing a verdict
		verbose:     verbose,
	}
}

// Investigate runs the triage agent to analyze a failed test step.
// It returns a structured verdict with classification, root cause, and recommendation.
func (ta *TriageAgent) Investigate(ctx context.Context, step StepResult, test LoadedTest, suite *LoadedSuite, setupLog string) (*TriageVerdict, error) {
	// Build the system prompt with failure context
	instruction := ta.buildInstruction(step, test, suite, setupLog)

	// Build triage tools from the executor
	triageTools, err := BuildTriageTools(ctx, ta.executor)
	if err != nil {
		return nil, fmt.Errorf("build triage tools: %w", err)
	}

	// Create the ADK agent
	agent, err := llmagent.New(llmagent.Config{
		Name:        "triage",
		Model:       ta.llm,
		Instruction: instruction,
		Tools:       triageTools,
	})
	if err != nil {
		return nil, fmt.Errorf("create triage agent: %w", err)
	}

	// Create in-memory session service and session
	sessionSvc := common.NewAutoInitService(session.InMemoryService())

	r, err := runner.New(runner.Config{
		AppName:        "astonish-triage",
		Agent:          agent,
		SessionService: sessionSvc,
	})
	if err != nil {
		return nil, fmt.Errorf("create triage runner: %w", err)
	}

	userID := "triage"
	sessionID := "triage-" + uuid.New().String()[:8]

	// Create the session
	_, err = sessionSvc.Create(ctx, &session.CreateRequest{
		AppName:   "astonish-triage",
		UserID:    userID,
		SessionID: sessionID,
		State:     map[string]any{},
	})
	if err != nil {
		return nil, fmt.Errorf("create triage session: %w", err)
	}

	// Build the user message that kicks off investigation
	userMsg := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: ta.buildUserMessage(step)},
		},
	}

	// Run the agent and collect output
	var outputParts []string
	toolCallCount := 0
	start := time.Now()

	if ta.verbose {
		log.Printf("[Triage] Starting investigation for step %q (tool: %s)", step.Name, step.Tool)
	}

	for event, runErr := range r.Run(ctx, userID, sessionID, userMsg, adkagent.RunConfig{}) {
		if runErr != nil {
			if ta.verbose {
				log.Printf("[Triage] Agent error: %v", runErr)
			}
			// Return a best-effort verdict from whatever we collected
			break
		}

		if event == nil {
			continue
		}

		// Count tool calls and collect text
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.Text != "" && !part.Thought {
					outputParts = append(outputParts, part.Text)
				}
				if part.FunctionCall != nil {
					toolCallCount++
					if ta.verbose {
						log.Printf("[Triage] Tool call %d: %s", toolCallCount, part.FunctionCall.Name)
					}
				}
			}
		}

		// Enforce max turns to prevent runaway investigation
		if toolCallCount >= ta.maxTurns {
			if ta.verbose {
				log.Printf("[Triage] Hit max turns (%d), stopping", ta.maxTurns)
			}
			break
		}
	}

	elapsed := time.Since(start)
	fullAnalysis := strings.Join(outputParts, "")

	if ta.verbose {
		log.Printf("[Triage] Investigation complete: %d tool calls, %v elapsed, %d chars output",
			toolCallCount, elapsed, len(fullAnalysis))
	}

	// Parse the JSON verdict from the agent's output
	verdict := ta.parseVerdict(fullAnalysis)

	// Save artifacts
	ta.saveTriageArtifacts(step.Name, fullAnalysis)

	return verdict, nil
}

// buildInstruction creates the system prompt for the triage agent.
func (ta *TriageAgent) buildInstruction(step StepResult, test LoadedTest, suite *LoadedSuite, setupLog string) string {
	var b strings.Builder

	b.WriteString(`You are the L1 Triage Agent for Astonish, an AI-powered test runner.
A deterministic test step has failed and you must investigate the root cause.

## Your Goal
Investigate the failure, classify it, and produce a structured JSON verdict.

## Investigation Workflow
1. **Check browser state** — If the failed step involves a browser tool, use browser_snapshot to see the current page state. Check browser_console_messages for JS errors and browser_network_requests for failed API calls.
2. **Check logs** — Use shell_command to check application logs (e.g., "cat /var/log/*.log", check process output with process_list + process_read).
3. **Read source code** — If the error points to a specific file or component, use read_file and grep_search to understand the code.
4. **Check environment** — Verify services are running (process_list), ports are listening (shell_command with "ss -tlnp"), and configuration is correct.
5. **Form hypothesis** — Based on evidence, determine the most likely root cause.

## Classification Categories
- **transient**: Timing issue, race condition, network flake, or temporary service unavailability. These failures may pass on retry.
- **bug**: Actual defect in the application code. The test correctly identified a problem.
- **environment**: Infrastructure or configuration issue (missing dependency, wrong port, service not started, container misconfiguration).
- **test_issue**: The test itself is incorrect — wrong selector, wrong expected value, or flawed test logic.

## Output Format
After your investigation, output a JSON verdict block enclosed in triple backticks with the "json" language tag. The JSON must have these exact fields:

` + "```" + `json
{
  "classification": "transient|bug|environment|test_issue",
  "confidence": 0.0-1.0,
  "root_cause": "One-sentence description of the root cause",
  "evidence": ["Evidence item 1", "Evidence item 2"],
  "location": "file:line if applicable, or empty string",
  "recommendation": "What should be done to fix this",
  "retry": true/false
}
` + "```" + `

Set "retry": true ONLY when classification is "transient" and you believe a retry would succeed.

## Rules
- Be concise. Focus on evidence, not speculation.
- Use tools to gather evidence. Do not guess without checking.
- If you cannot determine the cause, classify as "environment" with low confidence.
- Do not modify any files or restart services. Your role is diagnosis only.
- Keep your investigation to the minimum number of tool calls needed.

`)

	// Append failure context
	b.WriteString("## Failure Context\n\n")
	b.WriteString(fmt.Sprintf("**Suite:** %s\n", suite.Name))
	b.WriteString(fmt.Sprintf("**Test:** %s\n", test.Config.Description))
	b.WriteString(fmt.Sprintf("**Failed Step:** %s\n", step.Name))
	b.WriteString(fmt.Sprintf("**Tool:** %s\n", step.Tool))
	b.WriteString(fmt.Sprintf("**Duration:** %dms\n", step.Duration))

	if step.Error != "" {
		b.WriteString(fmt.Sprintf("**Error:** %s\n", step.Error))
	}

	if step.Assertion != nil {
		b.WriteString(fmt.Sprintf("**Assertion Type:** %s\n", step.Assertion.Type))
		b.WriteString(fmt.Sprintf("**Expected:** %s\n", step.Assertion.Expected))
		if step.Assertion.Actual != "" {
			b.WriteString(fmt.Sprintf("**Actual:** %s\n", step.Assertion.Actual))
		}
		if step.Assertion.Message != "" {
			b.WriteString(fmt.Sprintf("**Assertion Message:** %s\n", step.Assertion.Message))
		}
	}

	if step.Output != "" {
		b.WriteString("\n**Raw Output (from failed step):**\n```\n")
		output := step.Output
		if len(output) > 5120 {
			output = output[:5120] + "\n... (truncated)"
		}
		b.WriteString(output)
		b.WriteString("\n```\n")
	}

	// Include setup log snippet (last 2KB) for context
	if setupLog != "" {
		logSnippet := setupLog
		if len(logSnippet) > 2048 {
			logSnippet = "... (truncated)\n" + logSnippet[len(logSnippet)-2048:]
		}
		b.WriteString("\n**Setup Log (excerpt):**\n```\n")
		b.WriteString(logSnippet)
		b.WriteString("\n```\n")
	}

	return b.String()
}

// buildUserMessage creates the initial user message to kick off investigation.
func (ta *TriageAgent) buildUserMessage(step StepResult) string {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("Investigate why step %q failed. ", step.Name))

	if step.Tool != "" && strings.HasPrefix(step.Tool, "browser_") {
		msg.WriteString("This is a browser test step — start by checking browser state with browser_snapshot, then check console messages and network requests.")
	} else if step.Tool == "shell_command" {
		msg.WriteString("This is a shell command step — check the command output, then look at process state and relevant log files.")
	} else {
		msg.WriteString("Examine the error, check relevant system state, and determine the root cause.")
	}

	msg.WriteString("\n\nAfter investigation, output your verdict as a JSON block.")
	return msg.String()
}

// parseVerdict extracts the JSON verdict from the agent's text output.
func (ta *TriageAgent) parseVerdict(analysis string) *TriageVerdict {
	verdict := &TriageVerdict{
		FullAnalysis: analysis,
	}

	// Look for JSON block in ```json ... ``` markers
	jsonBlock := extractJSONBlock(analysis)
	if jsonBlock != "" {
		if err := json.Unmarshal([]byte(jsonBlock), verdict); err != nil {
			if ta.verbose {
				log.Printf("[Triage] Failed to parse verdict JSON: %v", err)
			}
		} else {
			verdict.FullAnalysis = analysis
			return verdict
		}
	}

	// Fallback: try to find raw JSON object
	start := strings.Index(analysis, `{"classification"`)
	if start >= 0 {
		end := strings.Index(analysis[start:], "}")
		if end >= 0 {
			candidate := analysis[start : start+end+1]
			if err := json.Unmarshal([]byte(candidate), verdict); err == nil {
				verdict.FullAnalysis = analysis
				return verdict
			}
		}
	}

	// No parseable verdict — return a default
	verdict.Classification = "environment"
	verdict.Confidence = 0.3
	verdict.RootCause = "Triage agent could not determine root cause (no structured verdict produced)"
	verdict.Recommendation = "Manual investigation required"

	return verdict
}

// extractJSONBlock finds the content between ```json and ``` markers.
func extractJSONBlock(text string) string {
	markers := []string{"```json\n", "```json\r\n", "```json "}
	for _, marker := range markers {
		start := strings.Index(text, marker)
		if start < 0 {
			continue
		}
		content := text[start+len(marker):]
		end := strings.Index(content, "```")
		if end < 0 {
			continue
		}
		return strings.TrimSpace(content[:end])
	}
	return ""
}

// saveTriageArtifacts saves the full analysis text as an artifact.
func (ta *TriageAgent) saveTriageArtifacts(stepName, analysis string) {
	if ta.artifactMgr == nil || analysis == "" {
		return
	}
	ta.artifactMgr.SaveLog(stepName+"_triage", analysis)
}
