# Codebase Guide

A file-by-file walkthrough of this repo. The goal is to help you understand what each piece does, why it exists, and how the pieces connect.

---

## Big picture first

When a Claude client connects, three things happen in sequence:

1. **Auth** — Claude presents a Firebase ID token in the `Authorization` header. The middleware validates it before any tool handler runs.
2. **MCP dispatch** — The mcp-go library parses the incoming JSON-RPC request and routes it to the right tool handler.
3. **Handler** — The handler talks to Firestore (via `briefcase-internal`) and returns a JSON result to Claude.

That's the entire hot path. Everything else is startup, OAuth login, or background cleanup.

---

## Root files

### `main.go`
**The entry point.** Does six things on startup, in order:
1. Reads environment variables (`GCP_PROJECT_ID`, `PORT`, etc.)
2. Initialises the Firebase Auth client (for token validation)
3. Initialises the Firestore client (for all DB reads/writes)
4. Optionally initialises the Cloud Storage client (for large docs)
5. Builds the HTTP route table and starts the server
6. Kicks off the background stale-session cleanup goroutine

Routes registered:
- `/.well-known/oauth-authorization-server` → OAuth metadata
- `/oauth/authorize`, `/oauth/callback`, `/oauth/token` → OAuth login flow
- `/mcp` → MCP tool dispatch (auth-gated)
- `/internal/cleanup` → manual cleanup trigger for Cloud Scheduler
- `/healthz` → health check for Cloud Run

### `server.go`
**Declares every MCP tool Claude can call.** Creates the `mcp-go` server, registers all 13 tools with their parameter schemas, and wires each tool to its handler function. Also registers the system prompt that Claude reads on connection, which tells it *when* to call each tool.

Think of this file as the public API contract. If you want to add a new tool, add it here (plus a handler in the relevant `handlers/` file).

### `session_manager.go`
**Background cleanup for sessions that never got ended.** If Claude crashes or the user closes the tab mid-session, `end_session` never gets called. This goroutine runs every 15 minutes and auto-closes any session that's been active for more than 2 hours.

Two entry points:
- `StartCleanupRoutine(ctx, db)` — called from `main.go`, starts the ticker goroutine
- `runCleanup(ctx, db)` — the actual cleanup logic; also called directly from `POST /internal/cleanup` so Cloud Scheduler can trigger it on demand

---

## `auth/` — OAuth 2.0 login flow

### `auth/oauth.go`
**Implements the OAuth 2.0 authorization server required by the MCP spec.** Claude clients don't accept API keys — they need a full OAuth 2.0 flow. This file handles the whole dance:

```
Claude discovers /.well-known/oauth-authorization-server
      ↓
Claude sends user to /oauth/authorize
      ↓
Server redirects to Google OAuth (accounts.google.com)
      ↓
User signs in with Google
      ↓
Google redirects to /oauth/callback with an auth code
      ↓
Server exchanges the code for a Google ID token,
then calls Firebase REST API to get a Firebase ID token
      ↓
Server creates a short-lived auth code and redirects
back to Claude's redirect_uri with ?code=...
      ↓
Claude calls POST /oauth/token to exchange the code
for the Firebase ID token (returned as access_token)
      ↓
Claude stores the token and uses it on every future request
```

All state (pending authorizations, pending codes) is in-memory with automatic TTL cleanup.

The four handlers are registered onto the mux by `RegisterRoutes(mux)`, called from `main.go`.

---

## `middleware/` — HTTP middleware

### `middleware/auth.go`
**Validates Firebase ID tokens on every MCP request.** Reads the `Authorization: Bearer <token>` header, calls `auth.VerifyToken` from `briefcase-internal`, and injects the user's identity (`UID`, `Email`, `Name`) into the request context.

Returns `401` immediately if the header is missing or the token is invalid. The MCP handler never sees unauthenticated requests.

Two exported symbols:
- `RequireAuth(client, next)` — the middleware function, wraps any `http.Handler`
- `ClaimsFromContext(ctx)` — helper that handlers call to get the current user's identity

---

## `handlers/` — MCP tool implementations

This is where the business logic lives. Each file corresponds to a category of tools.

### `handlers/services.go`
**Shared dependencies.** Defines the `Services` struct that every handler receives:
```go
type Services struct {
    DB  DBClient           // Firestore operations
    GCS *storage.GCSClient // Cloud Storage (for large docs)
}
```
In production, `DB` is `*db.Client` from `briefcase-internal`. In tests, it's a `fakeDB`.

### `handlers/interfaces.go`
**The `DBClient` interface.** Lists every Firestore method that handlers call. The real `*db.Client` satisfies this interface automatically. Having it here means tests can inject a fake without needing a real Firestore connection.

You'll never need to touch this unless you add a new handler that calls a new db method — in which case, add that method signature here.

### `handlers/session.go`
**`start_session` and `end_session`** — the two most important tools.

