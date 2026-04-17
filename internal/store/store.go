package store

import "orbyt-flow/internal/types"

type Store interface {
	SaveWorkflow(w *types.Workflow) error
	GetWorkflow(userID, workflowID string) (*types.Workflow, error)
	ListWorkflows(userID string) ([]*types.Workflow, error)
	DeleteWorkflow(userID, workflowID string) error

	SaveRun(r *types.Run) error
	GetRun(userID, runID string) (*types.Run, error)
	ListRuns(userID, workflowID string, limit int) ([]*types.Run, error)
}
