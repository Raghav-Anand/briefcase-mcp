package main

import (
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/raghav-anand/briefcase-mcp/handlers"
)

const systemPrompt = `You are connected to Briefcase — a project progress tracker. Maintain persistent context across sessions.

## Session start
Call list_projects(), then start_session(project_id). The response contains: project metadata, last_session (summary + next_steps — start here), open_milestones (IDs + titles), recent_decisions, recent_notes, and repo_docs (title/type/format only — no content). Summarise for the user: what was last done, open milestones, what's next.

On a new project (no last_session): propose milestones with pre-populated tasks, link repos via add_repo(), create an architecture doc if the design is already clear.

## During the session
- **Milestones**: add_milestone() for new goals; pre-populate tasks at creation. check_milestone_task() automatically as each task is completed during the session — do not wait to be asked. Never call complete_milestone() — that is the human's decision.
- **Notes**: add_note() for bugs, ideas, TODOs, and non-obvious findings. Don't duplicate decisions or milestones.
- **Decisions**: log_decision() for architectural choices — library/tool selection, API design, DB schema, infra, auth. Not minor implementation details.
- **Docs**: upload_doc() when generating or substantially revising architecture diagrams, API specs, READMEs, or design docs. Before uploading, check repo_docs from start_session or call list_docs() to get a doc_id — pass it to update in-place rather than creating a duplicate.
- **Repos**: add_repo() when a repo is first mentioned; update_repo() on changes.

Token rule: repo_docs is metadata only — never call get_doc() unless you need the full content.

## Session end
Call end_session() with a 2–4 sentence summary, specific next_steps, and any inline decisions not separately logged. Confirm to the user that progress is saved.

Never skip start_session() or end_session() — lost sessions lose context permanently.`

