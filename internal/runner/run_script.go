package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dop251/goja"

	"orbyt-flow/internal/template"
)

type runScriptConfig struct {
	Script         string `json:"script"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// RunScriptRunner executes JavaScript in a sandboxed goja runtime. The script must
// assign a global `result` value (typically an object); that value is JSON-marshaled
// as the node output. Globals `env`, `vars`, and `nodes` mirror the template context.
type RunScriptRunner struct{}

func (r *RunScriptRunner) Run(ctx context.Context, input Input) (*Output, error) {
	resolved, err := template.ResolveJSON(input.Config, input.Context)
	if err != nil {
		return nil, fmt.Errorf("run_script: resolve config: %w", err)
	}

	var cfg runScriptConfig
	if err := json.Unmarshal(resolved, &cfg); err != nil {
		return nil, fmt.Errorf("run_script: unmarshal config: %w", err)
	}

	timeout := 10 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	vm := goja.New()

	env := map[string]string{}
	if input.Context != nil && input.Context.Env != nil {
		env = input.Context.Env
	}
	vars := map[string]interface{}{}
	if input.Context != nil && input.Context.Vars != nil {
		vars = input.Context.Vars
	}
	nodes := map[string]interface{}{}
	if input.Context != nil && input.Context.NodeOutputs != nil {
		nodes = input.Context.NodeOutputs
	}

	if err := vm.Set("env", env); err != nil {
		return nil, fmt.Errorf("run_script: inject env: %w", err)
	}
	if err := vm.Set("vars", vars); err != nil {
		return nil, fmt.Errorf("run_script: inject vars: %w", err)
	}
	if err := vm.Set("nodes", nodes); err != nil {
		return nil, fmt.Errorf("run_script: inject nodes: %w", err)
	}

	runDone := make(chan struct{})
	go func() {
		select {
		case <-execCtx.Done():
			vm.Interrupt("run_script: execution canceled or timed out")
		case <-runDone:
		}
	}()
	defer close(runDone)

	if _, err := vm.RunString(cfg.Script); err != nil {
		if execCtx.Err() != nil {
			return nil, fmt.Errorf("run_script: %w", execCtx.Err())
		}
		return nil, fmt.Errorf("run_script: script error: %w", err)
	}

	resultVal := vm.Get("result")
	if resultVal == nil || goja.IsUndefined(resultVal) || goja.IsNull(resultVal) {
		return nil, fmt.Errorf("run_script: script did not set global variable 'result'")
	}

	exported := resultVal.Export()
	data, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("run_script: marshal result: %w", err)
	}

	return &Output{Data: data}, nil
}
