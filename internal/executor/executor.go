package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"time"

	"orbyt-flow/internal/runner"
	"orbyt-flow/internal/services"
	"orbyt-flow/internal/store"
	"orbyt-flow/internal/template"
	"orbyt-flow/internal/types"
)

// Executor orchestrates workflow runs.
type Executor struct {
	Store   store.Store
	Runners map[string]runner.Runner
	// DataDir is the file store root (e.g. FLOWENGINE_DATA_DIR). When non-empty, Google OAuth tokens are refreshed before each run so env GOOGLE_ACCESS_TOKEN stays valid.
	DataDir string
}

// NewExecutor creates an Executor wired to the default runner registry.
func NewExecutor(s store.Store) *Executor {
	return &Executor{
		Store:   s,
		Runners: runner.Registry(),
	}
}

// Execute runs a workflow and persists the resulting Run.
// An optional env map overrides the empty Env in the template context.
func (e *Executor) Execute(ctx context.Context, w *types.Workflow, triggerPayload interface{}, envOverride ...map[string]string) (*types.Run, error) {
	// 1. Generate run ID.
	runID, err := newID()
	if err != nil {
		return nil, fmt.Errorf("executor: generate run id: %w", err)
	}

	// 2. Create Run record.
	now := time.Now()
	run := &types.Run{
		ID:         runID,
		WorkflowID: w.ID,
		UserID:     w.UserID,
		Status:     "running",
		StartedAt:  now,
		Steps:      []types.StepResult{},
	}
	if err := e.Store.SaveRun(run); err != nil {
		return nil, fmt.Errorf("executor: save initial run: %w", err)
	}

	// 3 & 4. Build adjacency and in-degree maps.
	adj := make(map[string][]string) // from → []to
	inDeg := make(map[string]int)    // node → in-degree
	nodeMap := make(map[string]types.Node)

	for _, n := range w.Nodes {
		nodeMap[n.ID] = n
		if _, ok := adj[n.ID]; !ok {
			adj[n.ID] = nil
		}
		if _, ok := inDeg[n.ID]; !ok {
			inDeg[n.ID] = 0
		}
	}
	for _, c := range w.Connections {
		adj[c.From] = append(adj[c.From], c.To)
		inDeg[c.To]++
	}

	// 5. Kahn's topological sort.
	order, err := topoSort(w.Nodes, adj, inDeg)
	if err != nil {
		return e.failRun(run, "cycle detected in workflow")
	}

	// 6. Build template context.
	env := map[string]string{}
	if len(envOverride) > 0 && envOverride[0] != nil {
		env = envOverride[0]
	}
	if e.DataDir != "" {
		services.SetDataDir(e.DataDir)
		if _, err := services.LoadGoogleToken(w.UserID); err == nil {
			if err := services.EnsureFreshGoogleToken(w.UserID); err != nil {
				log.Printf("warn: google token refresh failed for user %s: %v", w.UserID, err)
			}
		}
		env = maps.Clone(env)
		tok := services.ReadUserEnvValue(w.UserID, services.GoogleUserSecretsAccessKey)
		if tok != "" {
			env[services.GoogleUserSecretsAccessKey] = tok
		} else {
			delete(env, services.GoogleUserSecretsAccessKey)
		}
	}
	tplCtx := &template.Context{
		Env:         env,
		Vars:        map[string]interface{}{},
		NodeOutputs: map[string]interface{}{},
	}
	if triggerPayload != nil {
		tplCtx.NodeOutputs["trigger"] = triggerPayload
	}

	// 7. Skip set for if-branching.
	skipSet := make(map[string]bool)

	// 8. Walk nodes in topological order.
	for _, nodeID := range order {
		node := nodeMap[nodeID]

		// 8a. Skipped node.
		if skipSet[nodeID] {
			run.Steps = append(run.Steps, types.StepResult{
				NodeID:   node.ID,
				NodeType: node.Type,
				Status:   "skipped",
			})
			continue
		}

		// 8b. Resolve config.
		resolvedConfig, err := template.ResolveJSON(node.Config, tplCtx)
		if err != nil {
			return e.failRun(run, fmt.Sprintf("node %s: resolve config: %v", node.ID, err))
		}

		// 8c. Look up runner.
		r, ok := e.Runners[node.Type]
		if !ok {
			step := types.StepResult{
				NodeID:   node.ID,
				NodeType: node.Type,
				Status:   "failed",
				Error:    "unknown node type",
			}
			run.Steps = append(run.Steps, step)
			return e.failRun(run, fmt.Sprintf("unknown node type: %s", node.Type))
		}

		// 8d–8g. Run with retry.
		maxAttempts := 1 + w.ErrorHandler.Retry
		var (
			output  *runner.Output
			runErr  error
			stepDur time.Duration
		)
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.Done():
					return e.failRun(run, ctx.Err().Error())
				}
			}
			nodeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			start := time.Now()
			output, runErr = r.Run(nodeCtx, runner.Input{
				Config:  resolvedConfig,
				Context: tplCtx,
			})
			stepDur = time.Since(start)
			cancel()
			if runErr == nil {
				break
			}
		}

		// 8e. Record step.
		step := types.StepResult{
			NodeID:     node.ID,
			NodeType:   node.Type,
			Input:      resolvedConfig,
			DurationMs: stepDur.Milliseconds(),
		}
		if runErr != nil {
			step.Status = "failed"
			step.Error = runErr.Error()
			run.Steps = append(run.Steps, step)
			return e.failRun(run, fmt.Sprintf("node %s failed: %v", node.ID, runErr))
		}
		step.Status = "success"
		if output != nil {
			step.Output = output.Data
		}
		run.Steps = append(run.Steps, step)

		// 8f. Store output in template context.
		if output != nil && len(output.Data) > 0 {
			var decoded interface{}
			if err := json.Unmarshal(output.Data, &decoded); err == nil {
				tplCtx.NodeOutputs[node.ID] = decoded
			}
		}

		// 8h. Check for __stop signal.
		if output != nil && containsStop(output.Data) {
			break
		}

		// 8i. Handle if-node branching.
		if node.Type == types.NodeIf {
			if err := e.handleIfBranch(w, node.ID, output, skipSet, adj); err != nil {
				return e.failRun(run, fmt.Sprintf("node %s: if branch: %v", node.ID, err))
			}
		}
	}

	// 9. Mark success.
	finishedAt := time.Now()
	run.Status = "success"
	run.FinishedAt = &finishedAt
	if err := e.Store.SaveRun(run); err != nil {
		return nil, fmt.Errorf("executor: save run: %w", err)
	}
	return run, nil
}

