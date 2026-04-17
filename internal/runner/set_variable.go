package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"orbyt-flow/internal/template"
)

type setVariableConfig struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type SetVariableRunner struct{}

func (r *SetVariableRunner) Run(_ context.Context, input Input) (*Output, error) {
	var cfg setVariableConfig
	if err := json.Unmarshal(input.Config, &cfg); err != nil {
		return nil, fmt.Errorf("set_variable: unmarshal config: %w", err)
	}

	// Resolve template tokens if value is a string.
	value := cfg.Value
	if s, ok := cfg.Value.(string); ok {
		resolved, err := template.Resolve(s, input.Context)
		if err != nil {
			return nil, fmt.Errorf("set_variable: resolve value: %w", err)
		}
		value = resolved
	}

	if input.Context.Vars == nil {
		input.Context.Vars = make(map[string]interface{})
	}
	input.Context.Vars[cfg.Key] = value

	return marshalOutput(map[string]any{
		"key":   cfg.Key,
		"value": value,
	})
}
