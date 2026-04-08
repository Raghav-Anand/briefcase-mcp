package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/raghav-anand/briefcase-internal/db"
)

const (
	staleSessionAge      = 2 * time.Hour
	cleanupInterval      = 15 * time.Minute
)

// StartCleanupRoutine runs a background goroutine that periodically closes stale sessions.
// It also registers the one-shot cleanup at /internal/cleanup so Cloud Scheduler can trigger it.
func StartCleanupRoutine(ctx context.Context, dbClient *db.Client) {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := runCleanup(ctx, dbClient); err != nil {
					log.Printf("cleanup error: %v", err)
				}
			}
		}
	}()
}

// runCleanup finds and auto-closes sessions that have been active for too long.
func runCleanup(ctx context.Context, dbClient *db.Client) error {
	stale, err := dbClient.FindStaleSessions(ctx, staleSessionAge)
	if err != nil {
		return fmt.Errorf("FindStaleSessions: %w", err)
	}
	if len(stale) == 0 {
		return nil
	}

	log.Printf("closing %d stale session(s)", len(stale))
	for _, s := range stale {
		summary := buildAutoCloseSummary(s.ToolCallCount)
		if err := dbClient.AutoCloseSession(ctx, s.UserID, s.ProjectID, s.ID, summary); err != nil {
			log.Printf("AutoCloseSession %s: %v", s.ID, err)
		}
	}
	return nil
}

// buildAutoCloseSummary produces a basic summary from the tool call count when a session is auto-closed.
func buildAutoCloseSummary(toolCallCount int) string {
	parts := []string{"Session auto-closed due to inactivity."}
	if toolCallCount > 0 {
		parts = append(parts, fmt.Sprintf("Tools called: %d.", toolCallCount))
	}
	return strings.Join(parts, " ")
}
