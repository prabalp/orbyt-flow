package routes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"orbyt-flow/internal/config"
	"orbyt-flow/internal/services"

	"golang.org/x/oauth2"
)

type oauthStatePayload struct {
	T int64  `json:"t"`
	U string `json:"u"`
}

func signOAuthState(clientSecret, userID string) (string, error) {
	if userID == "" {
		return "", errors.New("missing user id")
	}
	p := oauthStatePayload{T: time.Now().Unix(), U: userID}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(clientSecret))
	mac.Write(b)
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s.%s", hex.EncodeToString(b), sig), nil
}

func verifyOAuthState(clientSecret, state string) (string, error) {
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid state")
	}
	raw, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("state decode: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(clientSecret))
	mac.Write(raw)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(parts[1])), []byte(strings.ToLower(want))) {
		return "", errors.New("state signature mismatch")
	}
	var p oauthStatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if p.U == "" {
		return "", errors.New("state missing user")
	}
	issued := time.Unix(p.T, 0)
	if issued.After(time.Now().Add(5 * time.Minute)) {
		return "", errors.New("invalid state time")
	}
	if time.Since(issued) > 30*time.Minute {
		return "", errors.New("state expired")
	}
	return p.U, nil
}

// BuildGoogleAuthorizeURL returns the Google OAuth consent URL for userID (same logic as GET /oauth/google/authorize).
// State is HMAC-signed so the URL works when the browser callback hits a different process than the one that built the URL (e.g. MCP vs HTTP server).
func BuildGoogleAuthorizeURL(userID string) (string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", errors.New("missing user_id query parameter or X-User-ID header")
	}
	oauthConfig, err := config.GetOAuth2Config()
	if err != nil {
		return "", err
	}
	return BuildGoogleAuthorizeURLWithConfig(oauthConfig, userID)
}

// BuildGoogleAuthorizeURLWithConfig builds the consent URL using an already-loaded OAuth2 config (avoids a second env read).
func BuildGoogleAuthorizeURLWithConfig(oauthConfig *oauth2.Config, userID string) (string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", errors.New("missing user_id query parameter or X-User-ID header")
	}
	if oauthConfig == nil {
		return "", errors.New("missing oauth config")
	}
	state, err := signOAuthState(oauthConfig.ClientSecret, userID)
	if err != nil {
		return "", err
	}
	return oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce), nil
}

// GoogleOAuthStatus returns the JSON-shaped status for userID (same as GET /oauth/google/status).
func GoogleOAuthStatus(userID string) (map[string]any, error) {
	rec, err := services.LoadGoogleToken(userID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"connected": false, "email": ""}, nil
		}
		return nil, err
	}
	out := map[string]any{
		"connected": true,
		"email":     rec.Email,
	}
	if !rec.Expiry.IsZero() {
		out["expiry"] = rec.Expiry.UTC().Format(time.RFC3339)
	}
	return out, nil
}

// DisconnectGoogleOAuth disconnects Google for userID (same as DELETE /oauth/google/disconnect).
func DisconnectGoogleOAuth(ctx context.Context, userID string) error {
	return services.DisconnectGoogle(ctx, userID)
}

// GoogleOAuth registers /oauth/google/* handlers.
type GoogleOAuth struct {
	Auth func(http.HandlerFunc) http.HandlerFunc
}

// NewGoogleOAuth builds a route group. Auth should enforce X-User-ID where required (status, disconnect).
func NewGoogleOAuth(auth func(http.HandlerFunc) http.HandlerFunc) *GoogleOAuth {
	return &GoogleOAuth{Auth: auth}
}

// Register attaches OAuth routes to mux.
func (g *GoogleOAuth) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /oauth/google/authorize", g.handleAuthorize)
	mux.HandleFunc("GET /oauth/google/callback", g.handleCallback)
	mux.HandleFunc("GET /oauth/google/status", g.Auth(g.handleStatus))
	mux.HandleFunc("DELETE /oauth/google/disconnect", g.Auth(g.handleDisconnect))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (g *GoogleOAuth) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}
	authURL, err := BuildGoogleAuthorizeURL(userID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, config.ErrGoogleOAuthNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (g *GoogleOAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	oauthConfig, err := config.GetOAuth2Config()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	q := r.URL.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth: " + errMsg})
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing code or state"})
		return
	}
	userID, err := verifyOAuthState(oauthConfig.ClientSecret, state)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired oauth state: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	rec, err := services.SaveGoogleTokenFromOAuth(ctx, userID, oauthConfig, code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "connected",
		"email":  rec.Email,
	})
}

func (g *GoogleOAuth) handleStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	body, err := GoogleOAuthStatus(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (g *GoogleOAuth) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := DisconnectGoogleOAuth(ctx, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
