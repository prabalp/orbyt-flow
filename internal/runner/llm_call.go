package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"orbyt-flow/internal/template"
)

type llmCallConfig struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	System    string `json:"system"`
	MaxTokens int    `json:"max_tokens"`
	APIKey    string `json:"api_key"`
}

type LLMCallRunner struct{}

func (r *LLMCallRunner) Run(ctx context.Context, input Input) (*Output, error) {
	var cfg llmCallConfig
	if err := json.Unmarshal(input.Config, &cfg); err != nil {
		return nil, fmt.Errorf("llm_call: unmarshal config: %w", err)
	}

	prompt, err := template.Resolve(cfg.Prompt, input.Context)
	if err != nil {
		return nil, fmt.Errorf("llm_call: resolve prompt: %w", err)
	}
	system, err := template.Resolve(cfg.System, input.Context)
	if err != nil {
		return nil, fmt.Errorf("llm_call: resolve system: %w", err)
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1000
	}

	reqBody := map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if system != "" {
		reqBody["system"] = system
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("llm_call: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("llm_call: create request: %w", err)
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm_call: execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm_call: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm_call: API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("llm_call: parse response: %w", err)
	}

	text := ""
	if len(apiResp.Content) > 0 {
		text = apiResp.Content[0].Text
	}

	return marshalOutput(map[string]any{
		"text":          text,
		"input_tokens":  apiResp.Usage.InputTokens,
		"output_tokens": apiResp.Usage.OutputTokens,
	})
}
