# M4: Server & Dashboard

## Goal

Build a Go backend (Chi) + React SPA (Vite) for monitoring, rating, and the feedback loop. The SQLite database is a **shared resource** — the CLI writes to it directly during refinement, and the server reads from it for the dashboard + writes ratings. No server is required for data collection. The server is only needed for the web UI and rating workflow.

## Depends On

M2 (Core Pipeline) — needs a working refinement pipeline to observe.
M1 (Hook Protocol) — needs session_id and transcript_path from hooks.

---

## Architecture Overview

```
┌─────────────────────────────┐    ┌─────────────────────────────┐
│    CLI (restruct refine)    │    │   Server (restruct serve)   │
│                             │    │                             │
│  Writes directly to DB:     │    │  Reads from DB:             │
│  • sessions                 │    │  • sessions                 │
│  • refinements              │    │  • refinements              │
│  • pipeline_events          │    │  • pipeline_events          │
│  • cache                    │    │  • cache                    │
│                             │    │                             │
│  Runs WITHOUT server.       │    │  Writes to DB:              │
│  Every CLI command opens    │    │  • ratings                  │
│  its own SQLite connection  │    │  • system_prompts           │
│  and closes it when done.   │    │                             │
│                             │    │  Serves:                    │
│                             │    │  • React SPA                │
│                             │    │  • REST API (read + rate)   │
│                             │    │  • SSE (live updates)       │
└──────────────┬──────────────┘    └──────────────┬──────────────┘
               │                                  │
               └────────────────┬─────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────┐
│            SQLite (restruct.db) — WAL mode              │
│                                                         │
│  CLI-owned tables (written by CLI commands):            │
│  • sessions, refinements, pipeline_events, cache        │
│                                                         │
│  Server-owned tables (written by dashboard):            │
│  • ratings, system_prompts                              │
│                                                         │
│  Multiple concurrent readers always OK.                 │
│  WAL mode allows one writer + many readers.             │
│  Partitioned ownership avoids write contention.         │
└─────────────────────────────────────────────────────────┘
```

**Key principle: The DB is not the server's DB. It's a shared data store.** The CLI writes refinement data on every hook invocation whether or not the server is running. The server is purely a read+rate UI layer. This means:
- Data collection works from day one, before the server is even built
- Multiple CLI instances (concurrent Claude sessions) write concurrently via WAL mode
- The server can be started/stopped without losing any data
- No "offline sync" or "startup reconciliation" needed — there's nothing to sync

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

### Data Flow (No Server Required)

```
Hook fires → restruct refine
  ├── Opens SQLite connection (WAL mode)
  ├── Writes session record (upsert)
  ├── Writes refinement record + pipeline events
  ├── Writes to cache table
  ├── Closes SQLite connection
  └── Also writes .restruct/sessions/<id>.json (fast local session lookup)
```

**Every CLI command manages its own SQLite connection.** Open → write → close. No long-lived connections, no connection pool needed on the CLI side. SQLite WAL mode allows this to coexist with concurrent readers (the server dashboard) and other CLI writers (concurrent Claude sessions).

**Write ownership is partitioned to avoid contention:**

| Table | Written by | Read by |
|-------|-----------|---------|
| `sessions` | CLI (`refine`, `session start/end`) | Server (dashboard) |
| `refinements` | CLI (`refine`) | Server (dashboard, rating association) |
| `pipeline_events` | CLI (`refine`) | Server (dashboard) |
| `cache` | CLI (`refine`) | CLI (cache lookup), Server (analytics) |
| `ratings` | Server (dashboard POST) | Server (dashboard), M10 (analysis) |
| `system_prompts` | Server (dashboard POST) | CLI (active prompt selection), Server |

No two processes write to the same table in the hot path. The CLI writes refinement data; the server writes user feedback. This eliminates write contention without any coordination protocol.

**`.restruct/sessions/` files** are retained as a fast local index for the CLI to check "is this session already tracked?" without opening SQLite on every invocation. They are NOT the source of truth — SQLite is. The local files are a performance optimization only.

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
├── routes.go           -- Route registration (all read + rate endpoints)
├── handlers/
│   ├── sessions.go     -- Session reads
│   ├── refinements.go  -- Refinement reads + rating writes
│   ├── metrics.go      -- Aggregated metrics (computed from DB)
│   ├── events.go       -- SSE stream handler
│   ├── health.go       -- Health check (Ollama status, DB stats)
│   ├── config.go       -- Config endpoint
│   └── prompts.go      -- System prompt management (read/write)
├── sse/
│   └── hub.go          -- SSE broadcast hub (fed by DB poller)
├── poller/
│   └── poller.go       -- Polls DB for new refinements, broadcasts to SSE
└── middleware/
    └── logging.go      -- Request logging middleware
