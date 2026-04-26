package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"orbyt-flow/internal/executor"
	"orbyt-flow/internal/store"
	"orbyt-flow/internal/types"
)

// MCPServer exposes orbyt-flow capabilities as MCP tools over stdio.
type MCPServer struct {
	Store    store.Store
	Executor *executor.Executor
	DataDir  string
	UserID   string // from env MCP_USER_ID
}

// NewMCPServer creates an MCPServer.
func NewMCPServer(s store.Store, ex *executor.Executor, dataDir, userID string) *MCPServer {
	return &MCPServer{
		Store:    s,
		Executor: ex,
		DataDir:  dataDir,
		UserID:   userID,
	}
}

// Start registers all tools and blocks on the stdio transport.
func (m *MCPServer) Start() error {
	srv := mcpserver.NewMCPServer("orbyt-flow", "0.1.0")

	srv.AddTool(m.toolCreateWorkflow(), m.handleCreateWorkflow)
	srv.AddTool(m.toolUpdateWorkflow(), m.handleUpdateWorkflow)
	srv.AddTool(m.toolTriggerWorkflow(), m.handleTriggerWorkflow)
	srv.AddTool(m.toolGetRunStatus(), m.handleGetRunStatus)
	srv.AddTool(m.toolListWorkflows(), m.handleListWorkflows)
	srv.AddTool(m.toolDeleteWorkflow(), m.handleDeleteWorkflow)

	srv.AddTool(m.toolListUserSecrets(), m.handleListUserSecrets)
	srv.AddTool(m.toolUpsertUserSecrets(), m.handleUpsertUserSecrets)
	srv.AddTool(m.toolDeleteUserSecret(), m.handleDeleteUserSecret)

	return mcpserver.ServeStdio(srv)
}

// ---- tool definitions ----

func (m *MCPServer) toolCreateWorkflow() mcplib.Tool {
	return mcplib.NewTool(
		"create_workflow",
		mcplib.WithDescription("Create a new deterministic workflow. Nodes are connected via the connections array."),
		mcplib.WithString("name",
			mcplib.Description("Workflow name"),
			mcplib.Required(),
		),
		mcplib.WithString("description",
			mcplib.Description("Optional human-readable description"),
		),
		mcplib.WithObject("trigger",
			mcplib.Description("Trigger configuration. Properties: type (required, enum: schedule/webhook/manual), cron (string), tz (string), path (string, for webhook)"),
		),
		mcplib.WithArray("nodes",
			mcplib.Description("Array of node objects. Each node: { id (required), type (required), config (object) }"),
		),
		mcplib.WithArray("connections",
			mcplib.Description("Array of connection objects: { from (required), to (required) }"),
		),
		mcplib.WithObject("error_handler",
			mcplib.Description("Error handling: { notify (enum: telegram/email/none), retry (integer) }"),
		),
	)
}

func (m *MCPServer) toolUpdateWorkflow() mcplib.Tool {
	return mcplib.NewTool(
		"update_workflow",
		mcplib.WithDescription("Replace an existing workflow definition. Increments version."),
		mcplib.WithString("workflow_id",
			mcplib.Description("ID of the workflow to update"),
			mcplib.Required(),
		),
		mcplib.WithString("name",
			mcplib.Description("New workflow name"),
		),
		mcplib.WithObject("trigger",
			mcplib.Description("New trigger configuration: { type, cron, tz, path }"),
		),
		mcplib.WithArray("nodes",
			mcplib.Description("Replacement nodes array: [ { id, type, config } ]"),
		),
		mcplib.WithArray("connections",
			mcplib.Description("Replacement connections array: [ { from, to } ]"),
		),
		mcplib.WithObject("error_handler",
			mcplib.Description("New error handler: { notify, retry }"),
		),
	)
}

