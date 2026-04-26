package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var tokenRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// ErrNotFound is returned when a template path does not resolve (missing key, bad index, etc.).
var ErrNotFound = errors.New("template: not found")

// Context holds all values available during template resolution.
type Context struct {
	NodeOutputs map[string]interface{} // keyed by node ID e.g. "n1"
	Env         map[string]string
	Vars        map[string]interface{}
}

type pathSegment struct {
	key   string
	index *int // used with key for name[3] forms; nil if plain key or legacy numeric segment
	star  bool
}

func parseSegment(seg string) (pathSegment, error) {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return pathSegment{}, fmt.Errorf("template: empty path segment")
	}
	if i := strings.Index(seg, "["); i >= 0 {
		key := seg[:i]
		if !strings.HasSuffix(seg, "]") {
			return pathSegment{}, fmt.Errorf("template: bad bracket in %q", seg)
		}
		inner := seg[i+1 : len(seg)-1]
		if inner == "*" {
			if key == "" {
				return pathSegment{}, fmt.Errorf("template: invalid [*] segment")
			}
			return pathSegment{key: key, star: true}, nil
		}
		n, err := strconv.Atoi(inner)
		if err != nil {
			return pathSegment{}, fmt.Errorf("template: bad index in %q", seg)
		}
		if key == "" {
			return pathSegment{}, fmt.Errorf("template: missing key before [%s]", inner)
		}
		return pathSegment{key: key, index: &n}, nil
	}
	return pathSegment{key: seg}, nil
}

func parsePath(path string) ([]pathSegment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	parts := strings.Split(path, ".")
	segs := make([]pathSegment, 0, len(parts))
	for _, p := range parts {
		s, err := parseSegment(p)
		if err != nil {
			return nil, err
		}
		segs = append(segs, s)
	}
	return segs, nil
}

func normalizeSliceIndex(idx, length int) (int, bool) {
	if length == 0 {
		return 0, false
	}
	if idx < 0 {
		idx = length + idx
	}
	if idx < 0 || idx >= length {
		return 0, false
	}
	return idx, true
}

func navigateFrom(cur interface{}, segs []pathSegment, token string) (interface{}, error) {
	if len(segs) == 0 {
		return cur, nil
	}
	s0 := segs[0]
	rest := segs[1:]

	if cur == nil {
		return nil, notFoundErr(token)
	}

	// messages[*].rest — project over array at key
	if s0.star && s0.key != "" {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, notFoundErr(token)
		}
		raw, ok := m[s0.key]
		if !ok {
			return nil, notFoundErr(token)
		}
		arr, ok := raw.([]interface{})
		if !ok {
			return nil, notFoundErr(token)
		}
		var parts []string
		for _, el := range arr {
			v, err := navigateFrom(el, rest, token)
			if err != nil {
				return nil, err
			}
			parts = append(parts, valueToStringForEmbedding(v))
		}
		return strings.Join(parts, "\n"), nil
	}

	// key[index] e.g. messages[0]
	if s0.key != "" && s0.index != nil && !s0.star {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, notFoundErr(token)
		}
		child, ok := m[s0.key]
		if !ok {
			return nil, notFoundErr(token)
		}
		arr, ok := child.([]interface{})
		if !ok {
			return nil, notFoundErr(token)
		}
		idx, ok := normalizeSliceIndex(*s0.index, len(arr))
		if !ok {
			return nil, notFoundErr(token)
		}
		return navigateFrom(arr[idx], rest, token)
	}

	// Plain key or numeric segment on array
	if s0.key != "" && s0.index == nil && !s0.star {
		switch m := cur.(type) {
		case map[string]interface{}:
			child, ok := m[s0.key]
			if !ok {
				return nil, notFoundErr(token)
			}
			return navigateFrom(child, rest, token)
		case []interface{}:
			idx, err := strconv.Atoi(s0.key)
			if err != nil {
				return nil, notFoundErr(token)
			}
			nidx, ok := normalizeSliceIndex(idx, len(m))
			if !ok {
				return nil, notFoundErr(token)
			}
			return navigateFrom(m[nidx], rest, token)
		default:
			return nil, notFoundErr(token)
		}
	}

	return nil, notFoundErr(token)
}

func notFoundErr(token string) error {
	return fmt.Errorf("template: key not found: {{%s}}: %w", token, ErrNotFound)
}

func navigatePath(root interface{}, path string, token string) (interface{}, error) {
	segs, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return root, nil
	}
	return navigateFrom(root, segs, token)
}

