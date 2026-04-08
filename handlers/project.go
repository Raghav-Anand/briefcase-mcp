package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-internal/validation"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

// CreateProject handles create_project.
func CreateProject(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError("name is required"), nil
		}
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError("description is required"), nil
		}
		repoURL := req.GetString("repo_url", "")
		techStack := req.GetStringSlice("tech_stack", nil)

		pid, err := svc.DB.CreateProject(ctx, claims.UID, &models.CreateProjectInput{
			Name:        name,
			Description: description,
			RepoURL:     repoURL,
			TechStack:   techStack,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create project: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"project_id": pid,
			"message":    "Project created.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// ListProjects handles list_projects.
func ListProjects(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		includeArchived := req.GetBool("include_archived", false)

		projects, err := svc.DB.ListProjects(ctx, claims.UID, includeArchived)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list projects: %v", err)), nil
		}

		type projectSummary struct {
			ID                 string `json:"id"`
			Name               string `json:"name"`
			Status             string `json:"status"`
			LastSessionSummary string `json:"last_session_summary,omitempty"`
			UpdatedAt          string `json:"updated_at"`
		}
		summaries := make([]projectSummary, 0, len(projects))
		for _, p := range projects {
			summaries = append(summaries, projectSummary{
				ID:                 p.ID,
				Name:               p.Name,
				Status:             p.Status,
				LastSessionSummary: p.LastSessionSummary,
				UpdatedAt:          p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}

		out, _ := json.Marshal(map[string]interface{}{"projects": summaries})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// GetProject handles get_project.
func GetProject(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		project, err := svc.DB.GetProject(ctx, claims.UID, pid)
		if err != nil {
			return mcp.NewToolResultError("Project not found. Use list_projects() to see your projects."), nil
		}

		out, _ := json.MarshalIndent(project, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

// UpdateProject handles update_project.
func UpdateProject(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		updates := make(map[string]interface{})
		if name := req.GetString("name", ""); name != "" {
			updates["name"] = name
		}
		if desc := req.GetString("description", ""); desc != "" {
			updates["description"] = desc
		}
		if status := req.GetString("status", ""); status != "" {
			if err := validation.ValidateProjectStatus(status); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			updates["status"] = status
		}
		if repoURL := req.GetString("repo_url", ""); repoURL != "" {
			updates["repo_url"] = repoURL
		}
		if techStack := req.GetStringSlice("tech_stack", nil); len(techStack) > 0 {
			updates["tech_stack"] = techStack
		}

		if len(updates) == 0 {
			return mcp.NewToolResultError("no fields to update provided"), nil
		}

		if err := svc.DB.UpdateProject(ctx, claims.UID, pid, updates); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update project: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Project updated."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// ArchiveProject handles archive_project.
func ArchiveProject(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		if err := svc.DB.ArchiveProject(ctx, claims.UID, pid); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to archive project: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Project archived."})
		return mcp.NewToolResultText(string(out)), nil
	}
}
