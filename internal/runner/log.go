package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"orbyt-flow/internal/template"
)

type logConfig struct {
	Message string `json:"message"`
	Level   string `json:"level"` // "info" | "warn" | "error"
}

type LogRunner struct{}

func (r *LogRunner) Run(_ context.Context, input Input) (*Output, error) {
	var cfg logConfig
	if err := json.Unmarshal(input.Config, &cfg); err != nil {
		return nil, fmt.Errorf("log: unmarshal config: %w", err)
	}

	msg, err := template.Resolve(cfg.Message, input.Context)
	if err != nil {
		return nil, fmt.Errorf("log: resolve message: %w", err)
	}

	level := cfg.Level
	if level == "" {
		level = "info"
	}

	log.Printf("[%s] %s", level, msg)

	return marshalOutput(map[string]any{"logged": true})
}
