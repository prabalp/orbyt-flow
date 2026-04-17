package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"orbyt-flow/internal/template"
)

type ifConfig struct {
	Condition string `json:"condition"`
	TrueNext  string `json:"true_next"`
	FalseNext string `json:"false_next"`
}

type IfRunner struct{}

func (r *IfRunner) Run(_ context.Context, input Input) (*Output, error) {
	var cfg ifConfig
	if err := json.Unmarshal(input.Config, &cfg); err != nil {
		return nil, fmt.Errorf("if: unmarshal config: %w", err)
	}

	// Resolve templates in the condition string before parsing.
	resolved, err := template.Resolve(cfg.Condition, input.Context)
	if err != nil {
		return nil, fmt.Errorf("if: resolve condition: %w", err)
	}

	// TODO: replace with a full expression evaluator for arithmetic, boolean
	// logic, and function calls (e.g. contains, len).
	result, err := evalCondition(resolved)
	if err != nil {
		return nil, fmt.Errorf("if: evaluate condition: %w", err)
	}

	next := cfg.FalseNext
	if result {
		next = cfg.TrueNext
	}

	return marshalOutput(map[string]any{
		"result": result,
		"next":   next,
	})
}

// evalCondition parses and evaluates simple binary comparisons of the form
// "<lhs> <op> <rhs>" where both sides are treated as strings after template
// resolution.  Supported operators: ==, !=, >, <, >=, <=.
func evalCondition(expr string) (bool, error) {
	ops := []string{">=", "<=", "!=", "==", ">", "<"}
	for _, op := range ops {
		idx := strings.Index(expr, op)
		if idx == -1 {
			continue
		}
		lhs := strings.TrimSpace(expr[:idx])
		rhs := strings.TrimSpace(expr[idx+len(op):])
		switch op {
		case "==":
			return lhs == rhs, nil
		case "!=":
			return lhs != rhs, nil
		case ">":
			return lhs > rhs, nil
		case "<":
			return lhs < rhs, nil
		case ">=":
			return lhs >= rhs, nil
		case "<=":
			return lhs <= rhs, nil
		}
	}
	return false, fmt.Errorf("unsupported condition expression: %q", expr)
}
