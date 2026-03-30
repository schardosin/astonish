package common

import (
	"encoding/json"
)

// ExtractToolInputSchema extracts the input schema from a tool that implements Declaration().
// Returns nil if the tool doesn't have a declaration or input schema.
func ExtractToolInputSchema(t interface{ Name() string }) json.RawMessage {
	dt, ok := t.(ToolWithDeclaration)
	if !ok {
		return nil
	}
	decl := dt.Declaration()
	if decl == nil || decl.ParametersJsonSchema == nil {
		return nil
	}
	data, err := json.Marshal(decl.ParametersJsonSchema)
	if err != nil {
		return nil
	}
	if string(data) == "null" {
		return nil
	}
	return data
}
