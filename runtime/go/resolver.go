package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var templatePattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// ResolveRef resolves a $. reference against the execution context.
// Examples:
//
//	$.input.x              -> ctx.Input.(map)["x"]
//	$.steps.y.output       -> ctx.Steps["y"]["output"]
//	$.steps.y.output.z     -> ctx.Steps["y"]["output"].(map)["z"]
//	$.steps.y.output[0]    -> ctx.Steps["y"]["output"].([]interface{})[0]
//	$.steps.y.output[0].z  -> ctx.Steps["y"]["output"].([]interface{})[0].(map)["z"]
//	$.current              -> ctx.Steps["__current"]["output"]
func ResolveRef(ref interface{}, ctx *ExecutionContext) interface{} {
	s, ok := ref.(string)
	if !ok || !strings.HasPrefix(s, "$.") {
		return ref
	}

	parts := tokenizePath(s[2:]) // strip "$."
	if len(parts) == 0 {
		return ref
	}

	switch parts[0] {
	case "input":
		return traversePath(toMap(ctx.Input), parts[1:])
	case "current":
		// $.current is sugar for $.steps.__current.output
		if stepData, ok := ctx.Steps["__current"]; ok {
			if len(parts) > 1 {
				return traversePath(toMap(stepData["output"]), parts[1:])
			}
			return stepData["output"]
		}
		return nil
	case "steps":
		if len(parts) < 2 {
			return nil
		}
		stepID := parts[1]
		stepData, ok := ctx.Steps[stepID]
		if !ok {
			return nil
		}
		return traversePath(stepData, parts[2:])
	default:
		return ref
	}
}

// ResolveInputs resolves a map of input bindings, where values may be $. refs.
func ResolveInputs(bindings map[string]interface{}, ctx *ExecutionContext) map[string]interface{} {
	result := make(map[string]interface{}, len(bindings))
	for k, v := range bindings {
		result[k] = ResolveRef(v, ctx)
	}
	return result
}

// ResolveOutputs resolves the workflow output bindings.
func ResolveOutputs(bindings map[string]string, ctx *ExecutionContext) map[string]interface{} {
	result := make(map[string]interface{}, len(bindings))
	for k, v := range bindings {
		result[k] = ResolveRef(v, ctx)
	}
	return result
}

// ResolveTemplate replaces {{key}} and {{key.sub}} placeholders with values
// from the input map.
func ResolveTemplate(template string, input map[string]interface{}) string {
	return templatePattern.ReplaceAllStringFunc(template, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		parts := strings.Split(key, ".")
		val := traverseMap(input, parts)
		if val == nil {
			return match // leave unresolved
		}
		return fmt.Sprintf("%v", val)
	})
}

// tokenizePath splits a dotted path, handling array indices like "output[0]".
// "steps.fetch.output[0].name" -> ["steps", "fetch", "output", "[0]", "name"]
func tokenizePath(path string) []string {
	var tokens []string
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		// Check for array index: "field[N]"
		if idx := strings.Index(part, "["); idx != -1 {
			field := part[:idx]
			if field != "" {
				tokens = append(tokens, field)
			}
			// Extract "[N]"
			tokens = append(tokens, part[idx:])
		} else {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

// traversePath walks a path through nested maps and arrays.
func traversePath(m map[string]interface{}, path []string) interface{} {
	if m == nil || len(path) == 0 {
		if m == nil {
			return nil
		}
		return interface{}(m)
	}

	// Check if this is an array index token
	if strings.HasPrefix(path[0], "[") {
		// Can't index into a map
		return nil
	}

	val, ok := m[path[0]]
	if !ok {
		return nil
	}

	if len(path) == 1 {
		return val
	}

	remaining := path[1:]

	// Next token might be an array index
	if strings.HasPrefix(remaining[0], "[") {
		indexStr := strings.Trim(remaining[0], "[]")
		idx, err := strconv.Atoi(indexStr)
		if err != nil {
			return nil
		}
		arr, ok := val.([]interface{})
		if !ok || idx < 0 || idx >= len(arr) {
			return nil
		}
		if len(remaining) == 1 {
			return arr[idx]
		}
		// Continue traversing into the indexed element
		sub := toMap(arr[idx])
		return traversePath(sub, remaining[1:])
	}

	// Recurse into nested map
	sub := toMap(val)
	if sub == nil {
		return nil
	}
	return traversePath(sub, remaining)
}

// toMap converts an interface{} to map[string]interface{} if possible.
func toMap(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// traverseMap walks a dotted path through nested maps (no array support).
// Used by ResolveTemplate and condition evaluation.
func traverseMap(m map[string]interface{}, path []string) interface{} {
	if m == nil || len(path) == 0 {
		if m == nil {
			return nil
		}
		return interface{}(m)
	}

	val, ok := m[path[0]]
	if !ok {
		return nil
	}
	if len(path) == 1 {
		return val
	}
	sub := toMap(val)
	if sub == nil {
		return nil
	}
	return traverseMap(sub, path[1:])
}
