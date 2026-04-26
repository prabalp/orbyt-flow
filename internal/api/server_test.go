package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"orbyt-flow/internal/api"
	"orbyt-flow/internal/executor"
	"orbyt-flow/internal/store"
	"orbyt-flow/internal/types"
)

// ---- test helpers ----

const testUserID = "user-test"

func newTestServer(t *testing.T) *api.Server {
	t.Helper()
	dir := t.TempDir()
	s := store.NewFileStore(dir)
	ex := executor.NewExecutor(s)
	return api.NewServer(s, ex, dir, 0)
}

func do(t *testing.T, srv *api.Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func withUser(userID string) map[string]string {
	return map[string]string{"X-User-ID": userID}
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&m); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return m
}

// logWorkflow returns a minimal workflow with one log node.
func logWorkflow() map[string]any {
	return map[string]any{
		"name": "test-workflow",
		"trigger": map[string]any{
			"type": "manual",
		},
		"nodes": []map[string]any{
			{
				"id":     "n1",
				"type":   types.NodeLog,
				"config": map[string]any{"message": "hello", "level": "info"},
			},
		},
		"connections":   []any{},
		"error_handler": map[string]any{"notify": "none", "retry": 0},
	}
}

// createWorkflow is a test helper that creates a workflow and returns its ID.
func createWorkflow(t *testing.T, srv *api.Server) string {
	t.Helper()
	rr := do(t, srv, http.MethodPost, "/workflows", logWorkflow(), withUser(testUserID))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create workflow: got %d, want 201 — body: %s", rr.Code, rr.Body)
	}
	body := decodeBody(t, rr)
	id, ok := body["workflow_id"].(string)
	if !ok || id == "" {
		t.Fatalf("create workflow: missing workflow_id in response")
	}
	return id
}

// ---- tests ----

func TestHealthCheck(t *testing.T) {
	srv := newTestServer(t)
	rr := do(t, srv, http.MethodGet, "/health", nil, nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	body := decodeBody(t, rr)
	if body["status"] != "ok" {
		t.Errorf("status field: got %v, want ok", body["status"])
	}
	if body["version"] == "" {
		t.Errorf("version field missing")
	}
}

func TestCreateAndGetWorkflow(t *testing.T) {
	srv := newTestServer(t)
	id := createWorkflow(t, srv)

	// GET the workflow back.
	rr := do(t, srv, http.MethodGet, "/workflows/"+id, nil, withUser(testUserID))
	if rr.Code != http.StatusOK {
		t.Fatalf("get workflow: got %d, want 200 — body: %s", rr.Code, rr.Body)
	}
	body := decodeBody(t, rr)
	if body["workflow_id"] != id {
		t.Errorf("workflow_id: got %v, want %s", body["workflow_id"], id)
	}
	if body["name"] != "test-workflow" {
		t.Errorf("name: got %v, want test-workflow", body["name"])
	}
	if body["user_id"] != testUserID {
		t.Errorf("user_id: got %v, want %s", body["user_id"], testUserID)
	}
	if body["version"].(float64) != 1 {
		t.Errorf("version: got %v, want 1", body["version"])
	}
}

func TestListWorkflows(t *testing.T) {
	srv := newTestServer(t)

	// Create two workflows for the test user, one for another user.
	createWorkflow(t, srv)
	createWorkflow(t, srv)
	rr := do(t, srv, http.MethodPost, "/workflows", logWorkflow(), withUser("other-user"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create other workflow: got %d", rr.Code)
	}

	rr = do(t, srv, http.MethodGet, "/workflows", nil, withUser(testUserID))
	if rr.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", rr.Code)
	}

	var list []any
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("got %d workflows, want 2", len(list))
	}
}

func TestUpdateWorkflow(t *testing.T) {
	srv := newTestServer(t)
	id := createWorkflow(t, srv)

	updated := logWorkflow()
	updated["name"] = "updated-workflow"

	rr := do(t, srv, http.MethodPut, "/workflows/"+id, updated, withUser(testUserID))
	if rr.Code != http.StatusOK {
		t.Fatalf("update: got %d, want 200 — body: %s", rr.Code, rr.Body)
	}
	body := decodeBody(t, rr)
	if body["workflow_id"] != id {
		t.Errorf("workflow_id: got %v, want %s", body["workflow_id"], id)
	}
	if body["version"].(float64) != 2 {
		t.Errorf("version: got %v, want 2", body["version"])
	}

	// Verify the name change is persisted.
	rr = do(t, srv, http.MethodGet, "/workflows/"+id, nil, withUser(testUserID))
	body = decodeBody(t, rr)
	if body["name"] != "updated-workflow" {
		t.Errorf("persisted name: got %v, want updated-workflow", body["name"])
	}
}

func TestDeleteWorkflow(t *testing.T) {
	srv := newTestServer(t)
	id := createWorkflow(t, srv)

	rr := do(t, srv, http.MethodDelete, "/workflows/"+id, nil, withUser(testUserID))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d, want 204 — body: %s", rr.Code, rr.Body)
	}

	// Must be 404 after deletion.
	rr = do(t, srv, http.MethodGet, "/workflows/"+id, nil, withUser(testUserID))
	if rr.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", rr.Code)
	}
}

