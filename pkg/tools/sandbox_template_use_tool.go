package tools

import (
	"fmt"
	"log"
	"strings"

	"github.com/schardosin/astonish/pkg/sandbox"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// UseSandboxTemplateArgs are the arguments for the use_sandbox_template tool.
type UseSandboxTemplateArgs struct {
	// TemplateName is the name of the template to switch to.
	TemplateName string `json:"template_name" jsonschema:"Name of the sandbox template to use (e.g., 'juicytrade'). Must be a template returned by list_sandbox_templates."`
}

// UseSandboxTemplateResult is the result of the use_sandbox_template tool.
type UseSandboxTemplateResult struct {
	Status       string `json:"status"`
	TemplateName string `json:"template_name,omitempty"`
	ContainerIP  string `json:"container_ip,omitempty"`
	Message      string `json:"message"`
}

// useSandboxTemplateDeps holds the dependencies injected by the factory.
type useSandboxTemplateDeps struct {
	nodePool         *sandbox.NodeClientPool
	templateRegistry *sandbox.TemplateRegistry
}

var useSandboxTemplateDepsVar *useSandboxTemplateDeps

// NewUseSandboxTemplateTool creates the use_sandbox_template tool.
// This tool runs on the HOST (not inside the container) so it can tear down
// the current session container and replace it with one cloned from the
// specified template. It is intentionally NOT listed in sandbox.containerTools.
func NewUseSandboxTemplateTool(nodePool *sandbox.NodeClientPool, templateRegistry *sandbox.TemplateRegistry) (tool.Tool, error) {
	useSandboxTemplateDepsVar = &useSandboxTemplateDeps{
		nodePool:         nodePool,
		templateRegistry: templateRegistry,
	}

	t, err := functiontool.New(functiontool.Config{
		Name: "use_sandbox_template",
		Description: "Switch the current sandbox session to use a specific template. " +
			"This tears down the current container (if any) and creates a new one cloned " +
			"from the specified template, which includes all pre-installed dependencies and " +
			"project code. Call this after the user selects a template from list_sandbox_templates. " +
			"After this call, all file and shell tools will operate inside the new container. " +
			"The response includes the container's bridge IP (container_ip field) which is " +
			"needed for browser_navigate calls to reach container services. " +
			"IMPORTANT: This operation takes a few seconds as it recreates the container.",
	}, useSandboxTemplate)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func useSandboxTemplate(ctx tool.Context, args UseSandboxTemplateArgs) (UseSandboxTemplateResult, error) {
	if useSandboxTemplateDepsVar == nil {
		return UseSandboxTemplateResult{
			Status:  "error",
			Message: "Sandbox template system is not initialized. Ensure sandbox mode is enabled.",
		}, nil
	}

	deps := useSandboxTemplateDepsVar

	// Validate template name
	name := strings.TrimSpace(args.TemplateName)
	if name == "" {
		return UseSandboxTemplateResult{
			Status:  "error",
			Message: "template_name is required. Use list_sandbox_templates to see available templates.",
		}, nil
	}

	// Verify template exists in registry
	if err := deps.templateRegistry.Load(); err != nil {
		return UseSandboxTemplateResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to load template registry: %v", err),
		}, nil
	}

	if !deps.templateRegistry.Exists(name) {
		return UseSandboxTemplateResult{
			Status:  "error",
			Message: fmt.Sprintf("Template %q not found. Use list_sandbox_templates to see available templates.", name),
		}, nil
	}

	// Get session ID
	var sessionID string
	if ctx != nil {
		sessionID = ctx.SessionID()
	}
	if sessionID == "" {
		return UseSandboxTemplateResult{
			Status:  "error",
			Message: "No session ID available. Cannot determine which container to replace.",
		}, nil
	}

	// Replace the session container with one cloned from the selected template
	log.Printf("[sandbox-template] Replacing session %s container with template %q...", sessionID[:min(8, len(sessionID))], name)
	if err := deps.nodePool.ReplaceSession(sessionID, name); err != nil {
		return UseSandboxTemplateResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to switch to template %q: %v", name, err),
		}, nil
	}

	log.Printf("[sandbox-template] Session %s now using template %q", sessionID[:min(8, len(sessionID))], name)

	// Eagerly bind the new session to trigger container creation, then
	// discover the container's bridge IP. This lets the AI know the IP
	// immediately instead of requiring a separate hostname -I call.
	var containerIP string
	if client := deps.nodePool.GetOrCreate(sessionID); client != nil {
		if ip, err := client.GetContainerIP(sessionID); err == nil {
			containerIP = ip
			log.Printf("[sandbox-template] Session %s container IP: %s", sessionID[:min(8, len(sessionID))], ip)
		} else {
			log.Printf("[sandbox-template] Warning: could not discover container IP for session %s: %v", sessionID[:min(8, len(sessionID))], err)
		}
	}

	msg := fmt.Sprintf("Sandbox container switched to template %q. All file and shell tools now "+
		"operate inside a container with the template's pre-installed dependencies and project code.", name)
	if containerIP != "" {
		msg += fmt.Sprintf(" The container's bridge IP is %s — use this IP (not localhost) in "+
			"browser_navigate URLs to reach services running in the container.", containerIP)
	}

	return UseSandboxTemplateResult{
		Status:       "ok",
		TemplateName: name,
		ContainerIP:  containerIP,
		Message:      msg,
	}, nil
}
