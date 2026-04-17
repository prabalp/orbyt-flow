package types

import (
	"encoding/json"
	"time"
)

type Run struct {
	ID         string       `json:"run_id"`
	WorkflowID string       `json:"workflow_id"`
	UserID     string       `json:"user_id"`
	Status     string       `json:"status"` // "pending" | "running" | "success" | "failed"
	Steps      []StepResult `json:"steps"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
	Error      string       `json:"error,omitempty"`
}

type StepResult struct {
	NodeID     string          `json:"node_id"`
	NodeType   string          `json:"node_type"`
	Status     string          `json:"status"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int64           `json:"duration_ms"`
}
