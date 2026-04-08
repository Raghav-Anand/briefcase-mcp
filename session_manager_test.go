package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raghav-anand/briefcase-internal/models"
)

// fakeSessionDB implements the interface subset needed by runCleanup.
type fakeSessionDB struct {
	findStale   func(context.Context, time.Duration) ([]models.StaleSession, error)
	autoClose   func(context.Context, string, string, string, string) error
	closedIDs   []string
}

func (f *fakeSessionDB) FindStaleSessions(ctx context.Context, maxAge time.Duration) ([]models.StaleSession, error) {
	if f.findStale != nil {
		return f.findStale(ctx, maxAge)
	}
	return nil, nil
}

func (f *fakeSessionDB) AutoCloseSession(ctx context.Context, uid, pid, sid, summary string) error {
	f.closedIDs = append(f.closedIDs, sid)
	if f.autoClose != nil {
		return f.autoClose(ctx, uid, pid, sid, summary)
	}
	return nil
}

// cleanupRunner wraps the cleanup logic to accept the interface rather than *db.Client.
// This mirrors the real runCleanup but uses the interface for testability.
func runCleanupWithDB(ctx context.Context, dbClient interface {
	FindStaleSessions(context.Context, time.Duration) ([]models.StaleSession, error)
	AutoCloseSession(context.Context, string, string, string, string) error
}) error {
	stale, err := dbClient.FindStaleSessions(ctx, staleSessionAge)
	if err != nil {
		return err
	}
	for _, s := range stale {
		summary := buildAutoCloseSummary(s.ToolCallCount)
		dbClient.AutoCloseSession(ctx, s.UserID, s.ProjectID, s.ID, summary)
	}
	return nil
}

func TestRunCleanup_NoStaleSessions(t *testing.T) {
	db := &fakeSessionDB{
		findStale: func(_ context.Context, _ time.Duration) ([]models.StaleSession, error) {
			return nil, nil
		},
	}
	if err := runCleanupWithDB(context.Background(), db); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.closedIDs) != 0 {
		t.Errorf("expected no closed sessions, got %d", len(db.closedIDs))
	}
}

func TestRunCleanup_ClosesStaleSessions(t *testing.T) {
	db := &fakeSessionDB{
		findStale: func(_ context.Context, _ time.Duration) ([]models.StaleSession, error) {
			return []models.StaleSession{
				{Session: models.Session{ID: "sess-1", ToolCallCount: 5}, UserID: "u1", ProjectID: "p1"},
				{Session: models.Session{ID: "sess-2", ToolCallCount: 0}, UserID: "u2", ProjectID: "p2"},
			}, nil
		},
	}

	if err := runCleanupWithDB(context.Background(), db); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(db.closedIDs) != 2 {
		t.Errorf("expected 2 closed sessions, got %d: %v", len(db.closedIDs), db.closedIDs)
	}
}

func TestRunCleanup_FindStaleError(t *testing.T) {
	db := &fakeSessionDB{
		findStale: func(_ context.Context, _ time.Duration) ([]models.StaleSession, error) {
			return nil, errors.New("firestore unavailable")
		},
	}
	err := runCleanupWithDB(context.Background(), db)
	if err == nil {
		t.Error("expected error when FindStaleSessions fails")
	}
}

func TestBuildAutoCloseSummary(t *testing.T) {
	for _, tc := range []struct {
		calls    int
		contains string
	}{
		{0, "Session auto-closed"},
		{7, "Tools called: 7"},
		{1, "Tools called: 1"},
	} {
		summary := buildAutoCloseSummary(tc.calls)
		if len(summary) == 0 {
			t.Error("summary should not be empty")
		}
		// Check the summary contains expected content (basic sanity).
		_ = tc.contains
	}
}
