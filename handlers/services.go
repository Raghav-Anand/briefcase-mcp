package handlers

import "github.com/raghav-anand/briefcase-internal/storage"

// Services bundles all backend dependencies shared by tool handlers.
type Services struct {
	DB  DBClient           // *db.Client in production; fake in tests
	GCS *storage.GCSClient // may be nil if GCS_BUCKET is not configured
}
