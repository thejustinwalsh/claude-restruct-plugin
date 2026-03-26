# M4: Server & Dashboard

## Goal

Build a Go backend (Chi) + React SPA (Vite) that provides real-time visibility into the refinement pipeline, session tracking, prompt rating, and the feedback loop needed for self-improvement. The server runs as a daemon managed by the CLI, with SQLite for persistence.

## Depends On

M2 (Core Pipeline) — needs a working refinement pipeline to observe.
M1 (Hook Protocol) — needs session_id and transcript_path from hooks.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│                   React SPA (Vite)                   │
│                                                      │
│  ┌──────────┐ ┌──────────┐ ┌───────────┐           │
│  │ Live Feed│ │ Sessions │ │ Ratings & │           │
│  │ (SSE)    │ │ Browser  │ │ Feedback  │           │
│  └──────────┘ └──────────┘ └───────────┘           │
│                                                      │
│  ┌──────────┐ ┌──────────┐ ┌───────────┐           │
│  │ Prompt   │ │ Pipeline │ │ System    │           │
│  │ Diff View│ │ Metrics  │ │ Health    │           │
│  └──────────┘ └──────────┘ └───────────┘           │
└───────────────────┬─────────────────────────────────┘
                    │ HTTP + SSE
                    ▼
┌─────────────────────────────────────────────────────┐
│              Go Server (Chi + net/http)               │
│                                                      │
│  Routes:                                             │
│  GET  /api/events          → SSE stream              │
│  GET  /api/sessions        → list sessions           │
│  GET  /api/sessions/:id    → session detail           │
│  GET  /api/refinements     → list refinements         │
│  GET  /api/refinements/:id → refinement detail        │
│  POST /api/refinements/:id/rate  → rate a refinement │
│  GET  /api/metrics         → pipeline metrics         │
│  GET  /api/health          → server health            │
│  GET  /api/config          → current config           │
│  GET  /*                   → serve React SPA          │
│                                                      │
│  Internal:                                           │
│  - SSE hub (broadcast to connected clients)          │
│  - SQLite connection pool                            │
│  - Background workers (transcript parser, metrics)   │
└───────────────────┬─────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────┐
│              SQLite (restruct.db)                     │
│                                                      │
│  Tables:                                             │
│  - sessions         (id, started_at, ended_at, cwd) │
│  - refinements      (id, session_id, raw_prompt,     │
│                      refined_prompt, model, latency,  │
│                      cache_hit, created_at)           │
│  - ratings          (refinement_id, score, feedback,  │
│                      created_at)                      │
│  - pipeline_events  (id, refinement_id, stage,        │
│                      duration_ms, metadata, ts)       │
│  - system_prompts   (id, version, content, active,    │
│                      created_at)                      │
└─────────────────────────────────────────────────────┘
```

---

## Tasks

### 4.1 — SQLite Data Layer

**What:** Set up SQLite with schema, migrations, and a clean Go data access layer.

**Dependencies:**
- `modernc.org/sqlite` — pure Go SQLite (no CGO, cross-platform)
- Or `github.com/mattn/go-sqlite3` with CGO if performance matters more

**Recommendation:** `modernc.org/sqlite` — no CGO means simpler cross-compilation for plugin distribution.

**Schema:**

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,              -- session_id from Claude Code hooks
    project_path TEXT NOT NULL,       -- cwd from hook input (enables multi-project)
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    transcript_path TEXT,
    status TEXT DEFAULT 'active'      -- active, ended
);

CREATE TABLE refinements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    project_path TEXT NOT NULL,       -- denormalized for fast project-scoped queries
    raw_prompt TEXT NOT NULL,
    refined_prompt TEXT,              -- NULL if passthrough
    system_prompt_version INTEGER REFERENCES system_prompts(id),
    model TEXT,
    temperature REAL,
    latency_ms INTEGER,
    cache_hit BOOLEAN DEFAULT FALSE,
    passthrough BOOLEAN DEFAULT FALSE,  -- skipped refinement
    output_valid BOOLEAN,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Project-scoped prompt cache (consolidates file-based cache from M6)
CREATE TABLE cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cache_key TEXT NOT NULL,          -- SHA256(normalize(prompt) + rulesHash + model)
    project_path TEXT NOT NULL,       -- same prompt, different project = different cache entry
    raw_prompt TEXT NOT NULL,
    refined_prompt TEXT NOT NULL,
    rules_hash TEXT NOT NULL,
    model TEXT NOT NULL,
    accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,  -- for LRU eviction
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cache_key, project_path)
);

CREATE TABLE ratings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    refinement_id INTEGER NOT NULL REFERENCES refinements(id),
    score INTEGER NOT NULL CHECK(score BETWEEN 1 AND 5),
    feedback TEXT,                    -- freeform user feedback
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE pipeline_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    refinement_id INTEGER REFERENCES refinements(id),
    stage TEXT NOT NULL,              -- rules_load, cache_check, git_context, ollama_inference, validation
    duration_ms INTEGER,
    success BOOLEAN,
    metadata TEXT,                    -- JSON blob for stage-specific data
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE system_prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    active BOOLEAN DEFAULT FALSE,
    avg_rating REAL,                  -- computed from ratings of refinements using this version
    total_refinements INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_refinements_session ON refinements(session_id);
CREATE INDEX idx_refinements_project ON refinements(project_path);
CREATE INDEX idx_refinements_created ON refinements(created_at);
CREATE INDEX idx_ratings_refinement ON ratings(refinement_id);
CREATE INDEX idx_pipeline_events_refinement ON pipeline_events(refinement_id);
CREATE INDEX idx_cache_lookup ON cache(cache_key, project_path);
CREATE INDEX idx_cache_lru ON cache(accessed_at);
```

**Migration system:** Simple numbered SQL files in `server/migrations/`. Run on startup. Track applied migrations in a `_migrations` table.

**Data access:** Repository pattern — one Go file per table with CRUD methods. No ORM (keep it simple, raw SQL with `database/sql`).

### Write-Through Pattern (`.restruct/` ↔ Global SQLite)

The CLI and server share a two-tier data model:

```
Hook fires → restruct refine
  ├── Write to .restruct/sessions/<id>.json  (fast, local, O(1))
  ├── Write to .restruct/cache/              (fast local cache lookup)
  └── Write-through to global SQLite         (dashboard, analytics, ratings)
       ├── If server running: POST /api/internal/refine (server writes to DB)
       └── If server NOT running: CLI writes directly to SQLite
```

**Multi-instance safety:** SQLite handles concurrent writes via WAL mode. Multiple Claude instances in different projects all write to the same global DB. The `project_path` column partitions data logically.

**Offline sync:** If the server wasn't running during a session, the `.restruct/sessions/*.json` files contain enough metadata to backfill session records on next server start. The cache write-through to SQLite can be deferred — the local `.restruct/cache/` serves as the fast path.

**Startup reconciliation:** When the server starts, it:
1. Scans all known project paths for `.restruct/sessions/` files
2. Reconciles any session records not yet in SQLite
3. Marks stale sessions (>24h old, PID dead) as ended

### 4.2 — Go Server (Chi)

**What:** HTTP server with Chi router, SSE hub, and static file serving.

**Dependencies:**
- `github.com/go-chi/chi/v5` — router
- `github.com/go-chi/cors` — CORS middleware (for dev server)
- `modernc.org/sqlite` — database

**Server structure:**
```
server/
├── server.go           -- Server struct, Start(), Shutdown()
├── routes.go           -- Route registration
├── handlers/
│   ├── sessions.go     -- Session CRUD
│   ├── refinements.go  -- Refinement CRUD + rating
│   ├── metrics.go      -- Aggregated metrics
│   ├── events.go       -- SSE stream handler
│   ├── health.go       -- Health check
│   └── config.go       -- Config endpoint
├── sse/
│   └── hub.go          -- SSE broadcast hub (fan-out to connected clients)
├── db/
│   ├── db.go           -- SQLite connection, migration runner
│   ├── sessions.go     -- Session repository
│   ├── refinements.go  -- Refinement repository
│   ├── ratings.go      -- Rating repository
│   ├── events.go       -- Pipeline event repository
│   └── prompts.go      -- System prompt repository
├── migrations/
│   └── 001_initial.sql
└── middleware/
    └── logging.go      -- Request logging middleware
```

**SSE Hub:**
```go
type Hub struct {
    clients    map[chan Event]struct{}
    broadcast  chan Event
    register   chan chan Event
    unregister chan chan Event
}

type Event struct {
    Type string      `json:"type"` // refinement, session, metric, health
    Data interface{} `json:"data"`
}
```

Events streamed:
- `refinement:start` — when a prompt enters the pipeline
- `refinement:complete` — when refinement finishes (with raw → refined diff)
- `refinement:passthrough` — when refinement was skipped
- `session:start` — new Claude Code session detected
- `session:end` — session ended
- `rating:new` — new rating submitted
- `metric:update` — periodic metrics refresh

### 4.3 — Pipeline Integration (Recording)

**What:** Make the refinement pipeline record every step to the server's database.

**Approach:** The pipeline writes to SQLite directly (not via HTTP). The server reads from the same DB. This avoids an HTTP round-trip in the hot path.

**Changes to `pipeline/pipeline.go`:**
```go
type Recorder interface {
    RecordRefinement(ctx context.Context, r *Refinement) error
    RecordPipelineEvent(ctx context.Context, e *PipelineEvent) error
}
```

- Pipeline accepts an optional `Recorder`
- If no recorder (Ollama-only mode, no server), pipeline works exactly as before
- If recorder present, each stage writes timing + result to DB
- Recorder also publishes to SSE hub for live streaming

**Session tracking:**
- `cmd/refine.go` extracts `session_id` from hook input (available per M1 research)
- Creates or updates session record in DB
- All refinements linked to session via `session_id` foreign key

### 4.4 — React SPA (Vite)

**What:** Monitoring dashboard built with React + Vite.

**Tech stack:**
- React 19 + TypeScript
- Vite for bundling
- Tailwind CSS for styling
- React Router for navigation
- Native `EventSource` for SSE consumption

**Directory structure:**
```
web/
├── index.html
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.ts
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── api/
│   │   ├── client.ts         -- fetch wrapper
│   │   └── sse.ts            -- EventSource hook
│   ├── pages/
│   │   ├── Dashboard.tsx     -- live feed + metrics overview
│   │   ├── Sessions.tsx      -- session browser
│   │   ├── SessionDetail.tsx -- single session with its refinements
│   │   ├── Refinements.tsx   -- refinement list with filtering
│   │   ├── RefinementDetail.tsx -- raw vs refined diff, rating form
│   │   └── SystemPrompt.tsx  -- system prompt version history
│   ├── components/
│   │   ├── LiveFeed.tsx      -- SSE-powered real-time event stream
│   │   ├── PromptDiff.tsx    -- side-by-side raw vs refined
│   │   ├── RatingWidget.tsx  -- 1-5 star rating + feedback textarea
│   │   ├── MetricsCard.tsx   -- single metric with trend
│   │   ├── PipelineTimeline.tsx -- stage-by-stage timing visualization
│   │   └── StatusBadge.tsx   -- health/status indicators
│   └── hooks/
│       ├── useSSE.ts         -- SSE connection management
│       └── useApi.ts         -- data fetching hook
└── dist/                     -- Vite build output (go:embed target)
```

**Key views:**

**Dashboard (/):**
- Live feed of refinements as they happen (SSE)
- Metrics cards: total refinements today, avg latency, cache hit rate, avg rating
- Recent sessions list
- System health indicator (Ollama status, model loaded, DB size)

**Refinement Detail (/refinements/:id):**
- Side-by-side diff: raw prompt → refined prompt
- Pipeline timeline: stage-by-stage timing bars
- Rating widget (1-5 stars + feedback text)
- Session context: which session, what came before/after
- Model and config used

**Sessions (/sessions/:id):**
- All refinements in this session, chronologically
- Session metadata (cwd, duration, transcript link)
- Per-session rating summary

**System Prompt (/system-prompt):**
- Current active system prompt (editable in M9)
- Version history with diff between versions
- Per-version aggregate ratings
- "This version performed X% better than previous"

### 4.5 — Dev Server Flow

**What:** During development, Vite dev server proxies API calls to the Go backend. In production, Go serves the embedded SPA.

**Development:**
```bash
# Terminal 1: Go server with hot reload
make dev-server  # runs: go run ./cli serve --dev --port 8080

# Terminal 2: Vite dev server
cd web && npm run dev  # runs on :5173, proxies /api/* to :8080
```

**vite.config.ts:**
```typescript
export default defineConfig({
  server: {
    proxy: {
      '/api': 'http://localhost:8080'
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true
  }
})
```

**Production (go:embed):**
```go
//go:embed all:web/dist
var webDist embed.FS

func (s *Server) staticHandler() http.Handler {
    sub, _ := fs.Sub(webDist, "web/dist")
    return http.FileServer(http.FS(sub))
}
```

**Build flow:**
```makefile
build-web:
    cd web && npm ci && npm run build

build: build-web
    go build -o plugin/bin/restruct ./cli
```

### 4.6 — Daemon Management

**What:** CLI commands to start/stop/restart the server as a background daemon.

**Commands:**
```bash
restruct serve                    # foreground (dev/debug)
restruct serve --daemon           # background daemon
restruct serve --daemon --port 8080
restruct serve stop               # stop daemon
restruct serve restart            # restart daemon
restruct serve status             # show daemon state
```

**PID file management:**
- PID file location: `$CLAUDE_PLUGIN_DATA/restruct-server.pid` (or `~/.local/share/restruct/server.pid` fallback)
- On `--daemon`: fork process, write PID, detach from terminal
- On `stop`: read PID, send SIGTERM, wait for graceful shutdown, remove PID file
- On `status`: check if PID is alive, print port and uptime
- On startup: check for stale PID file (process dead but file exists), clean up

**Implementation:**
```go
// cmd/serve.go
func daemonize() error {
    // Use os.StartProcess or exec.Command to launch self
    cmd := exec.Command(os.Args[0], "serve", "--foreground", "--port", port)
    cmd.Stdout = logFile
    cmd.Stderr = logFile
    cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach
    if err := cmd.Start(); err != nil {
        return err
    }
    // Write PID
    os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
    fmt.Printf("Server started (PID %d) on port %s\n", cmd.Process.Pid, port)
    return nil
}
```

**Log file:** `$CLAUDE_PLUGIN_DATA/restruct-server.log` — rotated, max 10MB.

### 4.7 — Streaming Ollama Integration

**What:** Switch the Ollama client to streaming mode so the dashboard can show refinement progress in real-time.

**Current:** `stream: false` — waits for complete response.

**New behavior:**
- Ollama client uses `stream: true`
- Tokens stream through a channel
- Pipeline publishes token chunks to SSE hub as `refinement:token` events
- Dashboard shows the refined prompt being generated in real-time
- Final assembled response is still validated and cached as before

**Ollama streaming API:**
```go
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
    // POST to /api/chat with stream: true
    // Parse NDJSON response line-by-line
    // Send each chunk to channel
    // Close channel when done: true received
}

type StreamChunk struct {
    Content string `json:"content"` // token text
    Done    bool   `json:"done"`
}
```

### 4.8 — Hook-to-Server Bridge

**What:** The `restruct refine` command (called by Claude Code hook) communicates with the running server.

**Flow:**
```
Claude Code hook fires
  → restruct refine (stdin: hook JSON)
    → Checks if server is running (PID file + health check)
    → If server running:
        POST /api/internal/refine {session_id, raw_prompt, ...}
        Server: runs pipeline, records to DB, streams to SSE, returns refined prompt
        CLI: outputs hook response JSON
    → If server NOT running:
        Run pipeline directly (current behavior, no recording)
        Print warning to stderr: "restruct server not running, no tracking"
```

**Internal API (not exposed to SPA):**
```
POST /api/internal/refine    -- pipeline execution + recording
POST /api/internal/session   -- session lifecycle events
```

This keeps the hook CLI thin — it's just a bridge to the server. All logic lives in the server.

---

## Acceptance Criteria

- [ ] SQLite schema with migrations, repository pattern data access
- [ ] Chi server with REST API + SSE streaming
- [ ] Pipeline records all refinements and stage timings to DB
- [ ] React SPA with dashboard, session browser, refinement diff, rating widget
- [ ] Vite dev server proxies to Go backend; production uses go:embed
- [ ] `restruct serve --daemon` manages background server with PID file
- [ ] Ollama streaming shows real-time refinement progress
- [ ] Hook CLI bridges to server when running, falls back to direct execution

## New Files

```
server/                          -- Go server package
├── server.go
├── routes.go
├── handlers/*.go
├── sse/hub.go
├── db/*.go
├── migrations/001_initial.sql
└── middleware/logging.go

web/                             -- React SPA
├── package.json
├── vite.config.ts
├── src/**/*.tsx

cli/cmd/serve.go                 -- serve command + daemon management
cli/internal/ollama/stream.go    -- streaming Ollama client
```

## Dependencies Added

- `github.com/go-chi/chi/v5`
- `github.com/go-chi/cors`
- `modernc.org/sqlite`
- React, Vite, Tailwind (npm, frontend only)

## Risk

**High complexity.** This is the largest single milestone. Mitigate by:
1. Build the server skeleton + SQLite layer first (testable without frontend)
2. Build the SPA in parallel once API contracts are defined
3. Daemon management is well-understood (PID files are simple)
4. Streaming Ollama is additive — start with non-streaming server, add streaming after basic flow works

**Frontend scope risk:** Keep the SPA simple. Dashboard + refinement detail + rating is the MVP. Session browser and system prompt views can be stub pages initially.
