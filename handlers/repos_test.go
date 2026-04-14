package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/raghav-anand/briefcase-internal/models"
)

func TestAddRepo(t *testing.T) {
	t.Run("creates repo and returns id", func(t *testing.T) {
		var captured *models.CreateRepoInput
		db := &fakeDB{
			addRepo: func(_ context.Context, _, _ string, r *models.CreateRepoInput) (string, error) {
				captured = r
				return "repo-99", nil
			},
		}

		result, err := AddRepo(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id":  "p1",
			"name":        "briefcase-api",
			"url":         "https://github.com/org/briefcase-api",
			"description": "REST API",
			"language":    "Go",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["repo_id"] != "repo-99" {
			t.Errorf("expected repo_id 'repo-99', got %v", body["repo_id"])
		}
		if captured.Name != "briefcase-api" {
			t.Errorf("expected name 'briefcase-api', got %q", captured.Name)
		}
		if captured.URL != "https://github.com/org/briefcase-api" {
			t.Errorf("expected url to be set, got %q", captured.URL)
		}
		if captured.Language != "Go" {
			t.Errorf("expected language 'Go', got %q", captured.Language)
		}
	})

	t.Run("requires project_id", func(t *testing.T) {
		result, _ := AddRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"name": "repo",
			"url":  "https://github.com/org/repo",
		}))
		if !result.IsError {
			t.Error("expected error when project_id missing")
		}
	})

	t.Run("requires name", func(t *testing.T) {
		result, _ := AddRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"url":        "https://github.com/org/repo",
		}))
		if !result.IsError {
			t.Error("expected error when name missing")
		}
	})

	t.Run("requires url", func(t *testing.T) {
		result, _ := AddRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"name":       "repo",
		}))
		if !result.IsError {
			t.Error("expected error when url missing")
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		result, _ := AddRepo(svc(&fakeDB{}))(unauthCtx(), req(map[string]any{
			"project_id": "p1", "name": "r", "url": "https://github.com/org/r",
		}))
		if !result.IsError {
			t.Error("expected error when unauthenticated")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			addRepo: func(_ context.Context, _, _ string, _ *models.CreateRepoInput) (string, error) {
				return "", errors.New("db error")
			},
		}
		result, _ := AddRepo(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1", "name": "r", "url": "https://github.com/org/r",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}

func TestListRepos(t *testing.T) {
	t.Run("returns repos list", func(t *testing.T) {
		db := &fakeDB{
			listRepos: func(_ context.Context, _, _ string) ([]models.Repo, error) {
				return []models.Repo{
					{ID: "r1", Name: "briefcase-api", URL: "https://github.com/org/briefcase-api"},
					{ID: "r2", Name: "briefcase-web", URL: "https://github.com/org/briefcase-web"},
				}, nil
			},
		}

		result, err := ListRepos(svc(db))(authedCtx("u"), req(map[string]any{"project_id": "p1"}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		repos, ok := body["repos"].([]any)
		if !ok || len(repos) != 2 {
			t.Errorf("expected 2 repos, got %v", body["repos"])
		}
	})

	t.Run("returns empty list when no repos", func(t *testing.T) {
		result, _ := ListRepos(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{"project_id": "p1"}))
		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		result, _ := ListRepos(svc(&fakeDB{}))(unauthCtx(), req(map[string]any{"project_id": "p1"}))
		if !result.IsError {
			t.Error("expected error when unauthenticated")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			listRepos: func(_ context.Context, _, _ string) ([]models.Repo, error) {
				return nil, errors.New("db error")
			},
		}
		result, _ := ListRepos(svc(db))(authedCtx("u"), req(map[string]any{"project_id": "p1"}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}

func TestUpdateRepo(t *testing.T) {
	t.Run("updates repo with provided fields", func(t *testing.T) {
		var capturedUpdates map[string]interface{}
		db := &fakeDB{
			updateRepo: func(_ context.Context, _, _, _ string, updates map[string]interface{}) error {
				capturedUpdates = updates
				return nil
			},
		}

		result, err := UpdateRepo(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id":  "p1",
			"repo_id":     "r1",
			"description": "Updated description",
			"language":    "TypeScript",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}
		if capturedUpdates["description"] != "Updated description" {
			t.Errorf("expected description update, got %v", capturedUpdates)
		}
		if capturedUpdates["language"] != "TypeScript" {
			t.Errorf("expected language update, got %v", capturedUpdates)
		}
		if _, ok := capturedUpdates["name"]; ok {
			t.Error("expected name to be absent from updates")
		}
	})

	t.Run("rejects when no fields provided", func(t *testing.T) {
		result, _ := UpdateRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"repo_id":    "r1",
		}))
		if !result.IsError {
			t.Error("expected error when no update fields provided")
		}
	})

	t.Run("requires project_id", func(t *testing.T) {
		result, _ := UpdateRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"repo_id": "r1", "name": "new",
		}))
		if !result.IsError {
			t.Error("expected error when project_id missing")
		}
	})

	t.Run("requires repo_id", func(t *testing.T) {
		result, _ := UpdateRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1", "name": "new",
		}))
		if !result.IsError {
			t.Error("expected error when repo_id missing")
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		result, _ := UpdateRepo(svc(&fakeDB{}))(unauthCtx(), req(map[string]any{
			"project_id": "p1", "repo_id": "r1", "name": "new",
		}))
		if !result.IsError {
			t.Error("expected error when unauthenticated")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			updateRepo: func(_ context.Context, _, _, _ string, _ map[string]interface{}) error {
				return errors.New("db error")
			},
		}
		result, _ := UpdateRepo(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1", "repo_id": "r1", "name": "new",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}

func TestRemoveRepo(t *testing.T) {
	t.Run("removes repo", func(t *testing.T) {
		var removedRID string
		db := &fakeDB{
			removeRepo: func(_ context.Context, _, _, rid string) error {
				removedRID = rid
				return nil
			},
		}

		result, err := RemoveRepo(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"repo_id":    "r42",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}
		if removedRID != "r42" {
			t.Errorf("expected r42, got %q", removedRID)
		}
	})

	t.Run("requires project_id", func(t *testing.T) {
		result, _ := RemoveRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{"repo_id": "r1"}))
		if !result.IsError {
			t.Error("expected error when project_id missing")
		}
	})

	t.Run("requires repo_id", func(t *testing.T) {
		result, _ := RemoveRepo(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{"project_id": "p1"}))
		if !result.IsError {
			t.Error("expected error when repo_id missing")
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		result, _ := RemoveRepo(svc(&fakeDB{}))(unauthCtx(), req(map[string]any{
			"project_id": "p1", "repo_id": "r1",
		}))
		if !result.IsError {
			t.Error("expected error when unauthenticated")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			removeRepo: func(_ context.Context, _, _, _ string) error {
				return errors.New("db error")
			},
		}
		result, _ := RemoveRepo(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1", "repo_id": "r1",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}
