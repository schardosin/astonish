package api

import "sort"

// OrderYamlKeys creates a new map with keys in a consistent order
// Order: name, description, model, nodes, flow, layout, then any remaining keys alphabetically
func OrderYamlKeys(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}

	// Define the preferred key order (model is NOT included - engine uses global config)
	keyOrder := []string{"name", "description", "nodes", "flow", "layout"}
	keyOrderSet := make(map[string]bool)
	for _, k := range keyOrder {
		keyOrderSet[k] = true
	}

	// Use a slice of key-value pairs to preserve order
	// (Go 1.12+ preserves map literal key order, but we need explicit ordering)
	ordered := make(map[string]interface{})

	// First, add keys in preferred order
	for _, key := range keyOrder {
		if val, exists := data[key]; exists {
			ordered[key] = val
		}
	}

	// Then add remaining keys alphabetically
	var remainingKeys []string
	for key := range data {
		if !keyOrderSet[key] {
			remainingKeys = append(remainingKeys, key)
		}
	}
	sort.Strings(remainingKeys)

	for _, key := range remainingKeys {
		ordered[key] = data[key]
	}

	return ordered
}
