package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"orbyt-flow/internal/executor"
	"orbyt-flow/internal/routes"
	"orbyt-flow/internal/store"
	"orbyt-flow/internal/types"
)

const version = "0.1.0"

// ---- response writer that captures status for logging ----

type statusWriter struct {
	http.ResponseWriter
	status int
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// ---- Server ----

// Server is the HTTP API server.
type Server struct {
	Store    store.Store
	Executor *executor.Executor
	DataDir  string
	Port     int
	indexMu  sync.Mutex
	handler  http.Handler

	adminPassword string
	adminSessions *adminSessionStore
}

// NewServer creates a Server and pre-builds all routes.
func NewServer(s store.Store, ex *executor.Executor, dataDir string, port int) *Server {
	srv := &Server{
		Store:    s,
		Executor: ex,
		DataDir:  dataDir,
		Port:     port,
	}
	srv.handler = srv.buildRoutes()
	return srv
}

// ServeHTTP makes Server implement http.Handler (used in tests).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Start binds and serves on s.Port.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.Port)
	log.Printf("orbyt-flow listening on %s", addr)
	return http.ListenAndServe(addr, s.handler)
}

// ---- route registration ----

func (s *Server) buildRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("POST /admin/api/login", s.handleAdminLogin)
	mux.HandleFunc("POST /admin/api/logout", s.handleAdminLogout)
	mux.HandleFunc("GET /admin/api/overview", s.requireAdminSession(s.handleAdminOverview))
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin/{$}", s.serveAdminHTML)

	mux.HandleFunc("POST /workflows", s.auth(s.handleCreateWorkflow))
	mux.HandleFunc("GET /workflows", s.auth(s.handleListWorkflows))
	mux.HandleFunc("GET /workflows/{id}", s.auth(s.handleGetWorkflow))
	mux.HandleFunc("PUT /workflows/{id}", s.auth(s.handleUpdateWorkflow))
	mux.HandleFunc("DELETE /workflows/{id}", s.auth(s.handleDeleteWorkflow))
	mux.HandleFunc("POST /workflows/{id}/trigger", s.auth(s.handleTriggerWorkflow))
	mux.HandleFunc("GET /workflows/{id}/runs", s.auth(s.handleListRuns))
	mux.HandleFunc("GET /runs/{run_id}", s.auth(s.handleGetRun))

	mux.HandleFunc("POST /webhook/{workflow_id}", s.handleWebhook)

	routes.NewGoogleOAuth(s.auth).Register(mux)

	return s.withMiddleware(mux)
}

// ---- middleware ----

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return s.recovery(s.logger(next))
}

func (s *Server) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := newStatusWriter(w)
		next.ServeHTTP(sw, r)
		log.Printf("%s %s user=%s status=%d duration_ms=%d",
			r.Method, r.URL.Path,
			r.Header.Get("X-User-ID"),
			sw.status,
			time.Since(start).Milliseconds(),
		)
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-ID") == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing X-User-ID header"})
			return
		}
		next(w, r)
	}
}

// ---- global index (workflow_id → user_id) ----

func (s *Server) readIndex() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(s.DataDir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx map[string]string
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return idx, nil
}

func (s *Server) writeIndex(idx map[string]string) error {
	if err := os.MkdirAll(s.DataDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.DataDir, "index.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.DataDir, "index.json"))
}

func (s *Server) addToIndex(workflowID, userID string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	idx, err := s.readIndex()
	if err != nil {
		return err
	}
	idx[workflowID] = userID
	return s.writeIndex(idx)
}

func (s *Server) removeFromIndex(workflowID string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	idx, err := s.readIndex()
	if err != nil {
		return err
	}
	delete(idx, workflowID)
	return s.writeIndex(idx)
}

func (s *Server) lookupIndex(workflowID string) (string, bool) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	idx, err := s.readIndex()
	if err != nil {
		return "", false
	}
	userID, ok := idx[workflowID]
	return userID, ok
}

// ---- user environment ----

// loadUserEnv reads DataDir/{userID}/env.json as map[string]string.
// Returns an empty map if the file is absent or malformed.
func (s *Server) loadUserEnv(userID string) map[string]string {
	data, err := os.ReadFile(filepath.Join(s.DataDir, userID, "env.json"))
	if err != nil {
		return map[string]string{}
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return map[string]string{}
	}
	return env
}