func (m *MCPServer) toolTriggerWorkflow() mcplib.Tool {
	return mcplib.NewTool(
		"trigger_workflow",
		mcplib.WithDescription("Execute a workflow. mode=sync waits for result, mode=async fires immediately."),
		mcplib.WithString("workflow_id",
			mcplib.Description("ID of the workflow to execute"),
			mcplib.Required(),
		),
		mcplib.WithObject("payload",
			mcplib.Description("Trigger payload available as {{trigger.*}} in node configs"),
		),
		mcplib.WithString("mode",
			mcplib.Description("Execution mode: sync (default) or async"),
			mcplib.Enum("sync", "async"),
			mcplib.DefaultString("sync"),
		),
	)
}

func (m *MCPServer) toolGetRunStatus() mcplib.Tool {
	return mcplib.NewTool(
		"get_run_status",
		mcplib.WithDescription("Get execution result and per-step logs for a completed or running workflow run."),
		mcplib.WithString("run_id",
			mcplib.Description("ID of the run to retrieve"),
			mcplib.Required(),
		),
	)
}

func (m *MCPServer) toolListWorkflows() mcplib.Tool {
	return mcplib.NewTool(
		"list_workflows",
		mcplib.WithDescription("List all workflows belonging to the current user."),
	)
}

func (m *MCPServer) toolDeleteWorkflow() mcplib.Tool {
	return mcplib.NewTool(
		"delete_workflow",
		mcplib.WithDescription("Permanently delete a workflow. confirm must be true."),
		mcplib.WithString("workflow_id",
			mcplib.Description("ID of the workflow to delete"),
			mcplib.Required(),
		),
		mcplib.WithBoolean("confirm",
			mcplib.Description("Must be true to confirm deletion"),
			mcplib.Required(),
		),
	)
}

// ---- tool handlers ----

func (m *MCPServer) handleCreateWorkflow(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	args := req.GetArguments()
	trigger := parseTrigger(args["trigger"])
	nodes := parseNodes(args["nodes"])
	connections := parseConnections(args["connections"])
	errorHandler := parseErrorHandler(args["error_handler"])

	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("generate id: %w", err)
	}
	now := time.Now().UTC()

	wf := &types.Workflow{
		ID:           id,
		UserID:       m.UserID,
		Name:         name,
		Version:      1,
		Trigger:      trigger,
		Nodes:        nodes,
		Connections:  connections,
		ErrorHandler: errorHandler,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := m.Store.SaveWorkflow(wf); err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}

	return jsonResult(map[string]any{
		"workflow_id": id,
		"version":     1,
		"created_at":  now,
	})
}

func (m *MCPServer) handleUpdateWorkflow(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	workflowID := req.GetString("workflow_id", "")
	if workflowID == "" {
		return nil, fmt.Errorf("workflow_id is required")
	}

	existing, err := m.Store.GetWorkflow(m.UserID, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}

	args := req.GetArguments()

	if name, ok := args["name"].(string); ok && name != "" {
		existing.Name = name
	}
	if _, ok := args["trigger"]; ok {
		existing.Trigger = parseTrigger(args["trigger"])
	}
	if _, ok := args["nodes"]; ok {
		existing.Nodes = parseNodes(args["nodes"])
	}
	if _, ok := args["connections"]; ok {
		existing.Connections = parseConnections(args["connections"])
	}
	if _, ok := args["error_handler"]; ok {
		existing.ErrorHandler = parseErrorHandler(args["error_handler"])
	}

	now := time.Now().UTC()
	existing.Version++
	existing.UpdatedAt = now

	if err := m.Store.SaveWorkflow(existing); err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}

	return jsonResult(map[string]any{
		"workflow_id": existing.ID,
		"version":     existing.Version,
		"updated_at":  now,
	})
}

