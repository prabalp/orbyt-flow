package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"orbyt-flow/internal/config"

	"golang.org/x/oauth2"
)

const (
	// GoogleUserSecretsAccessKey is the env.json key synced from Google OAuth for use in workflows as {{env.GOOGLE_ACCESS_TOKEN}}.
	GoogleUserSecretsAccessKey = "GOOGLE_ACCESS_TOKEN"
	googleOAuthFileName        = "oauth_google.json"
)

var (
	dataRootMu sync.RWMutex
	dataRoot   string
)

// SetDataDir sets the file-store root (same as FLOWENGINE_DATA_DIR). Required for SaveGoogleToken / LoadGoogleToken / etc.
func SetDataDir(dir string) {
	dataRootMu.Lock()
	defer dataRootMu.Unlock()
	dataRoot = dir
}

func rootDir() string {
	dataRootMu.RLock()
	defer dataRootMu.RUnlock()
	return dataRoot
}

func googleOAuthPath(userID string) string {
	return filepath.Join(rootDir(), userID, googleOAuthFileName)
}

func userEnvPath(userID string) string {
	return filepath.Join(rootDir(), userID, "env.json")
}

// GoogleToken is persisted as oauth_google.json per user (logical key oauth_google_{user_id}).
type GoogleToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Email        string    `json:"email"`
}

// SaveGoogleToken writes the token record for userID.
func SaveGoogleToken(userID string, token *GoogleToken) error {
	if token == nil {
		return errors.New("google_auth: nil token")
	}
	if rootDir() == "" {
		return errors.New("google_auth: data dir not set (call services.SetDataDir)")
	}
	path := googleOAuthPath(userID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadGoogleToken reads oauth_google.json for the user.
func LoadGoogleToken(userID string) (*GoogleToken, error) {
	if rootDir() == "" {
		return nil, errors.New("google_auth: data dir not set (call services.SetDataDir)")
	}
	data, err := os.ReadFile(googleOAuthPath(userID))
	if err != nil {
		return nil, err
	}
	var t GoogleToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteGoogleToken removes oauth_google.json for the user.
func DeleteGoogleToken(userID string) error {
	if rootDir() == "" {
		return errors.New("google_auth: data dir not set (call services.SetDataDir)")
	}
	err := os.Remove(googleOAuthPath(userID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func readUserEnvMap(userID string) (map[string]string, error) {
	data, err := os.ReadFile(userEnvPath(userID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	if env == nil {
		env = map[string]string{}
	}
	return env, nil
}

func writeUserEnvAtomic(path string, env map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if env == nil {
		env = map[string]string{}
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveUserSecret merges a key into the user's env.json (secrets).
func SaveUserSecret(userID, key, value string) error {
	if rootDir() == "" {
		return errors.New("google_auth: data dir not set (call services.SetDataDir)")
	}
	path := userEnvPath(userID)
	env, err := readUserEnvMap(userID)
	if err != nil {
		return err
	}
	if value == "" {
		delete(env, key)
	} else {
		env[key] = value
	}
	return writeUserEnvAtomic(path, env)
}

// DeleteUserSecret removes a key from the user's env.json.
func DeleteUserSecret(userID, key string) error {
	return SaveUserSecret(userID, key, "")
}

// ReadUserEnvValue returns one key from env.json for the user.
func ReadUserEnvValue(userID, key string) string {
	if rootDir() == "" {
		return ""
	}
	env, err := readUserEnvMap(userID)
	if err != nil {
		return ""
	}
	return env[key]
}

// EnsureFreshGoogleToken refreshes the access token when it is expired or within 60s of expiry.
// If the user is not connected (no token file), it returns the load error.
func EnsureFreshGoogleToken(userID string) error {
	stored, err := LoadGoogleToken(userID)
	if err != nil {
		return err
	}
	if time.Now().Before(stored.Expiry.Add(-60 * time.Second)) {
		return nil
	}

	oauthConfig, err := config.GetOAuth2Config()
	if err != nil {
		return err
	}
	tok := &oauth2.Token{
		AccessToken:  stored.AccessToken,
		RefreshToken: stored.RefreshToken,
		Expiry:       stored.Expiry,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tokenSource := oauthConfig.TokenSource(ctx, tok)
	newToken, err := tokenSource.Token()
	if err != nil {
		_ = DeleteGoogleToken(userID)
		_ = DeleteUserSecret(userID, GoogleUserSecretsAccessKey)
		return fmt.Errorf("token refresh failed, please reconnect: %w", err)
	}
	refresh := newToken.RefreshToken
	if refresh == "" {
		refresh = stored.RefreshToken
	}
	err = SaveGoogleToken(userID, &GoogleToken{
		AccessToken:  newToken.AccessToken,
		RefreshToken: refresh,
		Expiry:       newToken.Expiry,
		Email:        stored.Email,
	})
	if err != nil {
		return err
	}
	return SaveUserSecret(userID, GoogleUserSecretsAccessKey, newToken.AccessToken)
}

// SaveGoogleTokenFromOAuth exchanges an auth code and persists the token + user email.
func SaveGoogleTokenFromOAuth(ctx context.Context, userID string, oauthCfg *oauth2.Config, code string) (*GoogleToken, error) {
	tok, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google_auth: exchange code: %w", err)
	}
	email, err := fetchGoogleEmail(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	refresh := tok.RefreshToken
	st := &GoogleToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: refresh,
		Expiry:       tok.Expiry,
		Email:        email,
	}
	if err := SaveGoogleToken(userID, st); err != nil {
		return nil, err
	}
	if err := SaveUserSecret(userID, GoogleUserSecretsAccessKey, st.AccessToken); err != nil {
		return nil, fmt.Errorf("google_auth: set %s: %w", GoogleUserSecretsAccessKey, err)
	}
	return st, nil
}

func fetchGoogleEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("google_auth: userinfo request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google_auth: userinfo status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var u struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", fmt.Errorf("google_auth: parse userinfo: %w", err)
	}
	return u.Email, nil
}

// RevokeGoogleToken revokes a refresh or access token via Google's revoke endpoint.
func RevokeGoogleToken(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	u, err := url.Parse("https://oauth2.googleapis.com/revoke")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("google_auth: revoke: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("google_auth: revoke status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// DisconnectGoogle revokes the refresh token, deletes oauth_google.json, and removes GOOGLE_ACCESS_TOKEN from secrets.
func DisconnectGoogle(ctx context.Context, userID string) error {
	rec, err := LoadGoogleToken(userID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		_ = RevokeGoogleToken(ctx, rec.RefreshToken)
	}
	_ = DeleteGoogleToken(userID)
	return DeleteUserSecret(userID, GoogleUserSecretsAccessKey)
}
