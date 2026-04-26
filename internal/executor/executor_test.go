package executor_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"orbyt-flow/internal/executor"
	"orbyt-flow/internal/runner"
	"orbyt-flow/internal/services"
	"orbyt-flow/internal/store"
	"orbyt-flow/internal/template"
	"orbyt-flow/internal/types"
)

// --- mock runner ---

type mockRunner struct {
	output json.RawMessage
	err    error
	calls  int
	// sequence: if non-nil, use outputs[calls] until exhausted, then fallback to output/err
	sequence []mockStep
}

type mockStep struct {
	output json.RawMessage
	err    error
}

func (m *mockRunner) Run(_ context.Context, _ runner.Input) (*runner.Output, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.sequence) {
		s := m.sequence[idx]
		if s.err != nil {
			return nil, s.err
		}
		return &runner.Output{Data: s.output}, nil
	}
	if m.err != nil {
		return nil, m.err
	}
	return &runner.Output{Data: m.output}, nil
}

// --- helpers ---

func makeWorkflow(nodes []types.Node, conns []types.Connection) *types.Workflow {
	return &types.Workflow{
		ID:           "wf-1",
		UserID:       "user-1",
		Name:         "test",
		Nodes:        nodes,
		Connections:  conns,
		ErrorHandler: types.ErrorHandler{Notify: "none", Retry: 0},
	}
}

