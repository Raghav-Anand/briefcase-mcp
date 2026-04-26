package handlers

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	internalauth "github.com/raghav-anand/briefcase-internal/auth"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-internal/storage"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

// ---- request builder ----

func req(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: args},
	}
}

// ---- context helpers ----

func authedCtx(uid string) context.Context {
	claims := &internalauth.Claims{UID: uid, Email: uid + "@example.com", Name: "Test User"}
	return context.WithValue(context.Background(), middleware.ClaimsKey, claims)
}

func unauthCtx() context.Context {
	return context.Background()
}

// ---- result helpers ----

func resultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// ---- fake DB ----

// fakeDB implements DBClient with configurable function fields.
// Unset functions return sensible zero-value defaults.
type fakeDB struct {
	upsertUser         func(context.Context, *internalauth.Claims) error
	createProject      func(context.Context, string, *models.CreateProjectInput) (string, error)
	listProjects       func(context.Context, string, bool) ([]models.Project, error)
	getProject         func(context.Context, string, string) (*models.Project, error)
	updateProject      func(context.Context, string, string, map[string]interface{}) error
	archiveProject     func(context.Context, string, string) error
	createSession      func(context.Context, string, string, string) (string, error)
	getActiveSession   func(context.Context, string, string) (*models.Session, error)
	endSession         func(context.Context, string, string, string, string, []string) error
	listSessions       func(context.Context, string, string, int, *time.Time) ([]models.Session, error)
	createMilestone    func(context.Context, string, string, *models.CreateMilestoneInput) (string, error)
	completeMilestone  func(context.Context, string, string, string) error
	createNote         func(context.Context, string, string, *models.CreateNoteInput) (string, error)
	createDecision     func(context.Context, string, string, *models.CreateDecisionInput) (string, error)
	upsertDoc          func(context.Context, string, string, *models.DocInput, *storage.GCSClient) (string, string, error)
	getDoc             func(context.Context, string, string, string, *storage.GCSClient) (*models.RepoDoc, error)
	deleteDoc          func(context.Context, string, string, string, *storage.GCSClient) error
	listDocs           func(context.Context, string, string, *string) ([]models.RepoDocMeta, error)
	listMilestones     func(context.Context, string, string, string) ([]models.Milestone, error)
	listDecisions      func(context.Context, string, string, int) ([]models.Decision, error)
	listNotes          func(context.Context, string, string, string, int) ([]models.Note, error)
	logToolCall        func(context.Context, string, string, string, *models.ToolCallEntry) error
	addRepo              func(context.Context, string, string, *models.CreateRepoInput) (string, error)
	listRepos            func(context.Context, string, string) ([]models.Repo, error)
	updateRepo           func(context.Context, string, string, string, map[string]interface{}) error
	removeRepo           func(context.Context, string, string, string) error
	uncompleteMilestone  func(context.Context, string, string, string) error
	addMilestoneTask     func(context.Context, string, string, string, string, string) (string, error)
	checkMilestoneTask   func(context.Context, string, string, string, string, bool) error
	removeMilestoneTask  func(context.Context, string, string, string, string) error
}

