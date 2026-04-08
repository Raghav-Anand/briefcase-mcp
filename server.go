package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-mcp/handlers"
)

const systemPrompt = `You are connected to the Briefcase MCP server — a project progress tracker.

Follow these rules:

1. At the START of any conversation where the user wants to work on a project:
   - Call list_projects() to see available projects
   - Ask which project to work on (or create a new one with create_project())
   - Call start_session(project_id) to load full context
   - Share the context summary with the user

2. DURING the conversation:
   - Call add_note() for important observations, findings, or progress
   - Call log_decision() when a technical or design decision is made
   - Call add_milestone() when new goals are identified
   - Call complete_milestone() when goals are achieved
   - Call upload_doc() when generating documentation, API references, or architecture diagrams

3. At the END of the conversation (when the user is wrapping up):
   - Call end_session() with a summary of what was accomplished, next steps, and any decisions made
   - Confirm to the user that their progress has been saved

Always be transparent about what you're logging. The user should know their progress is being tracked.`

// newMCPServer creates and fully configures the MCP server with all tools and prompts.
func newMCPServer(svc *handlers.Services) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"Briefcase",
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithPromptCapabilities(false),
		mcpserver.WithRecovery(),
		mcpserver.WithInstructions("If the Briefcase MCP server requires authentication, call the authenticate tool to get the OAuth URL, then immediately open it in the user's browser using the Bash tool: run `open <url>` on macOS or `xdg-open <url>` on Linux. Do not ask the user to open it manually."),
	)

	// --- System prompt ---
	s.AddPrompt(
		mcp.NewPrompt("briefcase_instructions",
			mcp.WithPromptDescription("Instructions for using the Briefcase project tracker"),
		),
		func(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return mcp.NewGetPromptResult(
				"Briefcase project tracker usage instructions",
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(systemPrompt)),
				},
			), nil
		},
	)

	// --- Session management ---
	s.AddTool(
		mcp.NewTool("start_session",
			mcp.WithDescription("Load full project context to start a working session. Call this at the beginning of every project conversation."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID to start a session for")),
		),
		handlers.StartSession(svc),
	)

	s.AddTool(
		mcp.NewTool("end_session",
			mcp.WithDescription("Save session progress and mark it complete. Call this when the user is done working."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("summary", mcp.Required(), mcp.Description("What was accomplished in this session")),
			mcp.WithArray("next_steps", mcp.Description("What to do in the next session"), mcp.WithStringItems()),
			mcp.WithArray("decisions", mcp.Description("Key decisions made during this session (will be logged)"), mcp.WithStringItems()),
		),
		handlers.EndSession(svc),
	)

	// --- Project CRUD ---
	s.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Create a new project to track."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Project name")),
			mcp.WithString("description", mcp.Required(), mcp.Description("Brief description of what this project is")),
			mcp.WithString("repo_url", mcp.Description("Optional GitHub/GitLab repository URL")),
			mcp.WithArray("tech_stack", mcp.Description("Technologies used, e.g. ['Go', 'React', 'Firestore']"), mcp.WithStringItems()),
		),
		handlers.CreateProject(svc),
	)

	s.AddTool(
		mcp.NewTool("list_projects",
			mcp.WithDescription("List all your projects."),
			mcp.WithBoolean("include_archived", mcp.Description("Include archived projects (default: false)")),
		),
		handlers.ListProjects(svc),
	)

	s.AddTool(
		mcp.NewTool("get_project",
			mcp.WithDescription("Get full details for a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
		),
		handlers.GetProject(svc),
	)

	s.AddTool(
		mcp.NewTool("update_project",
			mcp.WithDescription("Update project fields. Only provided fields are changed."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("name", mcp.Description("New project name")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("status", mcp.Description("New status: active | paused | completed | archived")),
			mcp.WithString("repo_url", mcp.Description("New repository URL")),
			mcp.WithArray("tech_stack", mcp.Description("Updated tech stack"), mcp.WithStringItems()),
		),
		handlers.UpdateProject(svc),
	)

	s.AddTool(
		mcp.NewTool("archive_project",
			mcp.WithDescription("Archive a project (hides it from default listing)."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
		),
		handlers.ArchiveProject(svc),
	)

	// --- Progress tracking ---
	s.AddTool(
		mcp.NewTool("add_milestone",
			mcp.WithDescription("Add a milestone (goal) to a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Milestone title, e.g. 'Deploy to production'")),
			mcp.WithString("description", mcp.Description("Optional detail about the milestone")),
			mcp.WithString("due_date", mcp.Description("Optional target date in RFC3339 or YYYY-MM-DD format")),
		),
		handlers.AddMilestone(svc),
	)

	s.AddTool(
		mcp.NewTool("complete_milestone",
			mcp.WithDescription("Mark a milestone as completed."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("milestone_id", mcp.Required(), mcp.Description("The milestone ID to complete")),
		),
		handlers.CompleteMilestone(svc),
	)

	s.AddTool(
		mcp.NewTool("add_note",
			mcp.WithDescription("Save a note about the project. Use for observations, bugs found, ideas, or todos."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Note content (markdown supported)")),
			mcp.WithString("note_type", mcp.Description("Type: general | bug | idea | todo (default: general)")),
		),
		handlers.AddNote(svc),
	)

	s.AddTool(
		mcp.NewTool("log_decision",
			mcp.WithDescription("Log a technical or design decision with its rationale. Decisions are the most valuable long-term artifact."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("decision", mcp.Required(), mcp.Description("The decision made, e.g. 'Chose Firestore over Cloud SQL'")),
			mcp.WithString("rationale", mcp.Required(), mcp.Description("Why this decision was made")),
			mcp.WithArray("tags", mcp.Description("Optional tags for categorization, e.g. ['database', 'infrastructure']"), mcp.WithStringItems()),
		),
		handlers.LogDecision(svc),
	)

	// --- Documentation ---
	s.AddTool(
		mcp.NewTool("upload_doc",
			mcp.WithDescription("Upload or update a project document (API reference, architecture, README, etc.)."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Document title, e.g. 'API Reference'")),
			mcp.WithString("doc_type", mcp.Required(), mcp.Description("Type: api_docs | architecture | readme | custom")),
			mcp.WithString("format", mcp.Required(), mcp.Description("Format: markdown | mermaid")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Full document content")),
		),
		handlers.UploadDoc(svc),
	)

	s.AddTool(
		mcp.NewTool("list_docs",
			mcp.WithDescription("List documents for a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("doc_type", mcp.Description("Filter by type: api_docs | architecture | readme | custom")),
		),
		handlers.ListDocs(svc),
	)

	s.AddTool(
		mcp.NewTool("get_doc",
			mcp.WithDescription("Get the full content of a document."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("doc_id", mcp.Required(), mcp.Description("The document ID (from list_docs)")),
		),
		handlers.GetDoc(svc),
	)

	return s
}
