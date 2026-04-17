package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"orbyt-flow/internal/template"
)

type httpRequestConfig struct {
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	Auth           httpAuth          `json:"auth"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

type httpAuth struct {
	Type     string `json:"type"`     // "bearer" | "basic" | ""
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type HTTPRequestRunner struct{}

func (r *HTTPRequestRunner) Run(ctx context.Context, input Input) (*Output, error) {
	// Resolve templates in the raw config first.
	resolved, err := template.ResolveJSON(input.Config, input.Context)
	if err != nil {
		return nil, fmt.Errorf("http_request: resolve config: %w", err)
	}

	var cfg httpRequestConfig
	if err := json.Unmarshal(resolved, &cfg); err != nil {
		return nil, fmt.Errorf("http_request: unmarshal config: %w", err)
	}

	timeout := 30 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = strings.NewReader(cfg.Body)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, cfg.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http_request: create request: %w", err)
	}

	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	switch cfg.Auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+cfg.Auth.Token)
	case "basic":
		req.SetBasicAuth(cfg.Auth.Username, cfg.Auth.Password)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http_request: execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http_request: read response body: %w", err)
	}

	respHeaders := make(map[string]string, len(resp.Header))
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			respHeaders[k] = vs[0]
		}
	}

	return marshalOutput(map[string]any{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
		"headers":     respHeaders,
	})
}
