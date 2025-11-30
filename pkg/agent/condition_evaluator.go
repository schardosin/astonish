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

// EvaluateExpression evaluates a Python-style expression using Starlark and returns the result
func EvaluateExpression(expr string, state map[string]interface{}) (interface{}, error) {
	// Convert Go map to Starlark dict
	starlarkDict := convertMapToStarlark(state)

	// Define environment (x = state, but also expose top-level keys directly for convenience)
	env := starlark.StringDict{
		"x": starlarkDict,
	}
	
	// Also expose top-level keys directly
	for k, v := range state {
		env[k] = toStarlarkValue(v)
	}

	// Evaluate expression
	thread := &starlark.Thread{Name: "expr-eval"}
	val, err := starlark.Eval(thread, "<expr>", expr, env)
	if err != nil {
		return nil, fmt.Errorf("evaluation error: %v", err)
	}

	return fromStarlarkValue(val), nil
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

// fromStarlarkValue converts a Starlark value back to a Go value
func fromStarlarkValue(v starlark.Value) interface{} {
	switch val := v.(type) {
	case starlark.String:
		return string(val)
	case starlark.Int:
		if i, ok := val.Int64(); ok {
			return int(i) // Return as int for convenience, or int64?
		}
		return val.BigInt()
	case starlark.Float:
		return float64(val)
	case starlark.Bool:
		return bool(val)
	case *starlark.List:
		var list []interface{}
		iter := val.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			list = append(list, fromStarlarkValue(item))
		}
		return list
	case *starlark.Dict:
		dict := make(map[string]interface{})
		for _, item := range val.Keys() {
			if keyStr, ok := item.(starlark.String); ok {
				value, _, _ := val.Get(item)
				dict[string(keyStr)] = fromStarlarkValue(value)
			}
		}
		return dict
	case starlark.NoneType:
		return nil
	default:
		return val.String()
	}
}
