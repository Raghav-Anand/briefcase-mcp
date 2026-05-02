package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

// StartSession handles start_session: assembles and returns full project context.
func StartSession(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		// Upsert user on every session start (keeps profile fields fresh).
		if upsertErr := svc.DB.UpsertUser(ctx, claims); upsertErr != nil {
			// Non-fatal: log but continue.
			_ = upsertErr
		}

		// Get project.
		project, err := svc.DB.GetProject(ctx, claims.UID, pid)
		if err != nil {
			return mcp.NewToolResultError("Project not found. Use list_projects() to see your projects."), nil
		}

		// Create new session (auto-closes any existing active session).
		clientType := clientTypeFromContext(ctx)
		sid, err := svc.DB.CreateSession(ctx, claims.UID, pid, clientType)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to start session: %v", err)), nil
		}

		// Log this tool call to the new session.
		_ = svc.DB.LogToolCall(ctx, claims.UID, pid, sid, &models.ToolCallEntry{
			ToolName: "start_session",
			Args:     map[string]interface{}{"project_id": pid},
			Result:   "ok",
			CalledAt: time.Now(),
		})

		// Fetch context components in parallel where possible.
		// Last completed session (for summary + next_steps).
		var lastSession *models.Session
		sessions, err := svc.DB.ListSessions(ctx, claims.UID, pid, 2, nil)
		if err == nil {
			for i := range sessions {
				if sessions[i].ID != sid && sessions[i].Status != "active" {
					lastSession = &sessions[i]
					break
				}
			}
		}

		// Open milestones.
		openMilestones, _ := svc.DB.ListMilestones(ctx, claims.UID, pid, "open")

		// Recent decisions (last 10).
		recentDecisions, _ := svc.DB.ListDecisions(ctx, claims.UID, pid, 10)

		// Recent notes (last 5).
		recentNotes, _ := svc.DB.ListNotes(ctx, claims.UID, pid, "", 5)

		// Repo docs metadata.
		repoDocs, _ := svc.DB.ListDocs(ctx, claims.UID, pid, nil)

		// Assemble response.
		type lastSessionInfo struct {
			Summary   string     `json:"summary,omitempty"`
			NextSteps []string   `json:"next_steps,omitempty"`
			EndedAt   *time.Time `json:"ended_at,omitempty"`
		}
		type milestoneInfo struct {
			ID      string     `json:"id"`
			Seq     int        `json:"seq"`
			Title   string     `json:"title"`
			DueDate *time.Time `json:"due_date,omitempty"`
		}
		type decisionInfo struct {
			Decision  string    `json:"decision"`
			Rationale string    `json:"rationale,omitempty"`
			CreatedAt time.Time `json:"created_at"`
		}
		type noteInfo struct {
			Content   string    `json:"content"`
			NoteType  string    `json:"note_type"`
			CreatedAt time.Time `json:"created_at"`
		}
		type docInfo struct {
			Title     string    `json:"title"`
			DocType   string    `json:"doc_type"`
			Format    string    `json:"format"`
			Summary   string    `json:"summary,omitempty"`
			Headings  []string  `json:"headings,omitempty"`
			UpdatedAt time.Time `json:"updated_at"`
		}

		response := struct {
			Project         interface{}    `json:"project"`
			LastSession     *lastSessionInfo `json:"last_session,omitempty"`
			OpenMilestones  []milestoneInfo  `json:"open_milestones"`
			RecentDecisions []decisionInfo   `json:"recent_decisions"`
			RecentNotes     []noteInfo       `json:"recent_notes"`
			RepoDocs        []docInfo        `json:"repo_docs"`
			Message         string           `json:"message"`
		}{
			Project: map[string]interface{}{
				"name":        project.Name,
				"description": project.Description,
				"status":      project.Status,
				"repo_url":    project.RepoURL,
				"tech_stack":  project.TechStack,
			},
			OpenMilestones:  make([]milestoneInfo, 0, len(openMilestones)),
			RecentDecisions: make([]decisionInfo, 0, len(recentDecisions)),
			RecentNotes:     make([]noteInfo, 0, len(recentNotes)),
			RepoDocs:        make([]docInfo, 0, len(repoDocs)),
			Message:         "Session started. Here's your full project context.",
		}

		if lastSession != nil {
			response.LastSession = &lastSessionInfo{
				Summary:   lastSession.Summary,
				NextSteps: lastSession.NextSteps,
				EndedAt:   lastSession.EndedAt,
			}
		}
		for _, m := range openMilestones {
			response.OpenMilestones = append(response.OpenMilestones, milestoneInfo{
				ID:      m.ID,
				Seq:     m.Seq,
				Title:   m.Title,
				DueDate: m.DueDate,
			})
		}
		for _, d := range recentDecisions {
			response.RecentDecisions = append(response.RecentDecisions, decisionInfo{
				Decision:  d.Decision,
				Rationale: d.Rationale,
				CreatedAt: d.CreatedAt,
			})
		}
		for _, n := range recentNotes {
			response.RecentNotes = append(response.RecentNotes, noteInfo{
				Content:   n.Content,
				NoteType:  n.NoteType,
				CreatedAt: n.CreatedAt,
			})
		}
		for _, doc := range repoDocs {
			response.RepoDocs = append(response.RepoDocs, docInfo{
				Title:     doc.Title,
				DocType:   doc.DocType,
				Format:    doc.Format,
				Summary:   doc.Summary,
				Headings:  doc.Headings,
				UpdatedAt: doc.UpdatedAt,
			})
		}

		out, _ := json.MarshalIndent(response, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

// EndSession handles end_session: writes summary and marks session complete.
func EndSession(svc *Services) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := middleware.ClaimsFromContext(ctx)
		if claims == nil {
			return mcp.NewToolResultError("authentication required"), nil
		}

		pid, err := req.RequireString("project_id")
		if err != nil {
			return mcp.NewToolResultError("project_id is required"), nil
		}
		summary, err := req.RequireString("summary")
		if err != nil {
			return mcp.NewToolResultError("summary is required"), nil
		}
		nextSteps := req.GetStringSlice("next_steps", nil)
		decisions := req.GetStringSlice("decisions", nil)

		// Find active session.
		session, err := svc.DB.GetActiveSession(ctx, claims.UID, pid)
		if err != nil || session == nil {
			return mcp.NewToolResultError("No active session found for this project. Use start_session() first."), nil
		}

		// Log tool call before closing.
		_ = svc.DB.LogToolCall(ctx, claims.UID, pid, session.ID, &models.ToolCallEntry{
			ToolName: "end_session",
			Args:     map[string]interface{}{"project_id": pid},
			Result:   "ok",
			CalledAt: time.Now(),
		})

		// Create any decisions provided inline with the session end.
		for _, d := range decisions {
			if d != "" {
				_, _ = svc.DB.CreateDecision(ctx, claims.UID, pid, &models.CreateDecisionInput{
					Decision:  d,
					SessionID: session.ID,
				})
			}
		}

		// Mark session completed.
		if err := svc.DB.EndSession(ctx, claims.UID, pid, session.ID, summary, nextSteps); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to end session: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"message": "Session saved. Next time you start, I'll have this context ready.",
		})
		return mcp.NewToolResultText(string(out)), nil
	}
}

// clientTypeFromContext extracts client type info from context (best-effort).
func clientTypeFromContext(_ context.Context) string {
	// Claude clients don't currently expose type in context. Default to unknown.
	return "unknown"
}