func node(id, typ string) types.Node {
	return types.Node{ID: id, Type: typ, Config: json.RawMessage(`{}`)}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func newExec(t *testing.T, runners map[string]runner.Runner) *executor.Executor {
	t.Helper()
	dir := t.TempDir()
	s := store.NewFileStore(dir)
	services.SetDataDir(dir)
	ex := executor.NewExecutor(s)
	ex.DataDir = dir
	ex.Runners = runners
	return ex
}

func successRunner(output json.RawMessage) *mockRunner {
	return &mockRunner{output: output}
}

// --- tests ---

func TestLinearWorkflow(t *testing.T) {
	runners := map[string]runner.Runner{
		"task": successRunner(mustJSON(map[string]any{"ok": true})),
	}
	w := makeWorkflow(
		[]types.Node{node("n1", "task"), node("n2", "task"), node("n3", "task")},
		[]types.Connection{{From: "n1", To: "n2"}, {From: "n2", To: "n3"}},
	)

	ex := newExec(t, runners)
	run, err := ex.Execute(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if run.Status != "success" {
		t.Errorf("status: got %q, want success", run.Status)
	}
	if len(run.Steps) != 3 {
		t.Fatalf("steps: got %d, want 3", len(run.Steps))
	}
	for i, s := range run.Steps {
		if s.Status != "success" {
			t.Errorf("step[%d] %s status: got %q, want success", i, s.NodeID, s.Status)
		}
	}
}

func TestWorkflowWithIf(t *testing.T) {
	// n1 → if_node → n2 (true branch) or n3 (false branch)
	// if runner returns next="n2" (true branch chosen)
	ifOutput := mustJSON(map[string]any{"result": true, "next": "n2"})
	runners := map[string]runner.Runner{
		"task":       successRunner(mustJSON(map[string]any{"ok": true})),
		types.NodeIf: successRunner(ifOutput),
	}
	w := makeWorkflow(
		[]types.Node{
			node("n1", "task"),
			{ID: "if_node", Type: types.NodeIf, Config: mustJSON(map[string]any{
				"condition":  "1 == 1",
				"true_next":  "n2",
				"false_next": "n3",
			})},
			node("n2", "task"),
			node("n3", "task"),
		},
		[]types.Connection{
			{From: "n1", To: "if_node"},
			{From: "if_node", To: "n2"},
			{From: "if_node", To: "n3"},
		},
	)

	ex := newExec(t, runners)
	run, err := ex.Execute(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if run.Status != "success" {
		t.Errorf("status: got %q, want success", run.Status)
	}

	stepStatus := make(map[string]string)
	for _, s := range run.Steps {
		stepStatus[s.NodeID] = s.Status
	}
	if stepStatus["n2"] != "success" {
		t.Errorf("n2 (true branch): got %q, want success", stepStatus["n2"])
	}
	if stepStatus["n3"] != "skipped" {
		t.Errorf("n3 (false branch): got %q, want skipped", stepStatus["n3"])
	}
}

func TestWorkflowNodeFailure(t *testing.T) {
	runners := map[string]runner.Runner{
		"task": &mockRunner{
			sequence: []mockStep{
				{output: mustJSON(map[string]any{"ok": true})}, // n1 succeeds
				{err: errors.New("something broke")},           // n2 fails
			},
		},
	}
	w := makeWorkflow(
		[]types.Node{node("n1", "task"), node("n2", "task"), node("n3", "task")},
		[]types.Connection{{From: "n1", To: "n2"}, {From: "n2", To: "n3"}},
	)

	ex := newExec(t, runners)
	run, err := ex.Execute(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("status: got %q, want failed", run.Status)
	}
	if run.Error == "" {
		t.Error("expected non-empty run.Error")
	}

	// n3 should never have been reached.
	for _, s := range run.Steps {
		if s.NodeID == "n3" {
			t.Errorf("n3 should not have a step record, got status=%s", s.Status)
		}
	}
}

func TestWorkflowRetry(t *testing.T) {
	runners := map[string]runner.Runner{
		"task": &mockRunner{
			sequence: []mockStep{
				{output: mustJSON(map[string]any{"ok": true})}, // n1 succeeds
				{err: errors.New("transient")},                 // n2 attempt 1 fails
				{err: errors.New("transient")},                 // n2 attempt 2 fails
				{output: mustJSON(map[string]any{"ok": true})}, // n2 attempt 3 succeeds
				{output: mustJSON(map[string]any{"ok": true})}, // n3 succeeds
			},
		},
	}
	w := &types.Workflow{
		ID:           "wf-retry",
		UserID:       "user-1",
		Name:         "retry test",
		Nodes:        []types.Node{node("n1", "task"), node("n2", "task"), node("n3", "task")},
		Connections:  []types.Connection{{From: "n1", To: "n2"}, {From: "n2", To: "n3"}},
		ErrorHandler: types.ErrorHandler{Retry: 2}, // up to 2 retries = 3 attempts
	}

	ex := newExec(t, runners)

	// Replace the wait backoff in tests by swapping to a fast context — the
	// actual 2s sleep is in executor; we accept it here since retry count is
	// small, but run with a generous timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run, err := ex.Execute(ctx, w, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if run.Status != "success" {
		t.Errorf("status: got %q, want success (error=%s)", run.Status, run.Error)
	}
	if len(run.Steps) != 3 {
		t.Errorf("steps: got %d, want 3", len(run.Steps))
	}
}

func TestWorkflowStopNode(t *testing.T) {
	runners := map[string]runner.Runner{
		"task": successRunner(mustJSON(map[string]any{"ok": true})),
		"stop": successRunner(mustJSON(map[string]any{"__stop": true})),
	}
	w := makeWorkflow(
		[]types.Node{node("n1", "task"), node("n2", "stop"), node("n3", "task")},
		[]types.Connection{{From: "n1", To: "n2"}, {From: "n2", To: "n3"}},
	)

	ex := newExec(t, runners)
	run, err := ex.Execute(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if run.Status != "success" {
		t.Errorf("status: got %q, want success", run.Status)
	}

	stepStatus := make(map[string]string)
	for _, s := range run.Steps {
		stepStatus[s.NodeID] = s.Status
	}
	if stepStatus["n2"] != "success" {
		t.Errorf("stop node n2: got %q, want success", stepStatus["n2"])
	}
	// n3 must not appear — loop broke before reaching it.
	if _, exists := stepStatus["n3"]; exists {
		t.Errorf("n3 should not have executed after stop, got status=%s", stepStatus["n3"])
	}
}

func TestCycleDetection(t *testing.T) {
	runners := map[string]runner.Runner{
		"task": successRunner(mustJSON(map[string]any{"ok": true})),
	}
	// n1 → n2 → n3 → n1 (cycle)
	w := makeWorkflow(
		[]types.Node{node("n1", "task"), node("n2", "task"), node("n3", "task")},
		[]types.Connection{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
			{From: "n3", To: "n1"},
		},
	)

	ex := newExec(t, runners)
	run, err := ex.Execute(context.Background(), w, nil)
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("status: got %q, want failed", run.Status)
	}
	if run.Error != "cycle detected in workflow" {
		t.Errorf("error msg: got %q, want %q", run.Error, "cycle detected in workflow")
	}

	// Verify persisted.
	fetched, err := store.NewFileStore(t.TempDir()).GetRun("user-1", run.ID)
	_ = fetched
	_ = err
	// (The store used internally is not exposed, so we rely on the returned run.)
}

// Ensure the mock runner satisfies the runner.Runner interface at compile time.
var _ runner.Runner = (*mockRunner)(nil)

// Ensure the mock uses template.Context indirectly (import kept alive).
var _ = (*template.Context)(nil)
