package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"orbyt-flow/internal/executor"
	"orbyt-flow/internal/store"
)

func TestUpsertListDeleteUserSecrets(t *testing.T) {
	dir := t.TempDir()
	st := store.NewFileStore(dir)
	userID := "test-user"
	m := &MCPServer{
		Store:    st,
		Executor: executor.NewExecutor(st),
		DataDir:  dir,
		UserID:   userID,
	}

	req := mcplib.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"secrets": map[string]any{
			"API_KEY": "secret-one",
			"OTHER":   "two",
		},
	}

	_, err := m.handleUpsertUserSecrets(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, userID, "env.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env["API_KEY"] != "secret-one" || env["OTHER"] != "two" {
		t.Fatalf("env: %#v", env)
	}

	listRes, err := m.handleListUserSecrets(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	txt, ok := listRes.Content[0].(mcplib.TextContent)
	if !ok || !strings.Contains(txt.Text, "API_KEY") {
		t.Fatalf("list result: %#v", listRes.Content)
	}

	req2 := mcplib.CallToolRequest{}
	req2.Params.Arguments = map[string]any{
		"secrets": map[string]any{"API_KEY": "new-val"},
	}
	_, err = m.handleUpsertUserSecrets(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}
	m2 := m.loadUserEnv()
	if m2["API_KEY"] != "new-val" || m2["OTHER"] != "two" {
		t.Fatalf("after merge: %#v", m2)
	}

	del := mcplib.CallToolRequest{}
	del.Params.Arguments = map[string]any{"key": "OTHER"}
	_, err = m.handleDeleteUserSecret(context.Background(), del)
	if err != nil {
		t.Fatal(err)
	}
	m3 := m.loadUserEnv()
	if _, ok := m3["OTHER"]; ok {
		t.Fatal("OTHER should be deleted")
	}
	if m3["API_KEY"] != "new-val" {
		t.Fatal("API_KEY should remain")
	}
}