`start_session` is called at the beginning of every conversation. It:
1. Auto-closes any stale active session (in case the last one wasn't ended cleanly)
2. Creates a new session document
3. Fetches the project, last session summary, open milestones, recent decisions, recent notes, and doc metadata
4. Returns everything as one JSON package so Claude has full context

`end_session` is called when the user wraps up. It writes the session summary and next steps to Firestore, and denormalizes the summary onto the project document for fast retrieval next time.

### `handlers/project.go`
**Project CRUD**: `create_project`, `list_projects`, `get_project`, `update_project`, `archive_project`.

Straightforward — each handler validates its inputs, calls the corresponding `db.Client` method, and returns a JSON result. `update_project` only includes fields in the update map if they were explicitly provided (so you can update just the status without clearing the name).

### `handlers/progress.go`
**Progress tracking**: `add_milestone`, `complete_milestone`, `add_note`, `log_decision`.

Each handler:
1. Looks up the active session (for the `session_id` field on new documents)
2. Logs the tool call to `tool_call_log` (the crash-recovery audit trail)
3. Writes the document to Firestore

The `logToolCall` helper at the bottom is shared by all four handlers.

### `handlers/docs.go`
**Documentation**: `upload_doc`, `list_docs`, `get_doc`.

`upload_doc` passes the content to `db.UpsertDoc`, which decides where to store it:
- Under 500 KB → inline in the Firestore document
- 500 KB or over → uploaded to Cloud Storage, with a `gcs_path` reference stored in Firestore

`get_doc` transparently fetches from GCS if needed — callers always get the full content regardless of where it's stored.

`list_docs` returns metadata only (no content), so Claude can see what docs exist without fetching potentially large files.

---

## `go.mod` and `go.sum`

Standard Go module files. Key direct dependencies:
- `github.com/mark3labs/mcp-go v0.47.0` — MCP protocol implementation (Streamable HTTP transport)
- `firebase.google.com/go/v4` — Firebase Admin SDK (Auth token validation)
- `github.com/raghav-anand/briefcase-internal` — all Firestore + GCS operations (local `replace` directive pointing to `../briefcase-internal`)

The `replace` directive means both repos must be siblings on disk when building locally. In CI, the deploy workflow checks out `briefcase-internal` alongside this repo before running `go mod vendor`.

---

## `Dockerfile`

Two-stage build:
1. **Builder** — compiles the Go binary with CGO disabled (needed for the small Alpine runtime image)
2. **Runtime** — just `alpine:3.19` + `ca-certificates` + the binary

The image expects `vendor/` to already exist in the build context (the CI workflow runs `go mod vendor` before `docker build`).

---

## `.github/workflows/deploy.yml`

Runs on every push to `main`:
1. Checks out `briefcase-internal` alongside this repo (so the `replace` directive resolves)
2. Runs `go mod vendor` to bundle all dependencies (including `briefcase-internal`) into the image
3. Authenticates to GCP via Workload Identity Federation (no long-lived service account keys)
4. Builds and pushes the Docker image to Artifact Registry
5. Deploys a new Cloud Run revision

The Cloud Run service definition (scaling, memory, env vars) lives in `briefcase-infra` and is not touched by this workflow.

---

## Test files

Each package has its tests alongside the production code (`_test.go` suffix). All tests are pure unit tests — no Firebase, Firestore, or network calls.

| File | Tests |
|------|-------|
| `handlers/testhelpers_test.go` | Shared test infrastructure: `fakeDB`, request builder, context helpers |
| `handlers/session_test.go` | `start_session`, `end_session` |
| `handlers/project_test.go` | All five project CRUD tools |
| `handlers/progress_test.go` | Milestones, notes, decisions |
| `handlers/docs_test.go` | Doc upload, list, get |
| `middleware/auth_test.go` | Missing header, wrong scheme, context extraction |
| `auth/oauth_test.go` | OAuth state machine: well-known metadata, authorize, token exchange |
| `session_manager_test.go` | Stale session cleanup logic |

The `fakeDB` in `testhelpers_test.go` implements `DBClient` with configurable function fields — each test sets only the functions it cares about, and the rest return safe zero values. This means tests are self-contained and never talk to a real database.

---

## How it all fits together

```
HTTP request arrives at /mcp
        │
        ▼
middleware.RequireAuth
  validates Firebase token
  injects *auth.Claims into context
        │
        ▼
StreamableHTTPServer (mcp-go)
  parses JSON-RPC envelope
  routes to registered tool
        │
        ▼
handler function (e.g. handlers.AddNote)
  calls middleware.ClaimsFromContext(ctx)  → gets UID
  calls svc.DB.GetActiveSession(...)       → gets session ID
  calls svc.DB.LogToolCall(...)            → writes audit log
  calls svc.DB.CreateNote(...)             → writes the note
  returns mcp.NewToolResultText(json)
        │
        ▼
mcp-go serialises response back to Claude
```
