package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type waitConfig struct {
	Seconds int `json:"seconds"`
}

type WaitRunner struct{}

func (r *WaitRunner) Run(ctx context.Context, input Input) (*Output, error) {
	var cfg waitConfig
	if err := json.Unmarshal(input.Config, &cfg); err != nil {
		return nil, fmt.Errorf("wait: unmarshal config: %w", err)
	}

	select {
	case <-time.After(time.Duration(cfg.Seconds) * time.Second):
	case <-ctx.Done():
		return nil, fmt.Errorf("wait: %w", ctx.Err())
	}

	return marshalOutput(map[string]any{"waited_seconds": cfg.Seconds})
}
