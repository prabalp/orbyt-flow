package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"orbyt-flow/internal/types"
)

type FileStore struct {
	baseDir string
}

func NewFileStore(baseDir string) *FileStore {
	return &FileStore{baseDir: baseDir}
}

// Verify FileStore implements Store at compile time.
var _ Store = (*FileStore)(nil)

// --- path helpers ---

func (s *FileStore) workflowPath(userID, workflowID string) string {
	return filepath.Join(s.baseDir, userID, "workflows", workflowID+".json")
}

func (s *FileStore) workflowDir(userID string) string {
	return filepath.Join(s.baseDir, userID, "workflows")
}

func (s *FileStore) runPath(userID, runID string) string {
	return filepath.Join(s.baseDir, userID, "runs", runID+".json")
}

func (s *FileStore) runDir(userID string) string {
	return filepath.Join(s.baseDir, userID, "runs")
}

// --- atomic write helper ---

func writeAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// --- Workflow ---

func (s *FileStore) SaveWorkflow(w *types.Workflow) error {
	return writeAtomic(s.workflowPath(w.UserID, w.ID), w)
}

func (s *FileStore) GetWorkflow(userID, workflowID string) (*types.Workflow, error) {
	data, err := os.ReadFile(s.workflowPath(userID, workflowID))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var w types.Workflow
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return &w, nil
}

func (s *FileStore) ListWorkflows(userID string) ([]*types.Workflow, error) {
	matches, err := filepath.Glob(filepath.Join(s.workflowDir(userID), "*.json"))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	workflows := make([]*types.Workflow, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		var w types.Workflow
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		workflows = append(workflows, &w)
	}
	return workflows, nil
}

func (s *FileStore) DeleteWorkflow(userID, workflowID string) error {
	if err := os.Remove(s.workflowPath(userID, workflowID)); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// --- Run ---

func (s *FileStore) SaveRun(r *types.Run) error {
	return writeAtomic(s.runPath(r.UserID, r.ID), r)
}

func (s *FileStore) GetRun(userID, runID string) (*types.Run, error) {
	data, err := os.ReadFile(s.runPath(userID, runID))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var r types.Run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return &r, nil
}

func (s *FileStore) ListRuns(userID, workflowID string, limit int) ([]*types.Run, error) {
	matches, err := filepath.Glob(filepath.Join(s.runDir(userID), "*.json"))
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var runs []*types.Run
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		var r types.Run
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		if r.WorkflowID == workflowID {
			runs = append(runs, &r)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}
