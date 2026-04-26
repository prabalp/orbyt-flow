package api

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"orbyt-flow/internal/types"
)

//go:embed admin/*
var adminStatic embed.FS

const adminSessionCookie = "orbyt_admin_session"

type adminSessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token hex -> expiry
}

func newAdminSessionStore() *adminSessionStore {
	return &adminSessionStore{tokens: make(map[string]time.Time)}
}

func (a *adminSessionStore) issue(ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	a.mu.Lock()
	a.tokens[tok] = time.Now().Add(ttl)
	a.mu.Unlock()
	return tok, nil
}

func (a *adminSessionStore) valid(tok string) bool {
	if tok == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.tokens[tok]
	if !ok || time.Now().After(exp) {
		delete(a.tokens, tok)
		return false
	}
	return true
}

func (a *adminSessionStore) revoke(tok string) {
	a.mu.Lock()
	delete(a.tokens, tok)
	a.mu.Unlock()
}

// SetAdminPassword enables the admin UI at /admin when non-empty.
func (s *Server) SetAdminPassword(password string) {
	s.adminPassword = password
	if s.adminSessions == nil {
		s.adminSessions = newAdminSessionStore()
	}
}

func (s *Server) adminSessionCookie(w http.ResponseWriter, r *http.Request) (*http.Cookie, bool) {
	c, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return nil, false
	}
	return c, c.Value != ""
}

func (s *Server) requireAdminSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.adminPassword == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin UI is not configured"})
			return
		}
		if s.adminSessions == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		c, ok := s.adminSessionCookie(w, r)
		if !ok || !s.adminSessions.valid(c.Value) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if s.adminPassword == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin UI is not configured"})
		return
	}
	if s.adminSessions == nil {
		s.adminSessions = newAdminSessionStore()
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.adminPassword)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
		return
	}

	tok, err := s.adminSessions.issue(7 * 24 * time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create session"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    tok,
		Path:     "/admin",
		MaxAge:   7 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if c, ok := s.adminSessionCookie(w, r); ok && s.adminSessions != nil {
		s.adminSessions.revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type adminFolder struct {
	UserID       string            `json:"user_id"`
	RelativeRoot string            `json:"relative_root"`
	WorkflowDir  string            `json:"workflow_dir"`
	Workflows    []*types.Workflow `json:"workflows"`
}

type adminScheduleRow struct {
	UserID     string `json:"user_id"`
	WorkflowID string `json:"workflow_id"`
	Name       string `json:"name"`
	Cron       string `json:"cron"`
	Tz         string `json:"tz"`
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.DataDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read data directory"})
		return
	}

	var userIDs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		userIDs = append(userIDs, name)
	}
	sort.Strings(userIDs)

	folders := make([]adminFolder, 0, len(userIDs))
	var schedules []adminScheduleRow

	for _, uid := range userIDs {
		wfs, err := s.Store.ListWorkflows(uid)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list workflows for " + uid})
			return
		}
		if wfs == nil {
			wfs = []*types.Workflow{}
		}
		sort.Slice(wfs, func(i, j int) bool {
			return strings.ToLower(wfs[i].Name) < strings.ToLower(wfs[j].Name)
		})

		folders = append(folders, adminFolder{
			UserID:       uid,
			RelativeRoot: uid,
			WorkflowDir:  filepath.Join(uid, "workflows"),
			Workflows:    wfs,
		})

		for _, wf := range wfs {
			if strings.EqualFold(wf.Trigger.Type, "schedule") {
				schedules = append(schedules, adminScheduleRow{
					UserID:     uid,
					WorkflowID: wf.ID,
					Name:       wf.Name,
					Cron:       wf.Trigger.Cron,
					Tz:         wf.Trigger.Tz,
				})
			}
		}
	}

	sort.Slice(schedules, func(i, j int) bool {
		if schedules[i].UserID != schedules[j].UserID {
			return schedules[i].UserID < schedules[j].UserID
		}
		return strings.ToLower(schedules[i].Name) < strings.ToLower(schedules[j].Name)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"data_dir":  s.DataDir,
		"users":     folders,
		"schedules": schedules,
	})
}

func (s *Server) serveAdminHTML(w http.ResponseWriter, _ *http.Request) {
	data, err := fs.ReadFile(adminStatic, "admin/index.html")
	if err != nil {
		http.Error(w, "admin UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
