package ui

import (
	"fmt"
	"reflect"
	"strings"
)

// FormatAsYamlLike formats a value as a YAML-like string for console output.
// It handles nested maps, slices, and primitives recursively.
func FormatAsYamlLike(v interface{}, indent int) string {
	indentStr := strings.Repeat("  ", indent)

	val := reflect.ValueOf(v)
	if !val.IsValid() {
		return "null"
	}

	// Handle pointers and interfaces
	if val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
		if val.IsNil() {
			return "null"
		}
		return FormatAsYamlLike(val.Elem().Interface(), indent)
	}

	switch val.Kind() {
	case reflect.Slice, reflect.Array:
		if val.Len() == 0 {
			return "[]"
		}
		var sb strings.Builder
		// If it's a list of primitives, we might want a compact representation,
		// but for now, let's stick to the bulleted list format for consistency.
		for i := 0; i < val.Len(); i++ {
			item := val.Index(i).Interface()
			itemStr := FormatAsYamlLike(item, indent+1)
			// Trim leading whitespace from itemStr because we are adding the bullet
			itemStr = strings.TrimLeft(itemStr, " ")

			// If the item is a complex type (map/struct) that produced multiple lines,
			// we need to ensure subsequent lines are indented correctly relative to the bullet.
			// However, FormatAsYamlLike(indent+1) already adds indentation.
			// The first line needs to be handled carefully.

			// Optimization: If item is a map/struct, FormatAsYamlLike will return:
			// "  key: val\n  key2: val2" (indented by indent+1)
			// We want:
			// "- key: val
			//   key2: val2"

			// Let's re-format the item with 0 indent and handle indentation here
			rawItemStr := FormatAsYamlLike(item, 0)
			lines := strings.Split(rawItemStr, "\n")

			sb.WriteString(fmt.Sprintf("\n%s- %s", indentStr, lines[0]))
			for j := 1; j < len(lines); j++ {
				if lines[j] != "" {
					sb.WriteString(fmt.Sprintf("\n%s  %s", indentStr, lines[j]))
				}
			}
		}
		return sb.String()

	case reflect.Map:
		if val.Len() == 0 {
			return "{}"
		}
		var sb strings.Builder
		iter := val.MapRange()

		first := true
		for iter.Next() {
			k := iter.Key()
			v := iter.Value()

			if !first {
				sb.WriteString("\n")
			}
			first = false

			keyStr := fmt.Sprintf("%v", k)
			valStr := FormatAsYamlLike(v.Interface(), indent+1)

			// Check if value is multiline (nested object/list)
			if strings.Contains(valStr, "\n") {
				// If it starts with a newline (list), append directly
				if strings.HasPrefix(valStr, "\n") {
					sb.WriteString(fmt.Sprintf("%s%s:%s", indentStr, keyStr, valStr))
				} else {
					// Nested map, usually starts with indent
					// We need to ensure it starts on a new line if it's a complex object
					// But FormatAsYamlLike returns indented string.
					// Let's simplify:
					// key:
					//   val
					sb.WriteString(fmt.Sprintf("%s%s:\n%s", indentStr, keyStr, valStr))
				}
			} else {
				// Simple value
				sb.WriteString(fmt.Sprintf("%s%s: %s", indentStr, keyStr, strings.TrimSpace(valStr)))
			}
		}
		return sb.String()

	case reflect.Struct:
		// Similar to map but using fields
		t := val.Type()
		var sb strings.Builder
		for i := 0; i < val.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" { // Skip unexported fields
				continue
			}
			if i > 0 {
				sb.WriteString("\n")
			}

			keyStr := field.Name
			valStr := FormatAsYamlLike(val.Field(i).Interface(), indent+1)

			if strings.Contains(valStr, "\n") {
				if strings.HasPrefix(valStr, "\n") {
					sb.WriteString(fmt.Sprintf("%s%s:%s", indentStr, keyStr, valStr))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s:\n%s", indentStr, keyStr, valStr))
				}
			} else {
				sb.WriteString(fmt.Sprintf("%s%s: %s", indentStr, keyStr, strings.TrimSpace(valStr)))
			}
		}
		return sb.String()

	default:
		// Primitive types
		return fmt.Sprintf("%v", v)
	}
}
