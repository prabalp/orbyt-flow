package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"orbyt-flow/internal/runner"
	"orbyt-flow/internal/template"
)

func TestScriptBasicReturnAndOutputShape(t *testing.T) {
	r := &runner.ScriptRunner{}
	out, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "javascript",
			"code":     `return { message_texts: "hello", n: 2 };`,
		}),
		Context: emptyCtx(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Data, &top); err != nil {
		t.Fatal(err)
	}
	inner, ok := top["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected output object, got %T", top["output"])
	}
	if inner["message_texts"] != "hello" {
		t.Errorf("message_texts: %v", inner["message_texts"])
	}
	if int(inner["n"].(float64)) != 2 {
		t.Errorf("n: %v", inner["n"])
	}
}

func TestScriptNodesWrappedLikeTemplates(t *testing.T) {
	r := &runner.ScriptRunner{}
	ctx := &template.Context{
		NodeOutputs: map[string]interface{}{
			"fetch_messages": map[string]interface{}{
				"status_code": float64(200),
				"body": map[string]interface{}{
					"messages": []interface{}{
						map[string]interface{}{"text": "a", "subtype": nil},
						map[string]interface{}{"text": "b", "subtype": nil},
					},
				},
				"headers": map[string]interface{}{"X-Test": "ok"},
			},
		},
		Env:  map[string]string{},
		Vars: map[string]interface{}{},
	}
	out, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "javascript",
			"code": `
				const body = nodes.fetch_messages.output.body;
				const messages = body.messages || [];
				const texts = messages.map(m => m.text).join(',');
				return { message_texts: texts };
			`,
		}),
		Context: ctx,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Data, &top); err != nil {
		t.Fatal(err)
	}
	inner := top["output"].(map[string]any)
	if inner["message_texts"] != "a,b" {
		t.Errorf("got %v", inner["message_texts"])
	}
}

func TestScriptThrows(t *testing.T) {
	r := &runner.ScriptRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "javascript",
			"code":     `throw new Error("boom");`,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "script:") {
		t.Errorf("wrap: %v", err)
	}
}

func TestScriptTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := &runner.ScriptRunner{}
	_, err := r.Run(ctx, runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "javascript",
			"code":     `while(true){}`,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "script:") {
		t.Errorf("wrap: %v", err)
	}
}

func TestScriptNonObjectReturn(t *testing.T) {
	r := &runner.ScriptRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "javascript",
			"code":     `return 42;`,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "plain object") {
		t.Errorf("got %v", err)
	}
}

func TestScriptMissingCode(t *testing.T) {
	r := &runner.ScriptRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "javascript",
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "code") {
		t.Errorf("got %v", err)
	}
}

func TestScriptUnsupportedLanguage(t *testing.T) {
	r := &runner.ScriptRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"language": "python",
			"code":     `return {};`,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("got %v", err)
	}
}

func TestScriptDefaultLanguageJavascript(t *testing.T) {
	r := &runner.ScriptRunner{}
	out, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"code": `return { ok: true };`,
		}),
		Context: emptyCtx(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(out.Data, &top); err != nil {
		t.Fatal(err)
	}
	inner := top["output"].(map[string]any)
	if inner["ok"] != true {
		t.Errorf("got %v", inner["ok"])
	}
}