// newMCPServer creates and fully configures the MCP server with all tools and prompts.
func newMCPServer(svc *handlers.Services) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"Briefcase",
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithPromptCapabilities(false),
		mcpserver.WithRecovery(),
		mcpserver.WithInstructions(systemPrompt),
	)

	// --- Session management ---
	s.AddTool(
		mcp.NewTool("start_session",
			mcp.WithDescription("Load full project context to start a working session. Returns: project metadata, last session summary + next_steps, open milestone IDs/titles, last 10 decisions, last 5 notes, and doc metadata (titles only — no content). Call this before any project work."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID to start a session for")),
		),
		handlers.StartSession(svc),
	)

	s.AddTool(
		mcp.NewTool("end_session",
			mcp.WithDescription("Save session progress and mark it complete. Call this whenever the user wraps up. The decisions array auto-logs each entry via log_decision — use it for quick decisions that weren't separately logged during the session."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("summary", mcp.Required(), mcp.Description("2-4 sentences describing what was accomplished this session")),
			mcp.WithArray("next_steps", mcp.Description("Specific, actionable items for the next session — not generic placeholders"), mcp.WithStringItems()),
			mcp.WithArray("decisions", mcp.Description("Quick decisions made inline this session that weren't separately logged (each will be auto-logged)"), mcp.WithStringItems()),
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
			mcp.WithDescription("Add a milestone (goal) to a project. Pre-populate tasks at creation time to avoid extra round-trips. Each task can be linked to a specific repo."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Milestone title, e.g. 'Deploy to production'")),
			mcp.WithString("description", mcp.Description("Optional detail about the milestone scope or acceptance criteria")),
			mcp.WithString("due_date", mcp.Description("Optional target date in RFC3339 or YYYY-MM-DD format")),
			mcp.WithArray("tasks", mcp.Description("Pre-populate the milestone checklist. Each item: {\"title\": string (required), \"repo_name\": string (optional, must match a linked repo name)}. Prefer pre-populating over separate add_milestone_task calls.")),
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
		mcp.NewTool("uncomplete_milestone",
			mcp.WithDescription("Re-open a completed milestone."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("milestone_id", mcp.Required(), mcp.Description("The milestone ID to reopen")),
		),
		handlers.UncompleteMilestone(svc),
	)

	s.AddTool(
		mcp.NewTool("add_milestone_task",
			mcp.WithDescription("Add a task (checklist item) to a milestone."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("milestone_id", mcp.Required(), mcp.Description("The milestone ID")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Task title")),
			mcp.WithString("repo_name", mcp.Description("Optional repo name this task belongs to (must match a linked repo)")),
		),
		handlers.AddMilestoneTask(svc),
	)

	s.AddTool(
		mcp.NewTool("check_milestone_task",
			mcp.WithDescription("Mark a milestone task as completed or incomplete."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("milestone_id", mcp.Required(), mcp.Description("The milestone ID")),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("The task ID")),
			mcp.WithBoolean("completed", mcp.Required(), mcp.Description("true to complete the task, false to uncheck it")),
		),
		handlers.CheckMilestoneTask(svc),
	)

	s.AddTool(
		mcp.NewTool("remove_milestone_task",
			mcp.WithDescription("Remove a task from a milestone."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("milestone_id", mcp.Required(), mcp.Description("The milestone ID")),
			mcp.WithString("task_id", mcp.Required(), mcp.Description("The task ID to remove")),
		),
		handlers.RemoveMilestoneTask(svc),
	)

	s.AddTool(
		mcp.NewTool("add_note",
			mcp.WithDescription("Save a note about the project. Use for bugs found, ideas, TODOs, and non-obvious findings. Don't duplicate things already captured as decisions or milestones."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Note content (markdown supported). Be specific — vague notes have no value later.")),
			mcp.WithString("note_type", mcp.Description("bug: something broken or wrong | idea: optimisation or future improvement | todo: action item without its own milestone | general: non-obvious finding or observation (default: general)")),
		),
		handlers.AddNote(svc),
	)

	s.AddTool(
		mcp.NewTool("log_decision",
			mcp.WithDescription("Log a technical or design decision with its rationale. These are permanent records — the most valuable long-term artifact in the system. Log when: a library/tool/framework is chosen, an API contract is designed, a DB schema is decided, a deployment or infra approach is settled, a security/auth approach is chosen. Don't log minor implementation details."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("decision", mcp.Required(), mcp.Description("Specific decision made, e.g. 'Chose Firestore over Cloud SQL for the sessions collection'")),
			mcp.WithString("rationale", mcp.Required(), mcp.Description("Why — constraints, tradeoffs, or context that drove the choice")),
			mcp.WithArray("tags", mcp.Description("Optional tags for categorization, e.g. ['database', 'infrastructure']"), mcp.WithStringItems()),
		),
		handlers.LogDecision(svc),
	)

	// --- Documentation ---
	s.AddTool(
		mcp.NewTool("upload_doc",
			mcp.WithDescription("Upload or update a project document. IMPORTANT: before creating a new doc, check repo_docs from start_session or call list_docs() — if a doc of the same type exists, get its doc_id and pass it here to update in-place rather than creating a duplicate. Docs under 500 KB are stored inline; larger docs go to Cloud Storage automatically."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("doc_id", mcp.Description("ID of an existing doc to update (from list_docs). Omit only when creating a brand new doc.")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Document title, e.g. 'API Reference' or 'System Architecture'")),
			mcp.WithString("doc_type", mcp.Required(), mcp.Description("api_docs: API specs/references | architecture: system/service diagrams | readme: onboarding/overview | custom: design docs, ADRs, runbooks")),
			mcp.WithString("format", mcp.Required(), mcp.Description("markdown: prose, tables, code blocks | mermaid: diagrams (architecture, sequence, ERD)")),
			mcp.WithString("summary", mcp.Description("One-sentence description of the doc's purpose, shown in session context without loading the full content.")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Full document content")),
		),
		handlers.UploadDoc(svc),
	)

	s.AddTool(
		mcp.NewTool("list_docs",
			mcp.WithDescription("List documents for a project — returns metadata only (id, title, doc_type, format, version, updated_at), no content. Use this to get doc_ids before updating an existing doc. start_session already returns doc metadata, so only call this mid-session when you need fresh IDs."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("doc_type", mcp.Description("Filter by type: api_docs | architecture | readme | custom")),
		),
		handlers.ListDocs(svc),
	)

	s.AddTool(
		mcp.NewTool("get_doc",
			mcp.WithDescription("Get the full content of a document. May fetch from Cloud Storage for large docs — only call when you actually need to read the content. Use list_docs to check what exists first."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("doc_id", mcp.Required(), mcp.Description("The document ID (from list_docs)")),
		),
		handlers.GetDoc(svc),
	)

	s.AddTool(
		mcp.NewTool("delete_doc",
			mcp.WithDescription("Delete a project document permanently. Also removes the file from Cloud Storage if applicable."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("doc_id", mcp.Required(), mcp.Description("The document ID (from list_docs)")),
		),
		handlers.DeleteDoc(svc),
	)

	// --- Repos ---
	s.AddTool(
		mcp.NewTool("add_repo",
			mcp.WithDescription("Link a repository to a project. Call this the first time a repo is mentioned. The repo name is used to link milestone tasks to specific repos, so use a consistent short name (e.g. 'briefcase-api', not the full URL)."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Short repo name, e.g. 'briefcase-api'. This is referenced by milestone tasks via repo_name — keep it consistent.")),
			mcp.WithString("url", mcp.Required(), mcp.Description("Repository URL, e.g. 'https://github.com/org/repo'")),
			mcp.WithString("description", mcp.Description("The repo's role in the project, e.g. 'REST API for the web dashboard'")),
			mcp.WithString("language", mcp.Description("Primary language, e.g. 'Go', 'TypeScript'")),
		),
		handlers.AddRepo(svc),
	)

	s.AddTool(
		mcp.NewTool("list_repos",
			mcp.WithDescription("List all repositories linked to a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
		),
		handlers.ListRepos(svc),
	)

	s.AddTool(
		mcp.NewTool("update_repo",
			mcp.WithDescription("Update a linked repository. Only provided fields are changed."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("repo_id", mcp.Required(), mcp.Description("The repo ID (from list_repos)")),
			mcp.WithString("name", mcp.Description("New repository name")),
			mcp.WithString("url", mcp.Description("New repository URL")),
			mcp.WithString("description", mcp.Description("New description")),
			mcp.WithString("language", mcp.Description("New primary language")),
		),
		handlers.UpdateRepo(svc),
	)

	s.AddTool(
		mcp.NewTool("remove_repo",
			mcp.WithDescription("Remove a linked repository from a project."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("The project ID")),
			mcp.WithString("repo_id", mcp.Required(), mcp.Description("The repo ID to remove")),
		),
		handlers.RemoveRepo(svc),
	)

	return s
}