```

The server has NO db/ package — it uses `cli/internal/db` (the shared package). The DB layer is not owned by the server.

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

### 4.3 — Pipeline Recording (Direct to SQLite)

**What:** The refinement pipeline writes every step directly to SQLite. No server involvement.

**Implementation:**
```go
// internal/db/db.go — shared across CLI and server
type DB struct {
    pool *sql.DB // SQLite with WAL mode, busy_timeout=5000
}

func Open(path string) (*DB, error) {
    pool, err := sql.Open("sqlite", path+"?_journal=WAL&_busy_timeout=5000")
    // Run migrations on first open
    ...
}

type Recorder struct {
    db *DB
}

func (r *Recorder) RecordRefinement(ctx context.Context, ref *Refinement) error { ... }
func (r *Recorder) RecordPipelineEvent(ctx context.Context, evt *PipelineEvent) error { ... }
func (r *Recorder) UpsertSession(ctx context.Context, sess *Session) error { ... }
```

- The `internal/db` package is shared by both CLI commands and the server
- CLI opens a connection, writes, closes — no long-lived connection
- Server opens a long-lived connection for reads and rating writes
- `busy_timeout=5000` handles the rare case where CLI and server write simultaneously
- Pipeline accepts a `Recorder` (nil = no recording, for testing)

**Session tracking:**
- `cmd/refine.go` extracts `session_id` from hook input
- Upserts session record directly in SQLite
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

### 4.8 — SSE Live Updates (DB Polling)

**What:** The server streams live updates to the dashboard via SSE, powered by polling the SQLite DB for new records.

**Approach:** The server polls the `refinements` and `ratings` tables on a short interval (1-2s) for new records since the last check. When new rows appear, it broadcasts SSE events to connected clients.

```go
// Background goroutine in the server
func (s *Server) pollForUpdates(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Second)
    var lastRefinementID int64
    for {
        select {
        case <-ticker.C:
            // SELECT * FROM refinements WHERE id > lastRefinementID
            newRefinements := s.db.GetRefinementsSince(lastRefinementID)
            for _, r := range newRefinements {
                s.sseHub.Broadcast(Event{Type: "refinement:new", Data: r})
                lastRefinementID = r.ID
            }
        case <-ctx.Done():
            return
        }
    }
}
```

**Why polling instead of SQLite update hooks?** `modernc.org/sqlite` (pure Go) doesn't reliably support `sqlite3_update_hook` across connections. Polling at 1s is simple, reliable, and more than fast enough for a monitoring dashboard. The DB is local — a SELECT with an index on `id` is sub-millisecond.

**No internal API needed.** The CLI never talks to the server. It writes to SQLite. The server discovers new data by polling. This is the simplest possible architecture with zero coordination.

---

## Acceptance Criteria

- [ ] Shared `internal/db` package with WAL mode, used by both CLI and server
- [ ] SQLite schema with migrations, repository pattern data access
- [ ] CLI writes refinements/sessions/pipeline_events directly to SQLite (no server needed)
- [ ] Chi server reads from SQLite, writes only ratings and system_prompts
- [ ] SSE live updates via DB polling (1s interval)
- [ ] React SPA with dashboard, session browser, refinement diff, rating widget
- [ ] Vite dev server proxies to Go backend; production uses go:embed
- [ ] `restruct serve --daemon` manages background server with PID file
- [ ] Ollama streaming shows real-time refinement progress in dashboard

## New Files

```
cli/internal/db/                 -- Shared DB package (CLI + server)
├── db.go                        -- Open, migrate, WAL config
├── sessions.go                  -- Session CRUD
├── refinements.go               -- Refinement CRUD
├── ratings.go                   -- Rating CRUD (server-side writes)
├── events.go                    -- Pipeline event CRUD
├── prompts.go                   -- System prompt CRUD
├── cache.go                     -- Cache table CRUD
└── migrations/001_initial.sql

server/                          -- Go server package (reads DB + writes ratings)
├── server.go
├── routes.go
├── handlers/*.go
├── sse/hub.go
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
