package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/raghav-anand/briefcase-internal/models"
)

func TestAddMilestone(t *testing.T) {
	t.Run("creates milestone and returns id", func(t *testing.T) {
		var captured *models.CreateMilestoneInput
		db := &fakeDB{
			createMilestone: func(_ context.Context, _, _ string, m *models.CreateMilestoneInput) (string, error) {
				captured = m
				return "ms-99", nil
			},
		}

		result, err := AddMilestone(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "Launch v1",
		}))

		if err != nil || result.IsError {
			t.Fatalf("unexpected failure: err=%v, result=%s", err, resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["milestone_id"] != "ms-99" {
			t.Errorf("expected milestone_id 'ms-99', got %v", body["milestone_id"])
		}
		if captured.Title != "Launch v1" {
			t.Errorf("expected title 'Launch v1', got %q", captured.Title)
		}
	})

	t.Run("parses due_date in YYYY-MM-DD format", func(t *testing.T) {
		var captured *models.CreateMilestoneInput
		db := &fakeDB{
			createMilestone: func(_ context.Context, _, _ string, m *models.CreateMilestoneInput) (string, error) {
				captured = m
				return "ms-1", nil
			},
		}

		result, _ := AddMilestone(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "Ship it",
			"due_date":   "2026-06-01",
		}))

		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}
		if captured.DueDate == nil {
			t.Error("expected due_date to be parsed")
		}
	})

	t.Run("rejects invalid due_date", func(t *testing.T) {
		result, _ := AddMilestone(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"title":      "t",
			"due_date":   "not-a-date",
		}))
		if !result.IsError {
			t.Error("expected error for invalid due_date")
		}
	})

	t.Run("propagates db error", func(t *testing.T) {
		db := &fakeDB{
			createMilestone: func(_ context.Context, _, _ string, _ *models.CreateMilestoneInput) (string, error) {
				return "", errors.New("db error")
			},
		}
		result, _ := AddMilestone(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1", "title": "t",
		}))
		if !result.IsError {
			t.Error("expected error when db fails")
		}
	})
}

func TestCompleteMilestone(t *testing.T) {
	t.Run("completes milestone", func(t *testing.T) {
		var completedMID string
		db := &fakeDB{
			completeMilestone: func(_ context.Context, _, _, mid string) error {
				completedMID = mid
				return nil
			},
		}

		result, _ := CompleteMilestone(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id":   "p1",
			"milestone_id": "ms-42",
		}))

		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}
		if completedMID != "ms-42" {
			t.Errorf("expected ms-42, got %q", completedMID)
		}
	})
}

func TestAddNote(t *testing.T) {
	t.Run("saves note with default type", func(t *testing.T) {
		var captured *models.CreateNoteInput
		db := &fakeDB{
			createNote: func(_ context.Context, _, _ string, n *models.CreateNoteInput) (string, error) {
				captured = n
				return "note-1", nil
			},
		}

		result, _ := AddNote(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"content":    "Found a bug in auth",
		}))

		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}
		if captured.NoteType != "general" {
			t.Errorf("expected default note_type 'general', got %q", captured.NoteType)
		}
	})

	t.Run("accepts valid note types", func(t *testing.T) {
		for _, noteType := range []string{"general", "bug", "idea", "todo"} {
			t.Run(noteType, func(t *testing.T) {
				result, _ := AddNote(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
					"project_id": "p1",
					"content":    "x",
					"note_type":  noteType,
				}))
				if result.IsError {
					t.Errorf("expected success for note_type %q, got: %s", noteType, resultText(result))
				}
			})
		}
	})

	t.Run("rejects invalid note type", func(t *testing.T) {
		result, _ := AddNote(svc(&fakeDB{}))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"content":    "x",
			"note_type":  "invalid",
		}))
		if !result.IsError {
			t.Error("expected error for invalid note_type")
		}
	})
}

func TestLogDecision(t *testing.T) {
	t.Run("creates decision with rationale and tags", func(t *testing.T) {
		var captured *models.CreateDecisionInput
		db := &fakeDB{
			createDecision: func(_ context.Context, _, _ string, d *models.CreateDecisionInput) (string, error) {
				captured = d
				return "dec-7", nil
			},
		}

		result, _ := LogDecision(svc(db))(authedCtx("u"), req(map[string]any{
			"project_id": "p1",
			"decision":   "Use Firestore",
			"rationale":  "Free tier is generous",
			"tags":       []any{"database"},
		}))

		if result.IsError {
			t.Fatalf("unexpected error: %s", resultText(result))
		}

		var body map[string]any
		json.Unmarshal([]byte(resultText(result)), &body)
		if body["decision_id"] != "dec-7" {
			t.Errorf("expected decision_id 'dec-7', got %v", body["decision_id"])
		}
		if captured.Decision != "Use Firestore" {
			t.Errorf("expected decision text 'Use Firestore', got %q", captured.Decision)
		}
	})

	t.Run("requires decision and rationale", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			args map[string]any
		}{
			{"missing decision", map[string]any{"project_id": "p1", "rationale": "r"}},
			{"missing rationale", map[string]any{"project_id": "p1", "decision": "d"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result, _ := LogDecision(svc(&fakeDB{}))(authedCtx("u"), req(tc.args))
				if !result.IsError {
					t.Error("expected error for missing required field")
				}
			})
		}
	})
}