func (f *fakeDB) UpsertUser(ctx context.Context, c *internalauth.Claims) error {
	if f.upsertUser != nil {
		return f.upsertUser(ctx, c)
	}
	return nil
}
func (f *fakeDB) CreateProject(ctx context.Context, uid string, p *models.CreateProjectInput) (string, error) {
	if f.createProject != nil {
		return f.createProject(ctx, uid, p)
	}
	return "proj-1", nil
}
func (f *fakeDB) ListProjects(ctx context.Context, uid string, incArchived bool) ([]models.Project, error) {
	if f.listProjects != nil {
		return f.listProjects(ctx, uid, incArchived)
	}
	return nil, nil
}
func (f *fakeDB) GetProject(ctx context.Context, uid, pid string) (*models.Project, error) {
	if f.getProject != nil {
		return f.getProject(ctx, uid, pid)
	}
	return &models.Project{ID: pid, Name: "Test Project", Status: "active"}, nil
}
func (f *fakeDB) UpdateProject(ctx context.Context, uid, pid string, updates map[string]interface{}) error {
	if f.updateProject != nil {
		return f.updateProject(ctx, uid, pid, updates)
	}
	return nil
}
func (f *fakeDB) ArchiveProject(ctx context.Context, uid, pid string) error {
	if f.archiveProject != nil {
		return f.archiveProject(ctx, uid, pid)
	}
	return nil
}
func (f *fakeDB) CreateSession(ctx context.Context, uid, pid, clientType string) (string, error) {
	if f.createSession != nil {
		return f.createSession(ctx, uid, pid, clientType)
	}
	return "sess-1", nil
}
func (f *fakeDB) GetActiveSession(ctx context.Context, uid, pid string) (*models.Session, error) {
	if f.getActiveSession != nil {
		return f.getActiveSession(ctx, uid, pid)
	}
	return &models.Session{ID: "sess-1", Status: "active"}, nil
}
func (f *fakeDB) EndSession(ctx context.Context, uid, pid, sid, summary string, nextSteps []string) error {
	if f.endSession != nil {
		return f.endSession(ctx, uid, pid, sid, summary, nextSteps)
	}
	return nil
}
func (f *fakeDB) ListSessions(ctx context.Context, uid, pid string, limit int, before *time.Time) ([]models.Session, error) {
	if f.listSessions != nil {
		return f.listSessions(ctx, uid, pid, limit, before)
	}
	return nil, nil
}
func (f *fakeDB) CreateMilestone(ctx context.Context, uid, pid string, m *models.CreateMilestoneInput) (string, error) {
	if f.createMilestone != nil {
		return f.createMilestone(ctx, uid, pid, m)
	}
	return "ms-1", nil
}
func (f *fakeDB) CompleteMilestone(ctx context.Context, uid, pid, mid string) error {
	if f.completeMilestone != nil {
		return f.completeMilestone(ctx, uid, pid, mid)
	}
	return nil
}
func (f *fakeDB) CreateNote(ctx context.Context, uid, pid string, n *models.CreateNoteInput) (string, error) {
	if f.createNote != nil {
		return f.createNote(ctx, uid, pid, n)
	}
	return "note-1", nil
}
func (f *fakeDB) CreateDecision(ctx context.Context, uid, pid string, d *models.CreateDecisionInput) (string, error) {
	if f.createDecision != nil {
		return f.createDecision(ctx, uid, pid, d)
	}
	return "dec-1", nil
}
func (f *fakeDB) UpsertDoc(ctx context.Context, uid, pid string, doc *models.DocInput, gcs *storage.GCSClient) (string, string, error) {
	if f.upsertDoc != nil {
		return f.upsertDoc(ctx, uid, pid, doc, gcs)
	}
	return "doc-1", "inline", nil
}
func (f *fakeDB) GetDoc(ctx context.Context, uid, pid, did string, gcs *storage.GCSClient) (*models.RepoDoc, error) {
	if f.getDoc != nil {
		return f.getDoc(ctx, uid, pid, did, gcs)
	}
	return &models.RepoDoc{ID: did, Title: "Test Doc", Format: "markdown", Content: "# Hello"}, nil
}
func (f *fakeDB) DeleteDoc(ctx context.Context, uid, pid, did string, gcs *storage.GCSClient) error {
	if f.deleteDoc != nil {
		return f.deleteDoc(ctx, uid, pid, did, gcs)
	}
	return nil
}
func (f *fakeDB) ListDocs(ctx context.Context, uid, pid string, docType *string) ([]models.RepoDocMeta, error) {
	if f.listDocs != nil {
		return f.listDocs(ctx, uid, pid, docType)
	}
	return nil, nil
}
func (f *fakeDB) ListMilestones(ctx context.Context, uid, pid, status string) ([]models.Milestone, error) {
	if f.listMilestones != nil {
		return f.listMilestones(ctx, uid, pid, status)
	}
	return nil, nil
}
func (f *fakeDB) ListDecisions(ctx context.Context, uid, pid string, limit int) ([]models.Decision, error) {
	if f.listDecisions != nil {
		return f.listDecisions(ctx, uid, pid, limit)
	}
	return nil, nil
}
func (f *fakeDB) ListNotes(ctx context.Context, uid, pid, noteType string, limit int) ([]models.Note, error) {
	if f.listNotes != nil {
		return f.listNotes(ctx, uid, pid, noteType, limit)
	}
	return nil, nil
}
func (f *fakeDB) LogToolCall(ctx context.Context, uid, pid, sid string, entry *models.ToolCallEntry) error {
	if f.logToolCall != nil {
		return f.logToolCall(ctx, uid, pid, sid, entry)
	}
	return nil
}
func (f *fakeDB) AddRepo(ctx context.Context, uid, pid string, r *models.CreateRepoInput) (string, error) {
	if f.addRepo != nil {
		return f.addRepo(ctx, uid, pid, r)
	}
	return "repo-1", nil
}
func (f *fakeDB) ListRepos(ctx context.Context, uid, pid string) ([]models.Repo, error) {
	if f.listRepos != nil {
		return f.listRepos(ctx, uid, pid)
	}
	return nil, nil
}
func (f *fakeDB) UpdateRepo(ctx context.Context, uid, pid, rid string, updates map[string]interface{}) error {
	if f.updateRepo != nil {
		return f.updateRepo(ctx, uid, pid, rid, updates)
	}
	return nil
}
func (f *fakeDB) RemoveRepo(ctx context.Context, uid, pid, rid string) error {
	if f.removeRepo != nil {
		return f.removeRepo(ctx, uid, pid, rid)
	}
	return nil
}

func (f *fakeDB) UncompleteMilestone(ctx context.Context, uid, pid, mid string) error {
	if f.uncompleteMilestone != nil {
		return f.uncompleteMilestone(ctx, uid, pid, mid)
	}
	return nil
}
func (f *fakeDB) AddMilestoneTask(ctx context.Context, uid, pid, mid, title, repoName string) (string, error) {
	if f.addMilestoneTask != nil {
		return f.addMilestoneTask(ctx, uid, pid, mid, title, repoName)
	}
	return "task-1", nil
}
func (f *fakeDB) CheckMilestoneTask(ctx context.Context, uid, pid, mid, taskID string, completed bool) error {
	if f.checkMilestoneTask != nil {
		return f.checkMilestoneTask(ctx, uid, pid, mid, taskID, completed)
	}
	return nil
}
func (f *fakeDB) RemoveMilestoneTask(ctx context.Context, uid, pid, mid, taskID string) error {
	if f.removeMilestoneTask != nil {
		return f.removeMilestoneTask(ctx, uid, pid, mid, taskID)
	}
	return nil
}

// svc builds a Services with the given fakeDB.
func svc(db *fakeDB) *Services {
	return &Services{DB: db}
}