func (m *MCPServer) handleTriggerWorkflow(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	workflowID := req.GetString("workflow_id", "")
	if workflowID == "" {
		return nil, fmt.Errorf("workflow_id is required")
	}
	mode := req.GetString("mode", "sync")

	wf, err := m.Store.GetWorkflow(m.UserID, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}

	args := req.GetArguments()
	payload := args["payload"]
	env := m.loadUserEnv()

	if mode == "async" {
		runID, _ := newID()
		go func() {
			_, _ = m.Executor.Execute(context.Background(), wf, payload, env)
		}()
		return jsonResult(map[string]any{"run_id": runID, "status": "pending"})
	}

	// Sync mode.
	run, err := m.Executor.Execute(ctx, wf, payload, env)
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}
	b, err := json.Marshal(run)
	if err != nil {
		return nil, err
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (m *MCPServer) handleGetRunStatus(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	runID := req.GetString("run_id", "")
	if runID == "" {
		return nil, fmt.Errorf("run_id is required")
	}

	run, err := m.Store.GetRun(m.UserID, runID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	b, err := json.Marshal(run)
	if err != nil {
		return nil, err
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (m *MCPServer) handleListWorkflows(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	wfs, err := m.Store.ListWorkflows(m.UserID)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}

	type summary struct {
		WorkflowID  string    `json:"workflow_id"`
		Name        string    `json:"name"`
		Version     int       `json:"version"`
		TriggerType string    `json:"trigger_type"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	result := make([]summary, 0, len(wfs))
	for _, wf := range wfs {
		result = append(result, summary{
			WorkflowID:  wf.ID,
			Name:        wf.Name,
			Version:     wf.Version,
			TriggerType: wf.Trigger.Type,
			UpdatedAt:   wf.UpdatedAt,
		})
	}

	return jsonResult(result)
}

func (m *MCPServer) handleDeleteWorkflow(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	workflowID := req.GetString("workflow_id", "")
	if workflowID == "" {
		return nil, fmt.Errorf("workflow_id is required")
	}
	confirm := req.GetBool("confirm", false)
	if !confirm {
		return nil, fmt.Errorf("confirm must be true to delete")
	}

	if err := m.Store.DeleteWorkflow(m.UserID, workflowID); err != nil {
		return nil, fmt.Errorf("delete workflow: %w", err)
	}

	return jsonResult(map[string]any{"deleted": true})
}

// ---- argument parsers ----

func parseTrigger(raw any) types.Trigger {
	m, ok := raw.(map[string]any)
	if !ok {
		return types.Trigger{}
	}
	t := types.Trigger{}
	if v, ok := m["type"].(string); ok {
		t.Type = v
	}
	if v, ok := m["cron"].(string); ok {
		t.Cron = v
	}
	if v, ok := m["tz"].(string); ok {
		t.Tz = v
	}
	if v, ok := m["path"].(string); ok {
		t.Path = v
	}
	return t
}

func parseNodes(raw any) []types.Node {
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	nodes := make([]types.Node, 0, len(slice))
	for _, item := range slice {
		nm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		n := types.Node{}
		if id, ok := nm["id"].(string); ok {
			n.ID = id
		}
		if t, ok := nm["type"].(string); ok {
			n.Type = t
		}
		if cfg, ok := nm["config"]; ok {
			if b, err := json.Marshal(cfg); err == nil {
				n.Config = json.RawMessage(b)
			}
		} else {
			n.Config = json.RawMessage(`{}`)
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func parseConnections(raw any) []types.Connection {
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	conns := make([]types.Connection, 0, len(slice))
	for _, item := range slice {
		cm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := types.Connection{}
		if v, ok := cm["from"].(string); ok {
			c.From = v
		}
		if v, ok := cm["to"].(string); ok {
			c.To = v
		}
		conns = append(conns, c)
	}
	return conns
}

func parseErrorHandler(raw any) types.ErrorHandler {
	m, ok := raw.(map[string]any)
	if !ok {
		return types.ErrorHandler{}
	}
	eh := types.ErrorHandler{}
	if v, ok := m["notify"].(string); ok {
		eh.Notify = v
	}
	if v, ok := m["retry"].(float64); ok {
		eh.Retry = int(v)
	}
	return eh
}

// ---- helpers ----

// jsonResult marshals v and wraps it as a text tool result.
func jsonResult(v any) (*mcplib.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcplib.NewToolResultText(string(b)), nil
}

// newID generates a random 16-byte hex string.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
