package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"gopkg.in/yaml.v3"
)

// GenerateFlowKnowledgeDoc creates a markdown knowledge document from a flow YAML.
// This document is indexed in the vector store for semantic flow matching.
// No LLM calls are needed — everything is parsed mechanically from the YAML.
//
// All {variable} patterns in node prompts are escaped to <variable> to prevent
// ADK's InjectSessionState from trying to resolve them when the knowledge text
// is injected into the system prompt.
func GenerateFlowKnowledgeDoc(flowYAMLContent string, entry FlowRegistryEntry) string {
	var sb strings.Builder

	// Parse the YAML
	var agentCfg config.AgentConfig
	if err := yaml.Unmarshal([]byte(flowYAMLContent), &agentCfg); err != nil {
		// Fallback: generate a minimal doc from registry entry only
		sb.WriteString(fmt.Sprintf("# %s\n\n", strings.TrimSuffix(entry.FlowFile, ".yaml")))
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", entry.Description))
		if len(entry.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("**Tags:** %s\n", strings.Join(entry.Tags, ", ")))
		}
		sb.WriteString(fmt.Sprintf("**Flow file:** %s\n", entry.FlowFile))
		return sb.String()
	}

	flowName := strings.TrimSuffix(entry.FlowFile, ".yaml")
	description := entry.Description
	if description == "" {
		description = agentCfg.Description
	}

	sb.WriteString(fmt.Sprintf("# %s\n\n", flowName))
	sb.WriteString(fmt.Sprintf("**Description:** %s\n", description))
	if len(entry.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("**Tags:** %s\n", strings.Join(entry.Tags, ", ")))
	}
	sb.WriteString(fmt.Sprintf("**Flow file:** %s\n", entry.FlowFile))
	if !entry.CreatedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("**Created:** %s\n", entry.CreatedAt.Format("2006-01-02")))
	}

	// Content hash for change detection
	hash := sha256.Sum256([]byte(flowYAMLContent))
	sb.WriteString(fmt.Sprintf("**Hash:** %x\n", hash[:8]))
	sb.WriteString("\n")

	// Parameters (input nodes)
	var inputs []config.Node
	for _, node := range agentCfg.Nodes {
		if node.Type == "input" {
			inputs = append(inputs, node)
		}
	}
	if len(inputs) > 0 {
		sb.WriteString("## Parameters\n")
		for _, inp := range inputs {
			prompt := inp.Prompt
			if prompt == "" {
				prompt = "(user provides value)"
			}
			prompt = EscapeCurlyPlaceholders(prompt)
			// Truncate long prompts
			if len(prompt) > 100 {
				prompt = prompt[:97] + "..."
			}
			sb.WriteString(fmt.Sprintf("- `%s`: %s\n", inp.Name, prompt))
			if len(inp.OutputModel) > 0 {
				var fields []string
				for k := range inp.OutputModel {
					fields = append(fields, k)
				}
				sort.Strings(fields)
				sb.WriteString(fmt.Sprintf("  Fields: %s\n", strings.Join(fields, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	// Tools used (collected from all nodes)
	toolSet := make(map[string]bool)
	for _, node := range agentCfg.Nodes {
		for _, t := range node.ToolsSelection {
			toolSet[t] = true
		}
	}
	if len(toolSet) > 0 {
		var toolNames []string
		for t := range toolSet {
			toolNames = append(toolNames, t)
		}
		sort.Strings(toolNames)
		sb.WriteString("## Tools Used\n")
		for _, t := range toolNames {
			sb.WriteString(fmt.Sprintf("- %s\n", t))
		}
		sb.WriteString("\n")
	}

	// Workflow steps
	sb.WriteString("## Workflow Steps\n")
	for i, node := range agentCfg.Nodes {
		stepDesc := node.Type
		if node.Prompt != "" {
			prompt := EscapeCurlyPlaceholders(node.Prompt)
			if len(prompt) > 80 {
				prompt = prompt[:77] + "..."
			}
			stepDesc += ": " + prompt
		} else if node.System != "" {
			sys := EscapeCurlyPlaceholders(node.System)
			if len(sys) > 80 {
				sys = sys[:77] + "..."
			}
			stepDesc += ": " + sys
		}
		sb.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, node.Name, stepDesc))
	}
	sb.WriteString("\n")

	// Search keywords — derived from description, tags, tool names, node names
	sb.WriteString("## Search Keywords\n")
	var keywords []string
	// From description words
	for _, word := range strings.Fields(strings.ToLower(description)) {
		word = strings.Trim(word, ".,;:!?()[]{}\"'")
		if len(word) > 2 {
			keywords = append(keywords, word)
		}
	}
	// From tags
	keywords = append(keywords, entry.Tags...)
	// From tool names
	for t := range toolSet {
		keywords = append(keywords, t)
	}
	// From node names
	for _, node := range agentCfg.Nodes {
		keywords = append(keywords, strings.ReplaceAll(node.Name, "_", " "))
	}
	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, kw := range keywords {
		lower := strings.ToLower(kw)
		if !seen[lower] {
			seen[lower] = true
			unique = append(unique, lower)
		}
	}
	sb.WriteString(strings.Join(unique, " "))
	sb.WriteString("\n")

	return sb.String()
}

// EscapeCurlyPlaceholders replaces {variable} patterns with <variable> to
// prevent ADK's session state resolver from treating them as state keys.
// Uses the curlyPlaceholder regex defined in flow_context.go.
func EscapeCurlyPlaceholders(s string) string {
	return curlyPlaceholder.ReplaceAllString(s, "<$1>")
}

// ReconcileFlowKnowledge ensures memory/flows/ docs are in sync with flow YAML files.
// For each flow in the registry:
//   - If no knowledge doc exists, generate one
//   - If the flow YAML has changed (hash mismatch), regenerate
//   - If a knowledge doc exists but the flow YAML is gone, remove it
//
// No LLM calls are made — all generation is mechanical YAML parsing.
func ReconcileFlowKnowledge(flowsDir, memoryFlowsDir string, entries []FlowRegistryEntry) error {
	// Ensure flows directory exists
	if err := os.MkdirAll(memoryFlowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create memory flows directory: %w", err)
	}

	// Track which knowledge docs should exist
	expectedDocs := make(map[string]bool)

	for _, entry := range entries {
		flowName := strings.TrimSuffix(entry.FlowFile, ".yaml")
		docName := flowName + ".md"
		expectedDocs[docName] = true

		flowPath := filepath.Join(flowsDir, entry.FlowFile)
		docPath := filepath.Join(memoryFlowsDir, docName)

		// Read the flow YAML
		flowData, err := os.ReadFile(flowPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Flow YAML is gone — clean up any stale knowledge doc
				os.Remove(docPath)
				expectedDocs[docName] = false
				continue
			}
			continue // skip on other errors
		}

		// Check if knowledge doc needs regeneration by comparing hashes
		flowHash := fmt.Sprintf("%x", sha256.Sum256(flowData))
		needsRegen := true

		if existingDoc, readErr := os.ReadFile(docPath); readErr == nil {
			// Extract hash from existing doc
			for _, line := range strings.Split(string(existingDoc), "\n") {
				if strings.HasPrefix(line, "**Hash:**") {
					existingHash := strings.TrimSpace(strings.TrimPrefix(line, "**Hash:**"))
					if existingHash == flowHash[:16] {
						needsRegen = false
					}
					break
				}
			}
		}

		if needsRegen {
			doc := GenerateFlowKnowledgeDoc(string(flowData), entry)
			if err := os.WriteFile(docPath, []byte(doc), 0644); err != nil {
				continue // best effort
			}
		}
	}

	// Remove orphaned knowledge docs (docs with no corresponding flow)
	dirEntries, err := os.ReadDir(memoryFlowsDir)
	if err != nil {
		return nil // non-fatal
	}
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		if !expectedDocs[de.Name()] {
			os.Remove(filepath.Join(memoryFlowsDir, de.Name()))
		}
	}

	return nil
}
