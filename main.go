package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/raghav-anand/briefcase-internal/db"
	"github.com/raghav-anand/briefcase-internal/storage"
	"github.com/raghav-anand/briefcase-mcp/auth"
	"github.com/raghav-anand/briefcase-mcp/handlers"
	"github.com/raghav-anand/briefcase-mcp/middleware"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	port := getEnv("PORT", "8080")
	gcpProject := mustEnv("GCP_PROJECT_ID")
	oauthClientID := mustEnv("OAUTH_CLIENT_ID")
	gcsBucket := os.Getenv("GCS_BUCKET")

	// Validate Google ID tokens using Google's public JWKS (via briefcase-internal).
	// Keys are fetched lazily on first request and cached with automatic rotation.
	tokenValidator := middleware.NewValidator(oauthClientID)

	// Initialize Firestore client.
	dbClient, err := db.NewClient(ctx, gcpProject)
	if err != nil {
		log.Fatalf("db.NewClient: %v", err)
	}
	defer dbClient.Close()

	// Initialize Cloud Storage client (optional; required only for large docs).
	var gcsClient *storage.GCSClient
	if gcsBucket != "" {
		gcsClient, err = storage.NewGCSClient(ctx, gcsBucket)
		if err != nil {
			log.Fatalf("storage.NewGCSClient: %v", err)
		}
	} else {
		log.Println("GCS_BUCKET not set — large doc uploads will fail")
	}

	svc := &handlers.Services{
		DB:  dbClient,
		GCS: gcsClient,
	}

	// Build MCP server.
	mcpSrv := newMCPServer(svc)

	// Build Streamable HTTP transport (MCP spec 2025-03-26).
	streamServer := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithEndpointPath("/mcp"),
	)

	// Build route mux.
	mux := http.NewServeMux()

	// OAuth 2.0 endpoints (unauthenticated — used during login).
	oauthHandler := auth.NewOAuthHandler()
	oauthHandler.RegisterRoutes(mux)

	// MCP endpoint — requires a valid Google ID token.
	mux.Handle("/mcp", middleware.RequireAuth(tokenValidator, streamServer))

	// Cloud Scheduler cleanup endpoint.
	mux.HandleFunc("/internal/cleanup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := runCleanup(r.Context(), dbClient); err != nil {
			log.Printf("manual cleanup error: %v", err)
			http.Error(w, "cleanup failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "cleanup complete")
	})

	// Health check.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Start stale session cleanup goroutine.
	StartCleanupRoutine(ctx, dbClient)

	// Start HTTP server.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout — SSE connections are long-lived.
	}

	go func() {
		log.Printf("briefcase-mcp listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}