// ---- handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version})
}

// workflowBody is the shared shape for POST and PUT bodies.
type workflowBody struct {
	Name         string             `json:"name"`
	Trigger      types.Trigger      `json:"trigger"`
	Nodes        []types.Node       `json:"nodes"`
	Connections  []types.Connection `json:"connections"`
	ErrorHandler types.ErrorHandler `json:"error_handler"`
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	var body workflowBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	id, err := newID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate id"})
		return
	}

	now := time.Now().UTC()
	wf := &types.Workflow{
		ID:           id,
		UserID:       userID,
		Name:         body.Name,
		Version:      1,
		Trigger:      body.Trigger,
		Nodes:        body.Nodes,
		Connections:  body.Connections,
		ErrorHandler: body.ErrorHandler,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.Store.SaveWorkflow(wf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save workflow"})
		return
	}
	if err := s.addToIndex(id, userID); err != nil {
		log.Printf("warning: addToIndex %s: %v", id, err)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"workflow_id": id,
		"version":     1,
		"created_at":  now,
	})
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	id := r.PathValue("id")

	wf, err := s.Store.GetWorkflow(userID, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get workflow"})
		return
	}

	writeJSON(w, http.StatusOK, wf)
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	wfs, err := s.Store.ListWorkflows(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list workflows"})
		return
	}
	if wfs == nil {
		wfs = []*types.Workflow{}
	}

	writeJSON(w, http.StatusOK, wfs)
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	id := r.PathValue("id")

	existing, err := s.Store.GetWorkflow(userID, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get workflow"})
		return
	}

	var body workflowBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	now := time.Now().UTC()
	existing.Name = body.Name
	existing.Trigger = body.Trigger
	existing.Nodes = body.Nodes
	existing.Connections = body.Connections
	existing.ErrorHandler = body.ErrorHandler
	existing.Version++
	existing.UpdatedAt = now

	if err := s.Store.SaveWorkflow(existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save workflow"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workflow_id": existing.ID,
		"version":     existing.Version,
		"updated_at":  now,
	})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	id := r.PathValue("id")

	if err := s.Store.DeleteWorkflow(userID, id); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete workflow"})
		return
	}
	if err := s.removeFromIndex(id); err != nil {
		log.Printf("warning: removeFromIndex %s: %v", id, err)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTriggerWorkflow(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	id := r.PathValue("id")

	wf, err := s.Store.GetWorkflow(userID, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get workflow"})
		return
	}

	var body struct {
		Payload interface{} `json:"payload"`
		Mode    string      `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Mode == "" {
		body.Mode = "sync"
	}

	env := s.loadUserEnv(userID)

	if body.Mode == "async" {
		runID, _ := newID()
		go func() {
			_, _ = s.Executor.Execute(context.Background(), wf, body.Payload, env)
		}()
		writeJSON(w, http.StatusAccepted, map[string]string{
			"run_id": runID,
			"status": "pending",
		})
		return
	}

	// Sync mode: 30 s timeout → 408.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	run, err := s.Executor.Execute(ctx, wf, body.Payload, env)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ctx.Err() == context.DeadlineExceeded {
		writeJSON(w, http.StatusRequestTimeout, map[string]string{"error": "execution timed out"})
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	workflowID := r.PathValue("id")

	limit := 20
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if l, err := strconv.Atoi(ls); err == nil && l > 0 {
			limit = l
		}
	}

	runs, err := s.Store.ListRuns(userID, workflowID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list runs"})
		return
	}
	if runs == nil {
		runs = []*types.Run{}
	}

	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	runID := r.PathValue("run_id")

	run, err := s.Store.GetRun(userID, runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get run"})
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("workflow_id")

	userID, ok := s.lookupIndex(workflowID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}

	wf, err := s.Store.GetWorkflow(userID, workflowID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow not found"})
		return
	}

	var payload interface{}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	env := s.loadUserEnv(userID)
	runID, _ := newID()

	go func() {
		_, _ = s.Executor.Execute(context.Background(), wf, payload, env)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"run_id": runID,
		"status": "pending",
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