func parseTokenModifiers(s string) (expr string, defaultVal *string, err error) {
	s = strings.TrimSpace(s)
	pipe := strings.Index(s, "|")
	if pipe < 0 {
		return s, nil, nil
	}
	expr = strings.TrimSpace(s[:pipe])
	mod := strings.TrimSpace(s[pipe+1:])
	if !strings.HasPrefix(mod, "default:") {
		return "", nil, fmt.Errorf("template: unknown modifier in %q (only default: is supported)", s)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(mod, "default:"))
	if rest == "" {
		return "", nil, fmt.Errorf("template: default: requires a quoted value")
	}
	uq, uerr := strconv.Unquote(rest)
	if uerr != nil {
		return "", nil, fmt.Errorf("template: default value must be a double-quoted string: %w", uerr)
	}
	return expr, &uq, nil
}

// evalExpression evaluates env.KEY, vars.KEY, or nodeId.path (no outer braces).
func evalExpression(expr string, ctx *Context, fullToken string) (interface{}, error) {
	expr = strings.TrimSpace(expr)
	parts := strings.SplitN(expr, ".", 2)
	ns := parts[0]

	switch ns {
	case "env":
		if len(parts) < 2 {
			return nil, notFoundErr(fullToken)
		}
		key := parts[1]
		v, ok := ctx.Env[key]
		if !ok {
			return nil, notFoundErr(fullToken)
		}
		return v, nil

	case "vars":
		if len(parts) < 2 {
			return nil, notFoundErr(fullToken)
		}
		key := parts[1]
		v, ok := ctx.Vars[key]
		if !ok {
			return nil, notFoundErr(fullToken)
		}
		return v, nil

	default:
		nodeID := ns
		root, ok := ctx.NodeOutputs[nodeID]
		if !ok {
			return nil, notFoundErr(fullToken)
		}
		if len(parts) < 2 {
			return root, nil
		}
		return navigatePath(root, parts[1], fullToken)
	}
}

func valueToStringForEmbedding(v interface{}) string {
	switch t := v.(type) {
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	default:
		return scalarToString(v)
	}
}

func scalarToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func resolveInnerToString(inner string, ctx *Context) (string, error) {
	full := strings.TrimSpace(inner)
	expr, def, err := parseTokenModifiers(full)
	if err != nil {
		return "", err
	}
	v, err := evalExpression(expr, ctx, full)
	if err != nil {
		if def != nil && errors.Is(err, ErrNotFound) {
			return *def, nil
		}
		return "", err
	}
	return valueToStringForEmbedding(v), nil
}

// Resolve replaces all {{...}} tokens in input with values from ctx.
func Resolve(input string, ctx *Context) (string, error) {
	var resolveErr error
	result := tokenRe.ReplaceAllStringFunc(input, func(match string) string {
		if resolveErr != nil {
			return match
		}
		inner := match[2 : len(match)-2]
		s, err := resolveInnerToString(inner, ctx)
		if err != nil {
			resolveErr = err
			return match
		}
		return s
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return result, nil
}

func isFullTemplateString(s string) bool {
	s = strings.TrimSpace(s)
	loc := tokenRe.FindStringSubmatchIndex(s)
	if loc == nil {
		return false
	}
	return loc[0] == 0 && loc[1] == len(s)
}

func fullTemplateInner(s string) string {
	sub := tokenRe.FindStringSubmatch(strings.TrimSpace(s))
	if len(sub) < 2 {
		return ""
	}
	return sub[1]
}

// ResolveJSON walks every string value in the JSON tree and resolves templates.
// String fields always remain JSON strings after resolution; if a template resolves
// to an object or array, it is JSON-encoded into that string. Non-string leaves
// (numbers, booleans, nested objects/arrays) are walked recursively; templates only
// appear in string leaves.
func ResolveJSON(raw json.RawMessage, ctx *Context) (json.RawMessage, error) {
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

func resolveValue(v interface{}, ctx *Context) (interface{}, error) {
	switch val := v.(type) {
	case string:
		if isFullTemplateString(val) {
			inner := strings.TrimSpace(fullTemplateInner(val))
			expr, def, err := parseTokenModifiers(inner)
			if err != nil {
				return nil, err
			}
			out, err := evalExpression(expr, ctx, inner)
			if err != nil {
				if def != nil && errors.Is(err, ErrNotFound) {
					return *def, nil
				}
				return nil, err
			}
			return valueToStringForEmbedding(out), nil
		}
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
