package handlers

import (
	"context"
	"time"

	"github.com/raghav-anand/briefcase-internal/auth"
	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-internal/storage"
)

// DBClient is the interface satisfied by *db.Client.
// Defined here to enable test doubles without pulling Firestore into test binaries.
type DBClient interface {
	UpsertUser(ctx context.Context, claims *auth.Claims) error
	CreateProject(ctx context.Context, uid string, p *models.CreateProjectInput) (string, error)
	ListProjects(ctx context.Context, uid string, includeArchived bool) ([]models.Project, error)
	GetProject(ctx context.Context, uid, pid string) (*models.Project, error)
	UpdateProject(ctx context.Context, uid, pid string, updates map[string]interface{}) error
	ArchiveProject(ctx context.Context, uid, pid string) error
	CreateSession(ctx context.Context, uid, pid string, clientType string) (string, error)
	GetActiveSession(ctx context.Context, uid, pid string) (*models.Session, error)
	EndSession(ctx context.Context, uid, pid, sid string, summary string, nextSteps []string) error
	ListSessions(ctx context.Context, uid, pid string, limit int, before *time.Time) ([]models.Session, error)
	CreateMilestone(ctx context.Context, uid, pid string, m *models.CreateMilestoneInput) (string, error)
	CompleteMilestone(ctx context.Context, uid, pid, mid string) error
	CreateNote(ctx context.Context, uid, pid string, n *models.CreateNoteInput) (string, error)
	CreateDecision(ctx context.Context, uid, pid string, d *models.CreateDecisionInput) (string, error)
	UpsertDoc(ctx context.Context, uid, pid string, doc *models.DocInput, gcs *storage.GCSClient) (string, string, error)
	GetDoc(ctx context.Context, uid, pid, did string, gcs *storage.GCSClient) (*models.RepoDoc, error)
	ListMilestones(ctx context.Context, uid, pid string, status string) ([]models.Milestone, error)
	ListDecisions(ctx context.Context, uid, pid string, limit int) ([]models.Decision, error)
	ListNotes(ctx context.Context, uid, pid string, noteType string, limit int) ([]models.Note, error)
	ListDocs(ctx context.Context, uid, pid string, docType *string) ([]models.RepoDocMeta, error)
	LogToolCall(ctx context.Context, uid, pid, sid string, entry *models.ToolCallEntry) error
	AddRepo(ctx context.Context, uid, pid string, r *models.CreateRepoInput) (string, error)
	ListRepos(ctx context.Context, uid, pid string) ([]models.Repo, error)
	UpdateRepo(ctx context.Context, uid, pid, rid string, updates map[string]interface{}) error
	RemoveRepo(ctx context.Context, uid, pid, rid string) error
}
