package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

// AddRepo handles add_repo.
func AddRepo(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError("name is required"), nil
		}
		url, err := req.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError("url is required"), nil
		}
		description := req.GetString("description", "")
		language := req.GetString("language", "")

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "add_repo", map[string]interface{}{
			"project_id": pid,
			"name":       name,
		})

		rid, err := svc.DB.AddRepo(ctx, claims.UID, pid, &models.CreateRepoInput{
			Name:        name,
			URL:         url,
			Description: description,
			Language:    language,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to add repo: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"repo_id": rid,
			"message": "Repo added.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// ListRepos handles list_repos.
func ListRepos(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		repos, err := svc.DB.ListRepos(ctx, claims.UID, pid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list repos: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]interface{}{"repos": repos})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// UpdateRepo handles update_repo.
func UpdateRepo(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		rid, err := req.RequireString("repo_id")
		if err != nil {
			return mcp.NewToolResultError("repo_id is required"), nil
		}

		updates := make(map[string]interface{})
		for _, field := range []string{"name", "url", "description", "language"} {
			v := req.GetString(field, "")
			if v != "" {
				updates[field] = v
			}
		}
		if len(updates) == 0 {
			return mcp.NewToolResultError("at least one field (name, url, description, language) must be provided"), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "update_repo", map[string]interface{}{
			"project_id": pid,
			"repo_id":    rid,
		})

		if err := svc.DB.UpdateRepo(ctx, claims.UID, pid, rid, updates); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update repo: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Repo updated."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// RemoveRepo handles remove_repo.
func RemoveRepo(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		rid, err := req.RequireString("repo_id")
		if err != nil {
			return mcp.NewToolResultError("repo_id is required"), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "remove_repo", map[string]interface{}{
			"project_id": pid,
			"repo_id":    rid,
		})

		if err := svc.DB.RemoveRepo(ctx, claims.UID, pid, rid); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to remove repo: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Repo removed."})
		return mcp.NewToolResultText(string(out)), nil
	}
}
