package tools

import (
	"fmt"
	"log"
	"strings"

	"github.com/schardosin/astonish/pkg/sandbox"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SaveSandboxTemplateArgs are the arguments for the save_sandbox_template tool.
type SaveSandboxTemplateArgs struct {
	// TemplateName is the name for the new template (lowercase, hyphens).
	// This is the value that goes into save_fleet_plan's template field.
	TemplateName string `json:"template_name" jsonschema:"Name for the new sandbox template (lowercase, hyphens, e.g., 'my-project'). This is the value you'll pass to save_fleet_plan's template field."`
	// Description is a human-readable description of what's installed.
	Description string `json:"description,omitempty" jsonschema:"Human-readable description of the template contents (e.g., 'Go 1.24 + node 22 + project dependencies')"`
}

// SaveSandboxTemplateResult is the result of the save_sandbox_template tool.
type SaveSandboxTemplateResult struct {
	Status       string `json:"status"`
	TemplateName string `json:"template_name,omitempty"`
	Message      string `json:"message"`
}

// sandboxTemplateDeps holds the dependencies injected by the factory via closure.
// This follows the same closure capture pattern used by browser and email tools.
type sandboxTemplateDeps struct {
	nodePool         *sandbox.NodeClientPool
	incusClient      *sandbox.IncusClient
	templateRegistry *sandbox.TemplateRegistry
}

var sandboxTemplateDepsVar *sandboxTemplateDeps

// NewSaveSandboxTemplateTool creates the save_sandbox_template tool with the
// given dependencies captured from the factory scope. Returns the tool and
// an error if creation fails.
//
// This tool freezes the current sandbox container as a reusable template.
// The wizard calls it after installing all project dependencies and cloning
// the repo inside the container. Later, fleet sessions clone from this
// custom template instead of @base.
func NewSaveSandboxTemplateTool(nodePool *sandbox.NodeClientPool, incusClient *sandbox.IncusClient, templateRegistry *sandbox.TemplateRegistry) (tool.Tool, error) {
	sandboxTemplateDepsVar = &sandboxTemplateDeps{
		nodePool:         nodePool,
		incusClient:      incusClient,
		templateRegistry: templateRegistry,
	}

	t, err := functiontool.New(functiontool.Config{
		Name: "save_sandbox_template",
		Description: "Freeze the current sandbox container as a reusable template. " +
			"Call this after cloning the project repo, installing dependencies, and configuring the " +
			"development environment inside the container. The template captures the entire container " +
			"state so future fleet sessions start with everything pre-installed. " +
			"The returned template_name should be passed to save_fleet_plan's template field. " +
			"IMPORTANT: This tool stops the container node process, snapshots the container, " +
			"and restarts the node. Tool calls will be temporarily unavailable during the snapshot.",
	}, saveSandboxTemplate)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func saveSandboxTemplate(ctx tool.Context, args SaveSandboxTemplateArgs) (SaveSandboxTemplateResult, error) {
	if sandboxTemplateDepsVar == nil {
		return SaveSandboxTemplateResult{
			Status:  "error",
			Message: "Sandbox template system is not initialized. Ensure sandbox mode is enabled.",
		}, nil
	}

	deps := sandboxTemplateDepsVar

	// Get session ID to find the right container
	var sessionID string
	if ctx != nil {
		sessionID = ctx.SessionID()
	}
	if sessionID == "" {
		return SaveSandboxTemplateResult{
			Status:  "error",
			Message: "No session ID available. Cannot determine which container to snapshot.",
		}, nil
	}

	// Validate args
	name := strings.TrimSpace(args.TemplateName)
	if name == "" {
		return SaveSandboxTemplateResult{
			Status:  "error",
			Message: "template_name is required. Use a lowercase, hyphenated name like 'my-project'.",
		}, nil
	}

	if name == sandbox.BaseTemplate {
		return SaveSandboxTemplateResult{
			Status:  "error",
			Message: "Cannot use 'base' as a template name (reserved).",
		}, nil
	}

	// Get the container name for this session from the pool
	containerName := deps.nodePool.GetContainerName(sessionID)
	if containerName == "" {
		return SaveSandboxTemplateResult{
			Status:  "error",
			Message: "No active sandbox container for this session. The container must be running before creating a template.",
		}, nil
	}

	// 1. Stop the node process (must be quiescent for snapshot)
	log.Printf("[sandbox-template] Stopping node for session %s template creation...", sessionID[:min(8, len(sessionID))])
	if err := deps.nodePool.StopNode(sessionID); err != nil {
		log.Printf("[sandbox-template] Warning: failed to stop node: %v (continuing anyway)", err)
	}

	// 2. Create the template from the container
	log.Printf("[sandbox-template] Creating template %q from container %q...", name, containerName)
	if err := sandbox.CreateTemplateFromContainer(
		deps.incusClient,
		deps.templateRegistry,
		containerName,
		name,
		strings.TrimSpace(args.Description),
	); err != nil {
		// Try to restart node even on failure
		if restartErr := deps.nodePool.RestartNode(sessionID); restartErr != nil {
			log.Printf("[sandbox-template] Warning: failed to restart node after template creation failure: %v", restartErr)
		}
		return SaveSandboxTemplateResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to create template: %v", err),
		}, nil
	}

	// 3. Restart the node process so the session can continue
	log.Printf("[sandbox-template] Restarting node...")
	if err := deps.nodePool.RestartNode(sessionID); err != nil {
		return SaveSandboxTemplateResult{
			Status:       "warning",
			TemplateName: name,
			Message: fmt.Sprintf("Template %q created successfully, but failed to restart the node: %v. "+
				"You may need to restart the session.", name, err),
		}, nil
	}

	return SaveSandboxTemplateResult{
		Status:       "saved",
		TemplateName: name,
		Message: fmt.Sprintf("Template %q created and ready for cloning. "+
			"Pass template: %q to save_fleet_plan to bind fleet sessions to this template.", name, name),
	}, nil
}
