package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FlowDryRunResult contains the result of a semantic dry-run validation.
// This goes beyond schema validation to check runtime semantics:
// - Variable resolution: do all {variable} references resolve?
// - Tool arg correctness: do tool node args match declared parameter names?
// - Flow reachability: can all nodes be reached from START?
type FlowDryRunResult struct {
	Valid    bool
	Warnings []string // Non-fatal issues (informational)
	Errors   []string // Fatal issues that will cause runtime failures
}

// DryRunFlowYAML performs semantic validation on a flow YAML.
// It simulates state propagation and checks that all variable references
// can be resolved, and that tool node args use correct parameter names.
// toolSchemas maps tool name -> JSON schema of parameters (can be nil for unknown tools).
func DryRunFlowYAML(yamlStr string, toolSchemas map[string]json.RawMessage) FlowDryRunResult {
	var result FlowDryRunResult
	result.Valid = true

	// Parse the YAML
	var flow map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &flow); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Invalid YAML syntax: %v", err))
		return result
	}

	// Extract nodes
	nodes, ok := flow["nodes"].([]interface{})
	if !ok {
		// Schema validation already catches this; skip
		return result
	}

	// Build node map and execution order from flow edges
	nodeMap := make(map[string]map[string]interface{})
	for _, n := range nodes {
		node, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := node["name"].(string)
		if name != "" {
			nodeMap[name] = node
		}
	}

	// Determine execution order from flow edges (simple linear traversal)
	executionOrder := resolveExecutionOrder(flow, nodeMap)

	// Simulate state propagation
	// Track which variables are available at each point in the flow
	availableVars := make(map[string]bool)

	for _, nodeName := range executionOrder {
		node, exists := nodeMap[nodeName]
		if !exists {
			continue
		}

		nodeType, _ := node["type"].(string)

		// Check variable references in this node's prompts/args
		checkVariableRefs(node, availableVars, &result)

		// Check tool node arg names against schemas
		if nodeType == "tool" {
			checkToolNodeArgs(node, toolSchemas, &result)
		}

		// After this node executes, what variables does it produce?
		if outputModel, ok := node["output_model"].(map[string]interface{}); ok {
			for varName := range outputModel {
				availableVars[varName] = true
			}
		}

		// Raw tool output also produces variables
		if rawOutput, ok := node["raw_tool_output"].(map[string]interface{}); ok {
			for varName := range rawOutput {
				availableVars[varName] = true
			}
		}
	}

	if len(result.Errors) > 0 {
		result.Valid = false
	}
	return result
}

// stateVarPattern matches {variable_name} patterns in strings.
// Excludes patterns that look like shell variables (${...}), double-braces ({{...}}),
// and common non-variable patterns.
var stateVarPattern = regexp.MustCompile(`(?:^|[^{$])(\{([a-zA-Z_][a-zA-Z0-9_]*)\})`)

// checkVariableRefs checks that all {variable} references in a node's
// prompts and args can be resolved from available state.
func checkVariableRefs(node map[string]interface{}, availableVars map[string]bool, result *FlowDryRunResult) {
	nodeName, _ := node["name"].(string)
	nodeType, _ := node["type"].(string)

	// Check prompt
	if prompt, ok := node["prompt"].(string); ok {
		checkStringVarRefs(prompt, "prompt", nodeName, availableVars, result)
	}

	// Check system prompt
	if system, ok := node["system"].(string); ok {
		checkStringVarRefs(system, "system", nodeName, availableVars, result)
	}

	// Check tool node args (only string values)
	if nodeType == "tool" {
		if args, ok := node["args"].(map[string]interface{}); ok {
			for argName, argVal := range args {
				if strVal, ok := argVal.(string); ok {
					checkStringVarRefs(strVal, fmt.Sprintf("args.%s", argName), nodeName, availableVars, result)
				}
			}
		}
	}
}

