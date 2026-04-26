package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	"orbyt-flow/internal/template"
)

const scriptTimeout = 5 * time.Second

type scriptConfig struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// ScriptRunner executes JavaScript (goja) with top-level return. Prior node outputs
// are exposed as nodes.<id>.output (same shape templates use for nested access).
// The runner output JSON is { "output": <returned object> } so e.g.
// {{node_id.output.field}} resolves in the template engine.
type ScriptRunner struct{}

func (r *ScriptRunner) Run(ctx context.Context, input Input) (*Output, error) {
	resolved, err := template.ResolveJSON(input.Config, input.Context)
	if err != nil {
		return nil, fmt.Errorf("script: resolve config: %w", err)
	}

	var cfg scriptConfig
	if err := json.Unmarshal(resolved, &cfg); err != nil {
		return nil, fmt.Errorf("script: unmarshal config: %w", err)
	}

	if strings.TrimSpace(cfg.Code) == "" {
		return nil, fmt.Errorf("script: missing or empty 'code' field")
	}

	lang := strings.TrimSpace(strings.ToLower(cfg.Language))
	if lang == "" {
		lang = "javascript"
	}
	if lang != "javascript" {
		return nil, fmt.Errorf("script: unsupported language %q (only \"javascript\" is supported)", cfg.Language)
	}

	execCtx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()

	vm := goja.New()

	nodesMap := make(map[string]interface{})
	if input.Context != nil && input.Context.NodeOutputs != nil {
		for id, out := range input.Context.NodeOutputs {
			nodesMap[id] = map[string]interface{}{
				"output": out,
			}
		}
	}

	if err := vm.Set("nodes", nodesMap); err != nil {
		return nil, fmt.Errorf("script: inject nodes: %w", err)
	}

	wrapped := "(function() {\n" + cfg.Code + "\n})()"

	runDone := make(chan struct{})
	go func() {
		select {
		case <-execCtx.Done():
			vm.Interrupt("script: execution timed out after 5s")
		case <-runDone:
		}
	}()
	defer close(runDone)

	val, err := vm.RunString(wrapped)
	if err != nil {
		if execCtx.Err() != nil {
			return nil, fmt.Errorf("script: %w", execCtx.Err())
		}
		return nil, fmt.Errorf("script: execution error: %w", err)
	}

	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil, fmt.Errorf("script: script must return a plain object (e.g. return { key: value })")
	}

	exported := val.Export()
	resultMap, ok := exported.(map[string]interface{})
	if !ok || resultMap == nil {
		return nil, fmt.Errorf("script: script must return a plain object (e.g. return { key: value })")
	}

	return marshalOutput(map[string]any{
		"output": resultMap,
	})
}