// failRun marks the run as failed, saves it, and returns it (not an error).
func (e *Executor) failRun(run *types.Run, msg string) (*types.Run, error) {
	finishedAt := time.Now()
	run.Status = "failed"
	run.Error = msg
	run.FinishedAt = &finishedAt
	_ = e.Store.SaveRun(run)
	return run, nil
}

// handleIfBranch adds the non-chosen branch (and all nodes reachable only
// through it) to skipSet.
func (e *Executor) handleIfBranch(
	w *types.Workflow,
	ifNodeID string,
	output *runner.Output,
	skipSet map[string]bool,
	adj map[string][]string,
) error {
	if output == nil {
		return nil
	}
	var result struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal(output.Data, &result); err != nil {
		return err
	}
	chosenNext := result.Next

	// Mark every direct successor of the if-node except the chosen one.
	for _, successor := range adj[ifNodeID] {
		if successor != chosenNext {
			e.resolveSkip(w, successor, skipSet, adj)
		}
	}
	return nil
}

// resolveSkip adds nodeID and all nodes reachable only through it to skipSet.
func (e *Executor) resolveSkip(
	w *types.Workflow,
	nodeID string,
	skipSet map[string]bool,
	adj map[string][]string,
) {
	if skipSet[nodeID] {
		return
	}
	skipSet[nodeID] = true
	for _, child := range adj[nodeID] {
		// Only skip child if all its predecessors are already skipped.
		if allPredsSkipped(w, child, skipSet) {
			e.resolveSkip(w, child, skipSet, adj)
		}
	}
}

// allPredsSkipped returns true if every node that connects to target is skipped.
func allPredsSkipped(w *types.Workflow, target string, skipSet map[string]bool) bool {
	for _, c := range w.Connections {
		if c.To == target && !skipSet[c.From] {
			return false
		}
	}
	return true
}

// topoSort returns node IDs in topological order via Kahn's algorithm.
// Returns an error if a cycle is detected.
func topoSort(nodes []types.Node, adj map[string][]string, inDeg map[string]int) ([]string, error) {
	// Work on a copy of in-degrees.
	deg := make(map[string]int, len(inDeg))
	for k, v := range inDeg {
		deg[k] = v
	}

	queue := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if deg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	order := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, next := range adj[cur] {
			deg[next]--
			if deg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(order) != len(nodes) {
		return nil, fmt.Errorf("cycle detected")
	}
	return order, nil
}

// containsStop returns true when the JSON output contains "__stop": true.
func containsStop(data json.RawMessage) bool {
	if len(data) == 0 {
		return false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	v, ok := m["__stop"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// newID generates a random 16-byte hex string.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