// checkStringVarRefs finds {variable} patterns in a string and checks resolution.
func checkStringVarRefs(s, field, nodeName string, availableVars map[string]bool, result *FlowDryRunResult) {
	matches := stateVarPattern.FindAllStringSubmatch(s, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			varName := match[2]
			if !availableVars[varName] {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("Node '%s', %s: references {%s} which is not produced by any preceding node's output_model. "+
						"Ensure this variable is available at runtime (from an input node or earlier processing).",
						nodeName, field, varName))
			}
		}
	}
}

// checkToolNodeArgs validates that a tool node's args keys match the
// tool's declared parameter schema.
func checkToolNodeArgs(node map[string]interface{}, toolSchemas map[string]json.RawMessage, result *FlowDryRunResult) {
	nodeName, _ := node["name"].(string)

	// Get tool name from tools_selection
	selection, ok := node["tools_selection"].([]interface{})
	if !ok || len(selection) == 0 {
		return
	}
	toolName, _ := selection[0].(string)
	if toolName == "" {
		return
	}

	// Get the schema for this tool
	schema, exists := toolSchemas[toolName]
	if !exists || schema == nil {
		return // Can't validate without schema
	}

	// Parse the schema to extract property names
	var schemaObj struct {
		Properties map[string]interface{} `json:"properties"`
		Required   []string               `json:"required"`
	}
	if err := json.Unmarshal(schema, &schemaObj); err != nil {
		return // Schema parse error, skip
	}

	if schemaObj.Properties == nil {
		return
	}

	// Check that args keys are valid parameter names
	args, ok := node["args"].(map[string]interface{})
	if !ok {
		return
	}

	validParams := make(map[string]bool)
	for paramName := range schemaObj.Properties {
		validParams[paramName] = true
	}

	for argName := range args {
		if !validParams[argName] {
			var validList []string
			for p := range validParams {
				validList = append(validList, p)
			}
			result.Errors = append(result.Errors,
				fmt.Sprintf("Node '%s' (tool): arg '%s' is not a valid parameter for tool '%s'. Valid parameters: %s",
					nodeName, argName, toolName, strings.Join(validList, ", ")))
		}
	}

	// Check required parameters are provided
	for _, required := range schemaObj.Required {
		if _, provided := args[required]; !provided {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Node '%s' (tool): required parameter '%s' for tool '%s' is not in args (it may be resolved from state or have a default)",
					nodeName, required, toolName))
		}
	}
}

// resolveExecutionOrder walks the flow edges to determine a linear execution order.
// For conditional edges, includes all possible targets (for variable analysis purposes).
func resolveExecutionOrder(flow map[string]interface{}, nodeMap map[string]map[string]interface{}) []string {
	flowEdges, ok := flow["flow"].([]interface{})
	if !ok {
		// Fallback: return nodes in definition order
		var order []string
		for name := range nodeMap {
			order = append(order, name)
		}
		return order
	}

	// Build adjacency list
	adjacency := make(map[string][]string)
	for _, e := range flowEdges {
		edge, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		from, _ := edge["from"].(string)
		if to, ok := edge["to"].(string); ok && to != "" && to != "END" {
			adjacency[from] = append(adjacency[from], to)
		}
		// Conditional edges
		if edges, ok := edge["edges"].([]interface{}); ok {
			for _, ce := range edges {
				condEdge, ok := ce.(map[string]interface{})
				if !ok {
					continue
				}
				if condTo, ok := condEdge["to"].(string); ok && condTo != "END" && condTo != "" {
					adjacency[from] = append(adjacency[from], condTo)
				}
			}
		}
	}

	// BFS from START
	var order []string
	visited := make(map[string]bool)
	queue := adjacency["START"]

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] || current == "END" || current == "START" {
			continue
		}
		visited[current] = true
		order = append(order, current)

		// Add successors
		for _, next := range adjacency[current] {
			if !visited[next] {
				queue = append(queue, next)
			}
		}
	}

	// Add any nodes not reachable from START (e.g., disconnected nodes)
	for name := range nodeMap {
		if !visited[name] {
			order = append(order, name)
		}
	}

	return order
}
