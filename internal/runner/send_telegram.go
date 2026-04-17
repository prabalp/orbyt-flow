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

type sendTelegramConfig struct {
	ChatID   string `json:"chat_id"`
	Message  string `json:"message"`
	BotToken string `json:"bot_token"`
}

type SendTelegramRunner struct{}

func (r *SendTelegramRunner) Run(ctx context.Context, input Input) (*Output, error) {
	resolved, err := template.ResolveJSON(input.Config, input.Context)
	if err != nil {
		return nil, fmt.Errorf("send_telegram: resolve config: %w", err)
	}

	var cfg sendTelegramConfig
	if err := json.Unmarshal(resolved, &cfg); err != nil {
		return nil, fmt.Errorf("send_telegram: unmarshal config: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"chat_id": cfg.ChatID,
		"text":    cfg.Message,
	})
	if err != nil {
		return nil, fmt.Errorf("send_telegram: marshal payload: %w", err)
	}

	url := "https://api.telegram.org/bot" + cfg.BotToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("send_telegram: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send_telegram: execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("send_telegram: read response: %w", err)
	}

	var apiResp struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || !apiResp.OK {
		return nil, fmt.Errorf("send_telegram: API error: %s", string(body))
	}

	return marshalOutput(map[string]any{
		"sent":       true,
		"message_id": apiResp.Result.MessageID,
	})
}
