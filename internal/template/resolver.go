package template

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var tokenRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Context holds all values available during template resolution.
type Context struct {
	NodeOutputs map[string]interface{} // keyed by node ID e.g. "n1"
	Env         map[string]string
	Vars        map[string]interface{}
}

// Resolve replaces all {{...}} tokens in input with values from ctx.
func Resolve(input string, ctx *Context) (string, error) {
	var resolveErr error
	result := tokenRe.ReplaceAllStringFunc(input, func(match string) string {
		if resolveErr != nil {
			return match
		}
		// Strip {{ and }}
		token := match[2 : len(match)-2]
		val, err := resolveToken(token, ctx)
		if err != nil {
			resolveErr = err
			return match
		}
		return val
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return result, nil
}

// resolveToken resolves a single token (without braces) against ctx.
func resolveToken(token string, ctx *Context) (string, error) {
	parts := strings.SplitN(token, ".", 2)
	namespace := parts[0]

	switch namespace {
	case "env":
		if len(parts) < 2 {
			return "", fmt.Errorf("template: key not found: {{%s}}", token)
		}
		key := parts[1]
		v, ok := ctx.Env[key]
		if !ok {
			return "", fmt.Errorf("template: key not found: {{%s}}", token)
		}
		return v, nil

	case "vars":
		if len(parts) < 2 {
			return "", fmt.Errorf("template: key not found: {{%s}}", token)
		}
		key := parts[1]
		v, ok := ctx.Vars[key]
		if !ok {
			return "", fmt.Errorf("template: key not found: {{%s}}", token)
		}
		return fmt.Sprintf("%v", v), nil

	default:
		// Treat namespace as a node ID; remaining path is dot-notation.
		nodeID := namespace
		root, ok := ctx.NodeOutputs[nodeID]
		if !ok {
			return "", fmt.Errorf("template: key not found: {{%s}}", token)
		}
		if len(parts) < 2 {
			return fmt.Sprintf("%v", root), nil
		}
		val, err := navigatePath(root, parts[1], token)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%v", val), nil
	}
}

// navigatePath traverses nested maps/slices using dot-notation path segments.
func navigatePath(current interface{}, path string, token string) (interface{}, error) {
	segments := strings.Split(path, ".")
	for _, seg := range segments {
		if current == nil {
			return nil, fmt.Errorf("template: key not found: {{%s}}", token)
		}
		switch node := current.(type) {
		case map[string]interface{}:
			val, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("template: key not found: {{%s}}", token)
			}
			current = val
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("template: key not found: {{%s}}", token)
			}
			current = node[idx]
		default:
			return nil, fmt.Errorf("template: key not found: {{%s}}", token)
		}
	}
	return current, nil
}

// ResolveJSON walks every string value in the JSON tree and calls Resolve on it.
func ResolveJSON(raw json.RawMessage, ctx *Context) (json.RawMessage, error) {
	// Unmarshal into a generic value.
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	resolved, err := resolveValue(v, ctx)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	return out, nil
}

// resolveValue recursively walks generic JSON values and resolves string leaves.
func resolveValue(v interface{}, ctx *Context) (interface{}, error) {
	switch val := v.(type) {
	case string:
		return Resolve(val, ctx)
	case map[string]interface{}:
		for k, elem := range val {
			resolved, err := resolveValue(elem, ctx)
			if err != nil {
				return nil, err
			}
			val[k] = resolved
		}
		return val, nil
	case []interface{}:
		for i, elem := range val {
			resolved, err := resolveValue(elem, ctx)
			if err != nil {
				return nil, err
			}
			val[i] = resolved
		}
		return val, nil
	default:
		return v, nil
	}
}
