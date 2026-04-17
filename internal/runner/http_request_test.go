package runner_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"orbyt-flow/internal/runner"
	"orbyt-flow/internal/template"
)

func emptyCtx() *template.Context {
	return &template.Context{
		NodeOutputs: map[string]interface{}{},
		Env:         map[string]string{},
		Vars:        map[string]interface{}{},
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func runHTTP(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	r := &runner.HTTPRequestRunner{}
	out, err := r.Run(context.Background(), runner.Input{
		Config:  mustMarshal(t, cfg),
		Context: emptyCtx(),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Data, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return result
}

func TestHTTPRunnerGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("X-Test", "hello")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	result := runHTTP(t, map[string]any{"method": "GET", "url": srv.URL})

	if int(result["status_code"].(float64)) != 200 {
		t.Errorf("status_code: got %v, want 200", result["status_code"])
	}
	if result["body"] != `{"ok":true}` {
		t.Errorf("body: got %v", result["body"])
	}
	headers := result["headers"].(map[string]any)
	if headers["X-Test"] != "hello" {
		t.Errorf("X-Test header: got %v, want hello", headers["X-Test"])
	}
}

func TestHTTPRunnerPOST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"key":"value"}` {
			t.Errorf("unexpected body: %s", body)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing Content-Type header")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"created":true}`)
	}))
	defer srv.Close()

	result := runHTTP(t, map[string]any{
		"method":  "POST",
		"url":     srv.URL,
		"body":    `{"key":"value"}`,
		"headers": map[string]string{"Content-Type": "application/json"},
	})

	if int(result["status_code"].(float64)) != 201 {
		t.Errorf("status_code: got %v, want 201", result["status_code"])
	}
	if result["body"] != `{"created":true}` {
		t.Errorf("body: got %v", result["body"])
	}
}

func TestHTTPRunnerTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the configured timeout.
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &runner.HTTPRequestRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config:  mustMarshal(t, map[string]any{"method": "GET", "url": srv.URL, "timeout_seconds": 0}),
		Context: emptyCtx(),
	})
	// timeout_seconds=0 falls back to the default 30s, so we drive timeout via
	// a cancelled context instead.
	_ = err

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = r.Run(ctx, runner.Input{
		Config:  mustMarshal(t, map[string]any{"method": "GET", "url": srv.URL, "timeout_seconds": 30}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHTTPRunnerNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not found")
	}))
	defer srv.Close()

	// Non-2xx must NOT return an error — status_code reflects it.
	result := runHTTP(t, map[string]any{"method": "GET", "url": srv.URL})

	if int(result["status_code"].(float64)) != 404 {
		t.Errorf("status_code: got %v, want 404", result["status_code"])
	}
	if result["body"] != "not found" {
		t.Errorf("body: got %v, want \"not found\"", result["body"])
	}
}
