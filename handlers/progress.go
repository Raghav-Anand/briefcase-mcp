package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-internal/validation"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

// AddMilestone handles add_milestone.
func AddMilestone(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError("title is required"), nil
		}
		description := req.GetString("description", "")
		dueDateStr := req.GetString("due_date", "")
		// Parse tasks: accept array of {title, repo_name?} objects.
		var tasks []models.MilestoneTaskInput
		if raw, ok := req.GetArguments()["tasks"]; ok && raw != nil {
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &tasks)
		}

		// Find active session for logging.
		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}

		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "add_milestone", map[string]interface{}{
			"project_id": pid,
			"title":      title,
		})

		input := &models.CreateMilestoneInput{
			Title:       title,
			Description: description,
			SessionID:   sessionID,
			Tasks:       tasks,
		}
		if dueDateStr != "" {
			t, err := time.Parse(time.RFC3339, dueDateStr)
			if err != nil {
				// Try date-only format.
				t, err = time.Parse("2006-01-02", dueDateStr)
				if err != nil {
					return mcp.NewToolResultError("due_date must be in RFC3339 or YYYY-MM-DD format"), nil
				}
			}
			input.DueDate = &t
		}

		mid, err := svc.DB.CreateMilestone(ctx, claims.UID, pid, input)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to add milestone: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"milestone_id": mid,
			"message":      "Milestone added.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// CompleteMilestone handles complete_milestone.
func CompleteMilestone(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		mid, err := req.RequireString("milestone_id")
		if err != nil {
			return mcp.NewToolResultError("milestone_id is required"), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "complete_milestone", map[string]interface{}{
			"project_id":   pid,
			"milestone_id": mid,
		})

		if err := svc.DB.CompleteMilestone(ctx, claims.UID, pid, mid); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to complete milestone: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Milestone completed."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// AddNote handles add_note.
func AddNote(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError("content is required"), nil
		}
		noteType := req.GetString("note_type", "general")

		if err := validation.ValidateNoteType(noteType); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "add_note", map[string]interface{}{
			"project_id": pid,
			"note_type":  noteType,
		})

		nid, err := svc.DB.CreateNote(ctx, claims.UID, pid, &models.CreateNoteInput{
			Content:   content,
			SessionID: sessionID,
			NoteType:  noteType,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save note: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"note_id": nid,
			"message": "Note saved.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// LogDecision handles log_decision.
func LogDecision(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		decision, err := req.RequireString("decision")
		if err != nil {
			return mcp.NewToolResultError("decision is required"), nil
		}
		rationale, err := req.RequireString("rationale")
		if err != nil {
			return mcp.NewToolResultError("rationale is required"), nil
		}
		tags := req.GetStringSlice("tags", nil)

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "log_decision", map[string]interface{}{
			"project_id": pid,
		})

		did, err := svc.DB.CreateDecision(ctx, claims.UID, pid, &models.CreateDecisionInput{
			Decision:  decision,
			Rationale: rationale,
			SessionID: sessionID,
			Tags:      tags,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to log decision: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"decision_id": did,
			"message":     "Decision logged.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// UncompleteMilestone handles uncomplete_milestone.
func UncompleteMilestone(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		mid, err := req.RequireString("milestone_id")
		if err != nil {
			return mcp.NewToolResultError("milestone_id is required"), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "uncomplete_milestone", map[string]interface{}{
			"project_id":   pid,
			"milestone_id": mid,
		})

		if err := svc.DB.UncompleteMilestone(ctx, claims.UID, pid, mid); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to uncomplete milestone: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Milestone reopened."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// AddMilestoneTask handles add_milestone_task.
func AddMilestoneTask(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		mid, err := req.RequireString("milestone_id")
		if err != nil {
			return mcp.NewToolResultError("milestone_id is required"), nil
		}
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError("title is required"), nil
		}
		repoName := req.GetString("repo_name", "")

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "add_milestone_task", map[string]interface{}{
			"project_id":   pid,
			"milestone_id": mid,
		})

		tid, err := svc.DB.AddMilestoneTask(ctx, claims.UID, pid, mid, title, repoName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to add task: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"task_id": tid,
			"message": "Task added.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// CheckMilestoneTask handles check_milestone_task.
func CheckMilestoneTask(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		mid, err := req.RequireString("milestone_id")
		if err != nil {
			return mcp.NewToolResultError("milestone_id is required"), nil
		}
		tid, err := req.RequireString("task_id")
		if err != nil {
			return mcp.NewToolResultError("task_id is required"), nil
		}
		completed := req.GetBool("completed", true)

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "check_milestone_task", map[string]interface{}{
			"project_id":   pid,
			"milestone_id": mid,
			"task_id":      tid,
			"completed":    completed,
		})

		if err := svc.DB.CheckMilestoneTask(ctx, claims.UID, pid, mid, tid, completed); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update task: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Task updated."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// RemoveMilestoneTask handles remove_milestone_task.
func RemoveMilestoneTask(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		mid, err := req.RequireString("milestone_id")
		if err != nil {
			return mcp.NewToolResultError("milestone_id is required"), nil
		}
		tid, err := req.RequireString("task_id")
		if err != nil {
			return mcp.NewToolResultError("task_id is required"), nil
		}

		session, _ := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		sessionID := ""
		if session != nil {
			sessionID = session.ID
		}
		_ = logToolCall(ctx, svc, claims.UID, pid, sessionID, "remove_milestone_task", map[string]interface{}{
			"project_id":   pid,
			"milestone_id": mid,
			"task_id":      tid,
		})

		if err := svc.DB.RemoveMilestoneTask(ctx, claims.UID, pid, mid, tid); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to remove task: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{"message": "Task removed."})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// logToolCall is a helper to log a tool invocation, skipping if sessionID is empty.
func logToolCall(ctx context.Context, svc *Services, uid, pid, sid, toolName string, args map[string]interface{}) error {
	if sid == "" {
		return nil
	}
	return svc.DB.LogToolCall(ctx, uid, pid, sid, &models.ToolCallEntry{
		ToolName: toolName,
		Args:     args,
		Result:   "ok",
		CalledAt: time.Now(),
	})
}
