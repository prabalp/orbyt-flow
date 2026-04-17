package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type stopConfig struct {
	Message string `json:"message"`
	IsError bool   `json:"is_error"`
}

type StopRunner struct{}

func (r *StopRunner) Run(_ context.Context, input Input) (*Output, error) {
	var cfg stopConfig
	if err := json.Unmarshal(input.Config, &cfg); err != nil {
		return nil, fmt.Errorf("stop: unmarshal config: %w", err)
	}

	if cfg.IsError {
		msg := cfg.Message
		if msg == "" {
			msg = "workflow stopped with error"
		}
		return nil, errors.New(msg)
	}

	return marshalOutput(map[string]any{"__stop": true})
}
