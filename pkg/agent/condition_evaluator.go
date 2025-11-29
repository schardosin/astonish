package agent

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// EvaluateCondition evaluates a Python-style condition using Starlark
func EvaluateCondition(conditionStr string, state map[string]interface{}) (bool, error) {
	// Strip "lambda x:" prefix if present
	cleanExpr := conditionStr
	if strings.HasPrefix(strings.TrimSpace(conditionStr), "lambda x:") {
		parts := strings.SplitN(conditionStr, ":", 2)
		if len(parts) == 2 {
			cleanExpr = strings.TrimSpace(parts[1])
		}
	}

	// Convert Go map to Starlark dict
	starlarkDict := convertMapToStarlark(state)

	// Define environment (x = state)
	env := starlark.StringDict{
		"x": starlarkDict,
	}

	// Evaluate expression
	thread := &starlark.Thread{Name: "condition-eval"}
	val, err := starlark.Eval(thread, "<expr>", cleanExpr, env)
	if err != nil {
		return false, fmt.Errorf("evaluation error: %v", err)
	}

	// Return truthiness (convert starlark.Bool to Go bool)
	return bool(val.Truth()), nil
}

// convertMapToStarlark converts a Go map to a Starlark dict
func convertMapToStarlark(m map[string]interface{}) *starlark.Dict {
	dict := starlark.NewDict(len(m))
	for k, v := range m {
		dict.SetKey(starlark.String(k), toStarlarkValue(v))
	}
	return dict
}

// toStarlarkValue converts a Go value to a Starlark value
func toStarlarkValue(v interface{}) starlark.Value {
	if v == nil {
		return starlark.None
	}

	switch val := v.(type) {
	case string:
		return starlark.String(val)
	case int:
		return starlark.MakeInt(val)
	case int64:
		return starlark.MakeInt64(val)
	case float64:
		return starlark.Float(val)
	case bool:
		return starlark.Bool(val)
	case []any:
		list := make([]starlark.Value, 0, len(val))
		for _, item := range val {
			list = append(list, toStarlarkValue(item))
		}
		return starlark.NewList(list)
	case map[string]interface{}:
		return convertMapToStarlark(val)
	default:
		// For unknown types, convert to string
		return starlark.String(fmt.Sprintf("%v", val))
	}
}
