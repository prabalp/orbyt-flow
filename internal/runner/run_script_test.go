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

func TestRunScriptBasic(t *testing.T) {
	r := &runner.RunScriptRunner{}
	out, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"script": `var result = { score: 42, label: "high" };`,
		}),
		Context: emptyCtx(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Data, &got); err != nil {
		t.Fatal(err)
	}
	if int(got["score"].(float64)) != 42 {
		t.Errorf("score: got %v", got["score"])
	}
	if got["label"] != "high" {
		t.Errorf("label: got %v", got["label"])
	}
}

func TestRunScriptContextGlobals(t *testing.T) {
	r := &runner.RunScriptRunner{}
	ctx := &template.Context{
		NodeOutputs: map[string]interface{}{
			"fetch_data": map[string]interface{}{
				"body": map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{"name": "alpha"},
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "secret"},
		Vars: map[string]interface{}{"offset": float64(3)},
	}
	out, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"script": `
				var body = nodes.fetch_data.body;
				var result = {
					count: body.items.length,
					first: body.items[0].name,
					token: env.TOKEN,
					shifted: vars.offset + 1
				};
			`,
		}),
		Context: ctx,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Data, &got); err != nil {
		t.Fatal(err)
	}
	if int(got["count"].(float64)) != 1 {
		t.Errorf("count: got %v", got["count"])
	}
	if got["first"] != "alpha" {
		t.Errorf("first: got %v", got["first"])
	}
	if got["token"] != "secret" {
		t.Errorf("token: got %v", got["token"])
	}
	if int(got["shifted"].(float64)) != 4 {
		t.Errorf("shifted: got %v", got["shifted"])
	}
}

func TestRunScriptThrows(t *testing.T) {
	r := &runner.RunScriptRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"script": `throw new Error("boom");`,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "run_script:") {
		t.Errorf("error should be wrapped: %v", err)
	}
}

func TestRunScriptTimeout(t *testing.T) {
	// Parent context expires before the script can finish; run_script must cancel via context.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := &runner.RunScriptRunner{}
	_, err := r.Run(ctx, runner.Input{
		Config: mustMarshal(t, map[string]any{
			"script":          "while(true){}",
			"timeout_seconds": 60,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "run_script:") {
		t.Errorf("error should be wrapped: %v", err)
	}
}

func TestRunScriptMissingResult(t *testing.T) {
	r := &runner.RunScriptRunner{}
	_, err := r.Run(context.Background(), runner.Input{
		Config: mustMarshal(t, map[string]any{
			"script": `var x = 1;`,
		}),
		Context: emptyCtx(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "result") {
		t.Errorf("expected message about result: %v", err)
	}
}
