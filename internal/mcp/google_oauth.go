package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"orbyt-flow/internal/config"
	"orbyt-flow/internal/routes"
)

func (m *MCPServer) toolGoogleAuthorize() mcplib.Tool {
	return mcplib.NewTool(
		"google_authorize",
		mcplib.WithDescription("Get the Google OAuth2 authorization URL for the current user to connect their Google account."),
	)
}

func (m *MCPServer) toolGoogleStatus() mcplib.Tool {
	return mcplib.NewTool(
		"google_status",
		mcplib.WithDescription("Check if the current user has connected their Google account."),
	)
}

func (m *MCPServer) toolGoogleDisconnect() mcplib.Tool {
	return mcplib.NewTool(
		"google_disconnect",
		mcplib.WithDescription("Disconnect the current user's Google account."),
	)
}

func (m *MCPServer) handleGoogleAuthorize(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	if m.UserID == "" {
		return nil, fmt.Errorf("MCP_USER_ID is not set")
	}
	cfg, err := config.LoadGoogleOAuthConfig()
	if err != nil {
		return nil, fmt.Errorf("Google OAuth2 is not configured on this server. "+
			"Please set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and "+
			"GOOGLE_REDIRECT_URI in your .env file or environment: %w", err)
	}
	oauthCfg := cfg.GetOAuth2Config()
	url, err := routes.BuildGoogleAuthorizeURLWithConfig(oauthCfg, m.UserID)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]string{"url": url})
}

func (m *MCPServer) handleGoogleStatus(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	if m.UserID == "" {
		return nil, fmt.Errorf("MCP_USER_ID is not set")
	}
	body, err := routes.GoogleOAuthStatus(m.UserID)
	if err != nil {
		return nil, err
	}
	return jsonResult(body)
}

func (m *MCPServer) handleGoogleDisconnect(ctx context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	if m.UserID == "" {
		return nil, fmt.Errorf("MCP_USER_ID is not set")
	}
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := routes.DisconnectGoogleOAuth(ctx2, m.UserID); err != nil {
		return nil, err
	}
	return jsonResult(map[string]string{"status": "disconnected"})
}
