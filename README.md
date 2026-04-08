# briefcase-mcp

MCP server for [Briefcase](https://github.com/Raghav-Anand/briefcase-mcp) — a project progress tracker for agentic coding sessions. Implements the [Model Context Protocol](https://spec.modelcontextprotocol.io) over Streamable HTTP (2025-03-26 spec) and exposes tools that Claude clients call to track session context, milestones, decisions, and documentation.

## Architecture

```
Claude Client (Desktop / Code / claude.ai)
        │  MCP over Streamable HTTP
        ▼
briefcase-mcp  (this repo, Cloud Run)
        │  Firebase Auth (token validation)
        │  Firestore (projects, sessions, notes, decisions, milestones)
        │  Cloud Storage (large docs > 500 KB)
        ▼
briefcase-internal  (shared Go library)
```

All Firestore and Cloud Storage operations go through `briefcase-internal`. The MCP server is pure routing + business logic.

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.22+ | `brew install go` |
| Firebase CLI | latest | `npm install -g firebase-tools` |
| gcloud CLI | latest | [cloud.google.com/sdk](https://cloud.google.com/sdk/docs/install) |

---

## Running Locally

### 1. Clone both repos side-by-side

```
~/code/
├── briefcase-mcp/       ← this repo
└── briefcase-internal/  ← dependency (replace directive in go.mod)
```

The `go.mod` has `replace github.com/raghav-anand/briefcase-internal => ../briefcase-internal`, so both must be siblings.

### 2. Start the Firebase Auth emulator

```bash
cd ~/code/briefcase-mcp
firebase emulators:start --only auth
```

This starts the Auth emulator on `localhost:9099`.

### 3. Start the Firestore emulator

```bash
firebase emulators:start --only firestore
```

Firestore emulator runs on `localhost:8080` by default. If that clashes with the MCP server port, use `--project=briefcase-dev` and set `FIRESTORE_EMULATOR_HOST`.

Or start both together:

```bash
firebase emulators:start --only auth,firestore
```

### 4. Set environment variables

```bash
export PORT=8080
export GCP_PROJECT_ID=briefcase-dev
export FIREBASE_PROJECT_ID=briefcase-dev
export GCS_BUCKET=                       # leave empty to skip GCS (large docs will fail)
export FIREBASE_AUTH_EMULATOR_HOST=localhost:9099
export FIRESTORE_EMULATOR_HOST=localhost:8081   # adjust to your emulator port

# For the OAuth flow (needed only if testing end-to-end with a real Claude client):
export OAUTH_CLIENT_ID=your-google-oauth-client-id
export OAUTH_CLIENT_SECRET=your-google-oauth-client-secret
export MCP_SERVER_URL=http://localhost:8080
export FIREBASE_API_KEY=your-firebase-web-api-key
```

### 5. Run the server

```bash
go run .
# → briefcase-mcp listening on :8080
```

### 6. Smoke test

```bash
# Health check
curl http://localhost:8080/healthz

# OAuth metadata
curl http://localhost:8080/.well-known/oauth-authorization-server | jq .
```

---

## Running Tests

### Unit tests (no external services required)

All unit tests use in-memory fakes. No Firebase, Firestore, or GCS needed.

```bash
go test ./...
```

Expected output:

```
ok  github.com/raghav-anand/briefcase-mcp
ok  github.com/raghav-anand/briefcase-mcp/auth
ok  github.com/raghav-anand/briefcase-mcp/handlers
ok  github.com/raghav-anand/briefcase-mcp/middleware
```

### Run a specific package

```bash
go test ./handlers/...   # handler unit tests
go test ./auth/...       # OAuth state machine tests
go test ./middleware/...  # auth middleware tests
go test .                # session manager + main package tests
```

### With verbose output

```bash
go test -v ./...
```

### With race detector

```bash
go test -race ./...
```

---

## Test Structure

```
briefcase-mcp/
├── handlers/
│   ├── testhelpers_test.go   ← fakeDB, request builder, context helpers
│   ├── session_test.go       ← start_session, end_session
│   ├── project_test.go       ← create/list/get/update/archive project
│   ├── progress_test.go      ← milestones, notes, decisions
│   └── docs_test.go          ← upload/list/get doc
├── middleware/
│   └── auth_test.go          ← RequireAuth middleware, ClaimsFromContext
├── auth/
│   └── oauth_test.go         ← OAuth state machine (authorize, token exchange)
└── session_manager_test.go   ← stale session cleanup logic
```

### What is and isn't tested

| Covered by unit tests | Requires integration |
|-----------------------|---------------------|
| All 13 MCP tool handlers (happy path + errors) | Firebase token verification (real token) |
| Missing/invalid parameters | Firestore emulator queries |
| Auth middleware (missing/wrong header) | Cloud Storage uploads |
| OAuth state machine (well-known, authorize, token) | End-to-end Claude MCP connection |
| Stale session cleanup logic | |

Integration tests (using Firebase/Firestore emulators) are a natural next step but not included here. The unit tests are sufficient to verify all business logic.

---

## Connecting to Claude

Once deployed (or with ngrok for local), add the MCP server to Claude:

**Claude Desktop** (`~/.claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "briefcase": {
      "url": "https://your-mcp-server.run.app/mcp"
    }
  }
}
```

**Claude Code** (CLI):
```bash
claude mcp add briefcase --transport http https://your-mcp-server.run.app/mcp
```

On first connection, Claude will trigger the OAuth flow. A browser window opens for Google sign-in. After that, all sessions are authenticated automatically.

---

## Testing the OAuth Flow Locally

For local OAuth testing you need a real Google OAuth client and Firebase project (the emulator doesn't support full OAuth flows):

1. Create a Google OAuth client at [console.cloud.google.com](https://console.cloud.google.com) with redirect URI `http://localhost:8080/oauth/callback`
2. Set `OAUTH_CLIENT_ID`, `OAUTH_CLIENT_SECRET`, `MCP_SERVER_URL=http://localhost:8080`, `FIREBASE_API_KEY`
3. Visit `http://localhost:8080/oauth/authorize?client_id=test&code_challenge=test&redirect_uri=http://localhost:3000/cb`
4. Complete Google sign-in
5. Observe the redirect with `?code=...`

For a full end-to-end test, configure Claude to point at your local server via [ngrok](https://ngrok.com):

```bash
ngrok http 8080
# → https://abc123.ngrok.io

export MCP_SERVER_URL=https://abc123.ngrok.io
# restart the server, then add to Claude
```

---

## Deployment

Push to `main` — GitHub Actions handles the rest:

1. Checks out `briefcase-internal` alongside this repo
2. Runs `go mod vendor` (bundles briefcase-internal for Docker)
3. Builds and pushes Docker image to Artifact Registry
4. Deploys new Cloud Run revision

### Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `WIF_PROVIDER` | Workload Identity Federation provider resource name |
| `GCP_SA_EMAIL` | Service account email for deployment |
| `GCP_PROJECT_ID` | GCP project ID |

### Required Cloud Run Environment Variables

Set these in `briefcase-infra` (Terraform) or manually in the Cloud Run console:

```
GCP_PROJECT_ID
FIREBASE_PROJECT_ID
GCS_BUCKET
OAUTH_CLIENT_ID
OAUTH_CLIENT_SECRET       ← from Secret Manager
MCP_SERVER_URL
FIREBASE_API_KEY          ← from Secret Manager
```

---

## Environment Variables Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | No | `8080` | HTTP listen port |
| `GCP_PROJECT_ID` | **Yes** | — | GCP project for Firestore |
| `GCS_BUCKET` | No | — | Cloud Storage bucket for large docs |
| `MCP_SERVER_URL` | Yes (OAuth) | — | Public base URL, e.g. `https://mcp.example.com` |
| `OAUTH_CLIENT_ID` | **Yes** | — | Google OAuth client ID (also validates token `aud` claim) |
| `OAUTH_CLIENT_SECRET` | Yes (OAuth) | — | Google OAuth client secret |
| `FIRESTORE_EMULATOR_HOST` | No | — | Set to `localhost:8081` for local dev |
