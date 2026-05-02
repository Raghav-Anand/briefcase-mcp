package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/raghav-anand/briefcase-internal/models"
)

func TestStartSession(t *testing.T) {
	t.Run("returns full context on success", func(t *testing.T) {
		db := &fakeDB{
			getProject: func(_ context.Context, _, pid string) (*models.Project, error) {
				return &models.Project{
					ID:          pid,
					Name:        "My App",
					Description: "Cool project",
					Status:      "active",
				}, nil
			},
			createSession: func(_ context.Context, _, _, _ string) (string, error) {
				return "sess-abc", nil
			},
			listSessions: func(_ context.Context, _, _ string, _ int, _ *time.Time) ([]models.Session, error) {
				return []models.Session{
					{ID: "sess-old", Status: "completed", Summary: "Last time we did X"},
				}, nil
			},
		}

		result, err := StartSession(svc(db))(authedCtx("user-1"), req(map[string]any{
			"project_id": "proj-1",
		}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected success, got error: %s", resultText(result))
		}

		text := resultText(result)
		var body map[string]any
		if err := json.Unmarshal([]byte(text), &body); err != nil {
			t.Fatalf("result is not valid JSON: %v\n%s", err, text)
		}
		if body["message"] != "Session started. Here's your full project context." {
			t.Errorf("unexpected message: %v", body["message"])
		}
		if _, ok := body["project"]; !ok {
			t.Error("missing 'project' field in response")
		}
	})

	t.Run("includes summary and headings in repo_docs", func(t *testing.T) {
		db := &fakeDB{
			createSession: func(_ context.Context, _, _, _ string) (string, error) {
				return "sess-new", nil
			},
			listSessions: func(_ context.Context, _, _ string, _ int, _ *time.Time) ([]models.Session, error) {
				return []models.Session{{ID: "sess-old", Status: "completed"}}, nil
			},
			listDocs: func(_ context.Context, _, _ string, _ *string) ([]models.RepoDocMeta, error) {
				return []models.RepoDocMeta{
					{
						ID:      "d1",
						Title:   "Architecture",
						DocType: "architecture",
						Format:  "markdown",
						Summary: "Service topology and data flow.",
						Headings: []string{"Overview", "Services", "Data Flow"},
					},
				}, nil
			},
		}

		result, err := StartSession(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "proj-1",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}
		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		docs, ok := body["repo_docs"].([]any)
		if !ok || len(docs) != 1 {
			t.Fatalf("expected 1 repo_doc, got: %v", body["repo_docs"])
		}
		doc := docs[0].(map[string]any)
		if doc["summary"] != "Service topology and data flow." {
			t.Errorf("expected summary in repo_docs, got %v", doc["summary"])
		}
		headings, ok := doc["headings"].([]any)
		if !ok || len(headings) != 3 {
			t.Errorf("expected 3 headings in repo_docs, got %v", doc["headings"])
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		result, err := StartSession(svc(&fakeDB{}))(unauthCtx(), req(map[string]any{
			"project_id": "proj-1",
		}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result for unauthenticated request")
		}
	})

	t.Run("requires project_id", func(t *testing.T) {
		result, err := StartSession(svc(&fakeDB{}))(authedCtx("user-1"), req(map[string]any{}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for missing project_id")
		}
	})

	t.Run("returns error when project not found", func(t *testing.T) {
		db := &fakeDB{
			getProject: func(_ context.Context, _, _ string) (*models.Project, error) {
				return nil, errors.New("not found")
			},
		}
		result, err := StartSession(svc(db))(authedCtx("user-1"), req(map[string]any{
			"project_id": "missing",
		}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result when project doesn't exist")
		}
	})
}

func TestEndSession(t *testing.T) {
	t.Run("marks session completed", func(t *testing.T) {
		var capturedSummary string
		db := &fakeDB{
			getActiveSession: func(_ context.Context, _, _ string) (*models.Session, error) {
				return &models.Session{ID: "sess-1", Status: "active"}, nil
			},
			endSession: func(_ context.Context, _, _, _, summary string, _ []string) error {
				capturedSummary = summary
				return nil
			},
		}

		result, err := EndSession(svc(db))(authedCtx("user-1"), req(map[string]any{
			"project_id": "proj-1",
			"summary":    "We shipped the feature",
		}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected success, got error: %s", resultText(result))
		}
		if capturedSummary != "We shipped the feature" {
			t.Errorf("expected summary 'We shipped the feature', got %q", capturedSummary)
		}
	})

	t.Run("returns error when no active session", func(t *testing.T) {
		db := &fakeDB{
			getActiveSession: func(_ context.Context, _, _ string) (*models.Session, error) {
				return nil, nil // no active session
			},
		}

		result, err := EndSession(svc(db))(authedCtx("user-1"), req(map[string]any{
			"project_id": "proj-1",
			"summary":    "done",
		}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error when no active session")
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		result, _ := EndSession(svc(&fakeDB{}))(unauthCtx(), req(map[string]any{
			"project_id": "proj-1",
			"summary":    "done",
		}))
		if !result.IsError {
			t.Error("expected error for unauthenticated request")
		}
	})
}