func TestTriggerSync(t *testing.T) {
	srv := newTestServer(t)
	id := createWorkflow(t, srv)

	rr := do(t, srv, http.MethodPost, "/workflows/"+id+"/trigger",
		map[string]any{"mode": "sync", "payload": map[string]any{"key": "val"}},
		withUser(testUserID))

	if rr.Code != http.StatusOK {
		t.Fatalf("trigger sync: got %d, want 200 — body: %s", rr.Code, rr.Body)
	}
	body := decodeBody(t, rr)
	if body["status"] != "success" {
		t.Errorf("run status: got %v, want success", body["status"])
	}
	if body["run_id"] == "" {
		t.Error("run_id missing")
	}
	steps, ok := body["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Errorf("steps: got %v, want 1 entry", body["steps"])
	}
}

func TestTriggerAsync(t *testing.T) {
	srv := newTestServer(t)
	// Give background goroutine time to finish before t.TempDir cleanup (LIFO).
	t.Cleanup(func() { time.Sleep(150 * time.Millisecond) })
	id := createWorkflow(t, srv)

	start := time.Now()
	rr := do(t, srv, http.MethodPost, "/workflows/"+id+"/trigger",
		map[string]any{"mode": "async", "payload": nil},
		withUser(testUserID))
	elapsed := time.Since(start)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("trigger async: got %d, want 202 — body: %s", rr.Code, rr.Body)
	}
	body := decodeBody(t, rr)
	if body["status"] != "pending" {
		t.Errorf("status: got %v, want pending", body["status"])
	}
	if body["run_id"] == "" {
		t.Error("run_id missing")
	}
	// The response must come back well before any workflow execution completes.
	if elapsed > 2*time.Second {
		t.Errorf("async response took %v, expected nearly immediate", elapsed)
	}
}

func TestMissingUserIDHeader(t *testing.T) {
	srv := newTestServer(t)

	authRoutes := []struct{ method, path string }{
		{http.MethodPost, "/workflows"},
		{http.MethodGet, "/workflows"},
		{http.MethodGet, "/workflows/nonexistent"},
		{http.MethodPut, "/workflows/nonexistent"},
		{http.MethodDelete, "/workflows/nonexistent"},
		{http.MethodPost, "/workflows/nonexistent/trigger"},
		{http.MethodGet, "/workflows/nonexistent/runs"},
		{http.MethodGet, "/runs/nonexistent"},
	}

	for _, tc := range authRoutes {
		rr := do(t, srv, tc.method, tc.path, map[string]any{}, nil) // no X-User-ID
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s %s: got %d, want 400", tc.method, tc.path, rr.Code)
		}
	}
}

func TestWebhookTrigger(t *testing.T) {
	srv := newTestServer(t)
	// Give background goroutines time to finish before t.TempDir cleanup runs.
	// Registered after TempDir so it fires first (LIFO cleanup order).
	t.Cleanup(func() { time.Sleep(150 * time.Millisecond) })

	// Create workflow via normal API (updates index.json).
	id := createWorkflow(t, srv)

	// Trigger via webhook — no X-User-ID header required.
	rr := do(t, srv, http.MethodPost, "/webhook/"+id,
		map[string]any{"event": "push"},
		nil) // intentionally no auth header

	if rr.Code != http.StatusAccepted {
		t.Fatalf("webhook: got %d, want 202 — body: %s", rr.Code, rr.Body)
	}
	body := decodeBody(t, rr)
	if body["status"] != "pending" {
		t.Errorf("status: got %v, want pending", body["status"])
	}
	if body["run_id"] == "" {
		t.Error("run_id missing")
	}
}

func TestAdminDisabledWithoutPassword(t *testing.T) {
	srv := newTestServer(t)

	rr := do(t, srv, http.MethodGet, "/admin/api/overview", nil, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("overview: got %d, want 404", rr.Code)
	}

	rr = do(t, srv, http.MethodPost, "/admin/api/login", map[string]string{"password": "x"}, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("login: got %d, want 404", rr.Code)
	}
}

func TestAdminLoginAndOverview(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAdminPassword("secret-admin")

	rr := do(t, srv, http.MethodPost, "/admin/api/login", map[string]string{"password": "wrong"}, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("login wrong password: got %d, want 401", rr.Code)
	}

	rr = do(t, srv, http.MethodPost, "/admin/api/login", map[string]string{"password": "secret-admin"}, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("login: got %d, want 200 — %s", rr.Code, rr.Body)
	}
	cookies := rr.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == "orbyt_admin_session" {
			session = c
			break
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("expected session cookie")
	}

	rr = do(t, srv, http.MethodGet, "/admin/api/overview", nil, map[string]string{
		"Cookie": session.String(),
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("overview: got %d, want 200 — %s", rr.Code, rr.Body)
	}
	var overview map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if overview["data_dir"] == nil {
		t.Error("expected data_dir in overview")
	}
}
