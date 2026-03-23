package engine

import (
	"fmt"
	"regexp"
	"strings"
)

var templatePattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// ResolveRef resolves a $. reference against the execution context.
// Examples:
//
//	$.input.x        -> ctx.Input.(map)["x"]
//	$.steps.y.output -> ctx.Steps["y"]["output"]
//	$.steps.y.output.z -> ctx.Steps["y"]["output"].(map)["z"]
func ResolveRef(ref interface{}, ctx *ExecutionContext) interface{} {
	s, ok := ref.(string)
	if !ok || !strings.HasPrefix(s, "$.") {
		return ref
	}

	parts := strings.Split(s[2:], ".") // strip "$."
	if len(parts) == 0 {
		return ref
	}

	switch parts[0] {
	case "input":
		return traverseMap(toMap(ctx.Input), parts[1:])
	case "steps":
		if len(parts) < 2 {
			return nil
		}
		stepID := parts[1]
		stepData, ok := ctx.Steps[stepID]
		if !ok {
			return nil
		}
		return traverseMap(stepData, parts[2:])
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
		// Strip {{ and }}
		key := strings.TrimSpace(match[2 : len(match)-2])
		parts := strings.Split(key, ".")
		val := traverseMap(input, parts)
		if val == nil {
			return match // leave unresolved
		}
		return fmt.Sprintf("%v", val)
	})
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

// traverseMap walks a dotted path through nested maps.
func traverseMap(m map[string]interface{}, path []string) interface{} {
	if m == nil || len(path) == 0 {
		if m == nil {
			return nil
		}
		// Return the map itself when the path is exhausted
		return interface{}(m)
	}

	val, ok := m[path[0]]
	if !ok {
		return nil
	}
	if len(path) == 1 {
		return val
	}
	// Recurse into nested map
	sub := toMap(val)
	if sub == nil {
		return nil
	}
	return traverseMap(sub, path[1:])
}
