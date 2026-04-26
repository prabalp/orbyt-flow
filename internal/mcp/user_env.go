package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func (m *MCPServer) userEnvPath() string {
	return filepath.Join(m.DataDir, m.UserID, "env.json")
}

// loadUserEnv reads DataDir/{userID}/env.json as map[string]string.
func (m *MCPServer) loadUserEnv() map[string]string {
	data, err := os.ReadFile(m.userEnvPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}
		}
		return map[string]string{}
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return map[string]string{}
	}
	if env == nil {
		return map[string]string{}
	}
	return env
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

func (m *MCPServer) toolListUserSecrets() mcplib.Tool {
	return mcplib.NewTool(
		"list_user_secrets",
		mcplib.WithDescription("List names of environment keys stored for the current user (MCP_USER_ID). Values are never returned — use this to see which {{env.KEY}} placeholders exist."),
	)
}

func (m *MCPServer) toolUpsertUserSecrets() mcplib.Tool {
	return mcplib.NewTool(
		"upsert_user_secrets",
		mcplib.WithDescription("Create or update secret values for the current user. Merges into env.json; existing keys are overwritten. Values are available in workflows as {{env.KEY_NAME}}."),
		mcplib.WithObject("secrets",
			mcplib.Description("Object map of KEY to string value, e.g. {\"ANTHROPIC_API_KEY\":\"sk-...\",\"TELEGRAM_BOT_TOKEN\":\"...\"}"),
			mcplib.Required(),
		),
	)
}

func (m *MCPServer) toolDeleteUserSecret() mcplib.Tool {
	return mcplib.NewTool(
		"delete_user_secret",
		mcplib.WithDescription("Remove one key from the current user's env.json."),
		mcplib.WithString("key",
			mcplib.Description("Environment variable name to remove"),
			mcplib.Required(),
		),
	)
}

func (m *MCPServer) handleListUserSecrets(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	env := m.loadUserEnv()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return jsonResult(map[string]any{"keys": keys})
}

func (m *MCPServer) handleUpsertUserSecrets(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	raw, ok := args["secrets"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("secrets must be a non-empty object")
	}

	merged := m.loadUserEnv()
	var updatedKeys []string
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, err := secretValueToString(v)
		if err != nil {
			return nil, fmt.Errorf("secrets[%q]: %w", k, err)
		}
		merged[k] = s
		updatedKeys = append(updatedKeys, k)
	}
	if len(updatedKeys) == 0 {
		return nil, fmt.Errorf("secrets must contain at least one non-empty key name")
	}

	if err := writeUserEnvAtomic(m.userEnvPath(), merged); err != nil {
		return nil, fmt.Errorf("save env: %w", err)
	}

	sort.Strings(updatedKeys)
	return jsonResult(map[string]any{
		"updated_keys": updatedKeys,
		"total_keys":   len(merged),
	})
}

func (m *MCPServer) handleDeleteUserSecret(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	key := strings.TrimSpace(req.GetString("key", ""))
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}

	env := m.loadUserEnv()
	if _, ok := env[key]; !ok {
		return jsonResult(map[string]any{"deleted": false, "key": key})
	}
	delete(env, key)
	if err := writeUserEnvAtomic(m.userEnvPath(), env); err != nil {
		return nil, fmt.Errorf("save env: %w", err)
	}
	return jsonResult(map[string]any{"deleted": true, "key": key})
}

func secretValueToString(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case float64:
		// JSON numbers; tokens are rarely numeric but coerce cleanly.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%.0f", t), nil
		}
		return fmt.Sprint(t), nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	case nil:
		return "", fmt.Errorf("value cannot be null")
	default:
		return fmt.Sprint(t), nil
	}
}
