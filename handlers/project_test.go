package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/raghav-anand/briefcase-internal/models"
)

func TestCreateProject(t *testing.T) {
	t.Run("creates project and returns id", func(t *testing.T) {
		var captured *models.CreateProjectInput
		db := &fakeDB{
			createProject: func(_ context.Context, _ string, p *models.CreateProjectInput) (string, error) {
				captured = p
				return "proj-xyz", nil
			},
		}

		result, err := CreateProject(svc(db))(authedCtx("user-1"), req(map[string]any{
			"name":        "My API",
			"description": "A REST API",
			"tech_stack":  []any{"Go", "Firestore"},
		}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["project_id"] != "proj-xyz" {
			t.Errorf("expected project_id 'proj-xyz', got %v", body["project_id"])
		}
		if captured.Name != "My API" {
			t.Errorf("expected name 'My API', got %q", captured.Name)
		}
	})

	t.Run("requires name and description", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			args map[string]any
		}{
			{"missing name", map[string]any{"description": "desc"}},
			{"missing description", map[string]any{"name": "proj"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result, _ := CreateProject(svc(&fakeDB{}))(authedCtx("u"), req(tc.args))
				if !result.IsError {
					t.Error("expected error for missing required field")
				}
			})
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			createProject: func(_ context.Context, _ string, _ *models.CreateProjectInput) (string, error) {
				return "", errors.New("firestore unavailable")
			},
		}
		result, _ := CreateProject(svc(db))(authedCtx("u"), req(map[string]any{
			"name": "x", "description": "y",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}

func TestListProjects(t *testing.T) {
	t.Run("returns project list", func(t *testing.T) {
		db := &fakeDB{
			listProjects: func(_ context.Context, _ string, _ bool) ([]models.Project, error) {
				return []models.Project{
					{ID: "p1", Name: "App One", Status: "active"},
					{ID: "p2", Name: "App Two", Status: "paused"},
				}, nil
			},
		}

		result, err := ListProjects(svc(db))(authedCtx("user-1"), req(map[string]any{}))

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		projects, ok := body["projects"].([]any)
		if !ok || len(projects) != 2 {
			t.Errorf("expected 2 projects, got: %v", body["projects"])
		}
	})

	t.Run("passes include_archived flag to db", func(t *testing.T) {
		var capturedFlag bool
		db := &fakeDB{
			listProjects: func(_ context.Context, _ string, incArchived bool) ([]models.Project, error) {
				capturedFlag = incArchived
				return nil, nil
			},
		}
		ListProjects(svc(db))(authedCtx("u"), req(map[string]any{"include_archived": true}))
		if !capturedFlag {
			t.Error("expected include_archived=true to be passed to db")
		}
	})
}

func TestUpdateProject(t *testing.T) {
	t.Run("updates only provided fields", func(t *testing.T) {
		var capturedUpdates map[string]interface{}
		db := &fakeDB{
			updateProject: func(_ context.Context, _, _ string, updates map[string]interface{}) error {
				capturedUpdates = updates
				return nil
			},
		}

		result, _ := UpdateProject(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id":  "p1",
			"description": "new desc",
		}))

		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}
		if capturedUpdates["description"] != "new desc" {
			t.Errorf("expected description update, got: %v", capturedUpdates)
		}
		if _, nameSet := capturedUpdates["name"]; nameSet {
			t.Error("name should not be in updates when not provided")
		}
	})

	t.Run("rejects invalid status", func(t *testing.T) {
		result, _ := UpdateProject(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"status":     "invalid-status",
		}))
		if !result.IsError {
			t.Error("expected error for invalid status")
		}
	})

	t.Run("returns error when no fields provided", func(t *testing.T) {
		result, _ := UpdateProject(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
		}))
		if !result.IsError {
			t.Error("expected error when no update fields provided")
		}
	})
}

func TestArchiveProject(t *testing.T) {
	t.Run("archives project", func(t *testing.T) {
		var archivedPID string
		db := &fakeDB{
			archiveProject: func(_ context.Context, _, pid string) error {
				archivedPID = pid
				return nil
			},
		}

		result, _ := ArchiveProject(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "proj-to-archive",
		}))

		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}
		if archivedPID != "proj-to-archive" {
			t.Errorf("expected proj-to-archive, got %q", archivedPID)
		}
	})
}
