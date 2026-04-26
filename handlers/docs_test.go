package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/raghav-anand/briefcase-internal/models"
	"github.com/raghav-anand/briefcase-internal/storage"
)

func TestUploadDoc(t *testing.T) {
	t.Run("saves inline doc and returns id", func(t *testing.T) {
		var capturedDoc *models.DocInput
		db := &fakeDB{
			upsertDoc: func(_ context.Context, _, _ string, doc *models.DocInput, _ *storage.GCSClient) (string, string, error) {
				capturedDoc = doc
				return "doc-5", "inline", nil
			},
		}

		result, err := UploadDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "API Reference",
			"doc_type":   "api_docs",
			"format":     "markdown",
			"content":    "# API\n\nEndpoints here.",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["doc_id"] != "doc-5" {
			t.Errorf("expected doc_id 'doc-5', got %v", body["doc_id"])
		}
		if body["storage"] != "inline" {
			t.Errorf("expected storage 'inline', got %v", body["storage"])
		}
		if capturedDoc.Title != "API Reference" {
			t.Errorf("expected title 'API Reference', got %q", capturedDoc.Title)
		}
		if capturedDoc.UpdatedBy != "claude" {
			t.Errorf("expected updated_by 'claude', got %q", capturedDoc.UpdatedBy)
		}
	})

	t.Run("rejects invalid doc_type", func(t *testing.T) {
		result, _ := UploadDoc(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "t",
			"doc_type":   "invalid",
			"format":     "markdown",
			"content":    "x",
		}))
		if !result.IsError {
			t.Error("expected error for invalid doc_type")
		}
	})

	t.Run("rejects invalid format", func(t *testing.T) {
		result, _ := UploadDoc(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "t",
			"doc_type":   "readme",
			"format":     "html",
			"content":    "x",
		}))
		if !result.IsError {
			t.Error("expected error for invalid format")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			upsertDoc: func(_ context.Context, _, _ string, _ *models.DocInput, _ *storage.GCSClient) (string, string, error) {
				return "", "", errors.New("storage error")
			},
		}
		result, _ := UploadDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "t",
			"doc_type":   "readme",
			"format":     "markdown",
			"content":    "x",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})

	t.Run("passes doc_id for update when provided", func(t *testing.T) {
		var capturedDoc *models.DocInput
		db := &fakeDB{
			upsertDoc: func(_ context.Context, _, _ string, doc *models.DocInput, _ *storage.GCSClient) (string, string, error) {
				capturedDoc = doc
				return doc.ID, "inline", nil
			},
		}

		result, err := UploadDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_id":     "existing-doc-99",
			"title":      "Updated Doc",
			"doc_type":   "readme",
			"format":     "markdown",
			"content":    "updated content",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}
		if capturedDoc.ID != "existing-doc-99" {
			t.Errorf("expected doc ID 'existing-doc-99' passed to db, got %q", capturedDoc.ID)
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["doc_id"] != "existing-doc-99" {
			t.Errorf("expected doc_id 'existing-doc-99' in response, got %v", body["doc_id"])
		}
	})
}

func TestDeleteDoc(t *testing.T) {
	t.Run("deletes doc and returns confirmation", func(t *testing.T) {
		var capturedDocID string
		db := &fakeDB{
			deleteDoc: func(_ context.Context, _, _, did string, _ *storage.GCSClient) error {
				capturedDocID = did
				return nil
			},
		}

		result, err := DeleteDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_id":     "doc-to-delete",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}
		if capturedDocID != "doc-to-delete" {
			t.Errorf("expected doc_id 'doc-to-delete' passed to db, got %q", capturedDocID)
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["message"] != "Doc deleted." {
			t.Errorf("expected 'Doc deleted.' message, got %v", body["message"])
		}
	})

	t.Run("requires project_id", func(t *testing.T) {
		result, _ := DeleteDoc(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"doc_id": "d1",
		}))
		if !result.IsError {
			t.Error("expected error when project_id missing")
		}
	})

	t.Run("requires doc_id", func(t *testing.T) {
		result, _ := DeleteDoc(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
		}))
		if !result.IsError {
			t.Error("expected error when doc_id missing")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			deleteDoc: func(_ context.Context, _, _, _ string, _ *storage.GCSClient) error {
				return errors.New("doc not found")
			},
		}
		result, _ := DeleteDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_id":     "missing",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}

func TestListDocs(t *testing.T) {
	t.Run("returns doc metadata list", func(t *testing.T) {
		db := &fakeDB{
			listDocs: func(_ context.Context, _, _ string, _ *string) ([]models.RepoDocMeta, error) {
				return []models.RepoDocMeta{
					{ID: "d1", Title: "README", DocType: "readme", Format: "markdown"},
					{ID: "d2", Title: "Architecture", DocType: "architecture", Format: "mermaid"},
				}, nil
			},
		}

		result, _ := ListDocs(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
		}))

		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}
		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		docs, ok := body["docs"].([]any)
		if !ok || len(docs) != 2 {
			t.Errorf("expected 2 docs, got: %v", body["docs"])
		}
	})

	t.Run("passes doc_type filter to db", func(t *testing.T) {
		var capturedFilter *string
		db := &fakeDB{
			listDocs: func(_ context.Context, _, _ string, docType *string) ([]models.RepoDocMeta, error) {
				capturedFilter = docType
				return nil, nil
			},
		}
		ListDocs(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_type":   "readme",
		}))
		if capturedFilter == nil || *capturedFilter != "readme" {
			t.Errorf("expected doc_type filter 'readme', got %v", capturedFilter)
		}
	})

	t.Run("rejects invalid doc_type filter", func(t *testing.T) {
		result, _ := ListDocs(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_type":   "nonsense",
		}))
		if !result.IsError {
			t.Error("expected error for invalid doc_type filter")
		}
	})
}

func TestGetDoc(t *testing.T) {
	t.Run("returns doc with content", func(t *testing.T) {
		db := &fakeDB{
			getDoc: func(_ context.Context, _, _, did string, _ *storage.GCSClient) (*models.RepoDoc, error) {
				return &models.RepoDoc{
					ID:      did,
					Title:   "My Doc",
					Format:  "markdown",
					Content: "# Hello World",
					Version: 3,
				}, nil
			},
		}

		result, _ := GetDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_id":     "d-42",
		}))

		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}
		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["title"] != "My Doc" {
			t.Errorf("expected title 'My Doc', got %v", body["title"])
		}
		if body["content"] != "# Hello World" {
			t.Errorf("expected content, got %v", body["content"])
		}
	})

	t.Run("returns friendly error when doc not found", func(t *testing.T) {
		db := &fakeDB{
			getDoc: func(_ context.Context, _, _, _ string, _ *storage.GCSClient) (*models.RepoDoc, error) {
				return nil, errors.New("not found")
			},
		}
		result, _ := GetDoc(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"doc_id":     "missing",
		}))
		if !result.IsError {
			t.Error("expected error for missing doc")
		}
	})
}
