package api

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// FlowValidationResult contains the result of validating a flow YAML
type FlowValidationResult struct {
	Valid  bool
	Errors []string
}

// ValidateFlowYAML validates the generated flow YAML against the schema rules
func ValidateFlowYAML(yamlStr string, availableTools []ToolInfo) FlowValidationResult {
	var result FlowValidationResult
	result.Valid = true

	// Parse the YAML
	var flow map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &flow); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Invalid YAML syntax: %v", err))
		return result
	}

	// Build tool name set for validation
	toolNames := make(map[string]bool)
	for _, t := range availableTools {
		toolNames[t.Name] = true
	}

	// Validate required top-level fields
	if _, ok := flow["name"]; !ok {
		result.Errors = append(result.Errors, "Missing required top-level field: name")
	}
	if _, ok := flow["description"]; !ok {
		result.Errors = append(result.Errors, "Missing required top-level field: description")
	}

	// Validate nodes
	nodes, ok := flow["nodes"].([]interface{})
	if !ok {
		result.Errors = append(result.Errors, "Missing or invalid 'nodes' section - must be an array")
	} else {
		nodeNames := make(map[string]bool)
		for i, n := range nodes {
			node, ok := n.(map[string]interface{})
			if !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("Node %d: invalid node format", i))
				continue
			}
			
			nodeName, _ := node["name"].(string)
			if nodeName == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("Node %d: missing required field 'name'", i))
			} else {
				nodeNames[nodeName] = true
			}
			
			nodeType, _ := node["type"].(string)
			if nodeType == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("Node '%s': missing required field 'type'", nodeName))
				continue
			}
			
			// Validate node type specific fields
			switch nodeType {
			case "input":
				// input nodes require output_model
				if _, ok := node["output_model"]; !ok {
					result.Errors = append(result.Errors, fmt.Sprintf("Node '%s' (input): missing required field 'output_model'", nodeName))
				}
			case "llm":
				// LLM nodes should have prompt
				if _, ok := node["prompt"]; !ok {
					result.Errors = append(result.Errors, fmt.Sprintf("Node '%s' (llm): missing required field 'prompt'", nodeName))
				}
				// If tools is true, validate tools_selection
				if tools, ok := node["tools"].(bool); ok && tools {
					if selection, ok := node["tools_selection"].([]interface{}); ok {
						for _, t := range selection {
							toolName, _ := t.(string)
							if toolName != "" && !toolNames[toolName] {
								result.Errors = append(result.Errors, fmt.Sprintf("Node '%s': unknown tool '%s' in tools_selection. Use only tools from Available Tools list.", nodeName, toolName))
							}
						}
					}
				}
			case "output":
				// output nodes require user_message
				if _, ok := node["user_message"]; !ok {
					result.Errors = append(result.Errors, fmt.Sprintf("Node '%s' (output): missing required field 'user_message' (should be an array)", nodeName))
				}
			case "tool":
				// tool nodes require tools_selection
				if _, ok := node["tools_selection"]; !ok {
					result.Errors = append(result.Errors, fmt.Sprintf("Node '%s' (tool): missing required field 'tools_selection'", nodeName))
				} else if selection, ok := node["tools_selection"].([]interface{}); ok {
					for _, t := range selection {
						toolName, _ := t.(string)
						if toolName != "" && !toolNames[toolName] {
							result.Errors = append(result.Errors, fmt.Sprintf("Node '%s': unknown tool '%s'. Use only tools from Available Tools list.", nodeName, toolName))
						}
					}
				}
			case "update_state":
				// update_state nodes require updates
				if _, ok := node["updates"]; !ok {
					result.Errors = append(result.Errors, fmt.Sprintf("Node '%s' (update_state): missing required field 'updates'", nodeName))
				}
			default:
				result.Errors = append(result.Errors, fmt.Sprintf("Node '%s': unknown node type '%s'. Valid types: input, llm, output, tool, update_state", nodeName, nodeType))
			}
		}
		
		// Validate flow edges
		flowEdges, ok := flow["flow"].([]interface{})
		if !ok {
			result.Errors = append(result.Errors, "Missing or invalid 'flow' section - must be an array of edges")
		} else {
			for i, e := range flowEdges {
				edge, ok := e.(map[string]interface{})
				if !ok {
					result.Errors = append(result.Errors, fmt.Sprintf("Flow edge %d: invalid edge format", i))
					continue
				}
				
				from, _ := edge["from"].(string)
				to, _ := edge["to"].(string)
				
				// Validate 'from' references
				if from == "" {
					result.Errors = append(result.Errors, fmt.Sprintf("Flow edge %d: missing 'from' field", i))
				} else if from != "START" && !nodeNames[from] {
					result.Errors = append(result.Errors, fmt.Sprintf("Flow edge %d: 'from' references unknown node '%s'", i, from))
				}
				
				// Validate 'to' references (for simple edges)
				if to != "" {
					if to != "END" && !nodeNames[to] {
						result.Errors = append(result.Errors, fmt.Sprintf("Flow edge %d: 'to' references unknown node '%s'", i, to))
					}
				} else {
					// Check for conditional edges
					edges, ok := edge["edges"].([]interface{})
					if !ok {
						result.Errors = append(result.Errors, fmt.Sprintf("Flow edge %d: must have either 'to' or 'edges' field", i))
					} else {
						for j, ce := range edges {
							condEdge, ok := ce.(map[string]interface{})
							if !ok {
								continue
							}
							condTo, _ := condEdge["to"].(string)
							if condTo != "END" && condTo != "START" && !nodeNames[condTo] {
								result.Errors = append(result.Errors, fmt.Sprintf("Flow edge %d, condition %d: 'to' references unknown node '%s'", i, j, condTo))
							}
						}
					}
				}
			}
		}
	}

	if len(result.Errors) > 0 {
		result.Valid = false
	}
	return result
}

// FormatValidationErrors formats validation errors for LLM feedback
func FormatValidationErrors(errors []string) string {
	var sb strings.Builder
	sb.WriteString("The generated YAML has the following validation errors:\n\n")
	for i, err := range errors {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, err))
	}
	sb.WriteString("\nPlease fix these errors and regenerate the YAML.")
	return sb.String()
}
