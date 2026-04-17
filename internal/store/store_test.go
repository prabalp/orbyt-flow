package store_test

import (
	"testing"
	"time"

	"orbyt-flow/internal/store"
	"orbyt-flow/internal/types"
)

func newWorkflow(id, userID, name string) *types.Workflow {
	now := time.Now()
	return &types.Workflow{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func newRun(id, userID, workflowID string, startedAt time.Time) *types.Run {
	return &types.Run{
		ID:         id,
		WorkflowID: workflowID,
		UserID:     userID,
		Status:     "success",
		StartedAt:  startedAt,
	}
}

func TestSaveAndGetWorkflow(t *testing.T) {
	s := store.NewFileStore(t.TempDir())

	w := newWorkflow("wf-1", "user-1", "My Workflow")
	if err := s.SaveWorkflow(w); err != nil {
		t.Fatalf("SaveWorkflow: %v", err)
	}

	got, err := s.GetWorkflow("user-1", "wf-1")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.ID != w.ID {
		t.Errorf("ID: got %q, want %q", got.ID, w.ID)
	}
	if got.Name != w.Name {
		t.Errorf("Name: got %q, want %q", got.Name, w.Name)
	}
	if got.UserID != w.UserID {
		t.Errorf("UserID: got %q, want %q", got.UserID, w.UserID)
	}
}

func TestListWorkflows(t *testing.T) {
	s := store.NewFileStore(t.TempDir())

	for _, id := range []string{"wf-a", "wf-b", "wf-c"} {
		if err := s.SaveWorkflow(newWorkflow(id, "user-1", id+"-name")); err != nil {
			t.Fatalf("SaveWorkflow %s: %v", id, err)
		}
	}
	// Different user — should not appear.
	if err := s.SaveWorkflow(newWorkflow("wf-x", "user-2", "other")); err != nil {
		t.Fatalf("SaveWorkflow user-2: %v", err)
	}

	list, err := s.ListWorkflows("user-1")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("got %d workflows, want 3", len(list))
	}
}

func TestDeleteWorkflow(t *testing.T) {
	s := store.NewFileStore(t.TempDir())

	w := newWorkflow("wf-del", "user-1", "To Delete")
	if err := s.SaveWorkflow(w); err != nil {
		t.Fatalf("SaveWorkflow: %v", err)
	}
	if err := s.DeleteWorkflow("user-1", "wf-del"); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}

	_, err := s.GetWorkflow("user-1", "wf-del")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestSaveAndGetRun(t *testing.T) {
	s := store.NewFileStore(t.TempDir())

	r := newRun("run-1", "user-1", "wf-1", time.Now())
	if err := s.SaveRun(r); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := s.GetRun("user-1", "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("ID: got %q, want %q", got.ID, r.ID)
	}
	if got.WorkflowID != r.WorkflowID {
		t.Errorf("WorkflowID: got %q, want %q", got.WorkflowID, r.WorkflowID)
	}
}

func TestListRuns(t *testing.T) {
	s := store.NewFileStore(t.TempDir())

	base := time.Now()
	// Three runs for wf-1, one for wf-2 (should be excluded).
	runs := []*types.Run{
		newRun("run-1", "user-1", "wf-1", base.Add(-3*time.Minute)),
		newRun("run-2", "user-1", "wf-1", base.Add(-1*time.Minute)),
		newRun("run-3", "user-1", "wf-1", base.Add(-2*time.Minute)),
		newRun("run-4", "user-1", "wf-2", base),
	}
	for _, r := range runs {
		if err := s.SaveRun(r); err != nil {
			t.Fatalf("SaveRun %s: %v", r.ID, err)
		}
	}

	// All runs for wf-1, no limit.
	list, err := s.ListRuns("user-1", "wf-1", 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("got %d runs, want 3", len(list))
	}
	// Should be sorted newest-first: run-2, run-3, run-1.
	if list[0].ID != "run-2" {
		t.Errorf("first run: got %q, want run-2", list[0].ID)
	}
	if list[1].ID != "run-3" {
		t.Errorf("second run: got %q, want run-3", list[1].ID)
	}
	if list[2].ID != "run-1" {
		t.Errorf("third run: got %q, want run-1", list[2].ID)
	}

	// Apply limit = 2.
	limited, err := s.ListRuns("user-1", "wf-1", 2)
	if err != nil {
		t.Fatalf("ListRuns with limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("got %d runs with limit 2, want 2", len(limited))
	}
}
