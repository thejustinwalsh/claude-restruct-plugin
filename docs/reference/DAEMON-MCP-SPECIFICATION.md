# Daemon & MCP Server Specification

The daemon is a single long-running Go process that owns all indexes, watches the filesystem, and serves every client through multiple listeners. The MCP shim is a thin short-lived proxy that Claude Code spawns as a child process, connecting to the daemon over a Unix socket.

## Design Principle

> **One process, multiple ears, shared state.**
>
> Every consumer — CLI, MCP, web UI, prompt builder — talks to the same daemon over the same socket (or HTTP for the browser). Indexes are loaded once into memory. One file watcher. One idle timer. One thing to debug.

---

## Process Architecture

```
LONG-RUNNING (one process):
┌──────────────────────────────────────────────────┐
│  prompt-cli daemon                                │
│                                                   │
│  ┌─────────────────────────────────────────────┐  │
│  │  Shared State                                │  │
│  │                                              │  │
│  │  TrigramIndex   map[uint32][]uint32          │  │
│  │  WordIndex      map[string][]WordHit         │  │
│  │  FileTree       in-memory directory cache    │  │
│  │  Outlines       map[fileID][]Symbol          │  │
│  │  Embeddings     []TurnVec (float32×768)      │  │
│  │  RuleEmbeddings []RuleVec                    │  │
│  │  SQLite         *sql.DB (context.db)         │  │
│  └─────────────────────────────────────────────┘  │
│                                                   │
│  ┌──────────┐  ┌──────────┐  ┌────────────────┐  │
│  │  Unix    │  │  HTTP    │  │  File watcher  │  │
│  │  socket  │  │  :7719   │  │  (fsnotify)    │  │
│  │          │  │          │  │                │  │
│  │  NDJSON  │  │  REST +  │  │  Incremental   │  │
│  │  proto   │  │  static  │  │  re-index on   │  │
│  │          │  │  assets  │  │  file change   │  │
│  └──────────┘  └──────────┘  └────────────────┘  │
│                                                   │
│  Idle watchdog: shuts down after 30min no activity│
│  Signal handler: SIGTERM/SIGINT clean shutdown    │
└──────────────────────────────────────────────────┘

SHORT-LIVED (spawned on demand, no state):
┌────────────────────────────────┐
│  prompt-cli mcp                 │  ← Claude Code spawns
│  stdin/stdout ↔ JSON-RPC 2.0  │
│  connects to daemon socket     │
│  pure proxy                    │
└────────────────────────────────┘

┌────────────────────────────────┐
│  prompt-cli search "foo"        │  ← user types
│  connects to daemon socket     │
│  prints results, exits         │
└────────────────────────────────┘

┌────────────────────────────────┐
│  prompt-cli prompt "make..."    │  ← prompt builder
│  connects to daemon socket     │
│  also calls Ollama + Claude    │
│  exits                         │
└────────────────────────────────┘
```

---

## Daemon Lifecycle

### Startup

Every CLI subcommand that needs the daemon calls `ensureDaemon()` before doing anything:

```go
func ensureDaemon() (*net.UnixConn, error) {
    // 1. Try to connect to existing daemon
    conn, err := net.Dial("unix", socketPath())
    if err == nil {
        return conn, nil
    }

    // 2. Check for stale PID file
    if pidAlive(pidPath()) {
        // PID exists but socket gone — zombie, kill it
        killPID(pidPath())
    }

    // 3. Fork daemon in background
    cmd := exec.Command(os.Args[0], "daemon", "run",
        "--repo", repoRoot(),
    )
    cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
    cmd.Start()

    // 4. Wait for socket to appear
    for i := 0; i < 60; i++ {
        time.Sleep(50 * time.Millisecond)
        conn, err = net.Dial("unix", socketPath())
        if err == nil {
            return conn, nil
        }
    }
    return nil, fmt.Errorf("daemon failed to start within 3s")
}
```

### Daemon `run` Subcommand

```go
func runDaemon(repoPath string, httpAddr string) {
    // Write PID file
    writePID(pidPath())
    defer removePID(pidPath())

    // Open SQLite
    db := openDB(dbPath())
    defer db.Close()

    // Build indexes (cold start)
    idx := index.New(repoPath, db)
    idx.Build()  // trigram + word + outlines + tree

    // Load embeddings into memory
    idx.LoadEmbeddings()

    // Start file watcher
    go idx.Watch()  // fsnotify, incremental re-index

    // Start listeners
    go listenUnixSocket(idx, db)
    go listenHTTP(idx, db, httpAddr)

    // Idle watchdog
    go idleWatchdog(30 * time.Minute)

    // Wait for shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    select {
    case <-sigCh:
    case <-shutdownCh:
    }

    // Cleanup
    removeSocket()
    removePID(pidPath())
    db.Close()
}
```

### Shutdown Triggers

| Trigger | How |
|---|---|
| Idle timeout (30min default) | Background goroutine checks `lastActivity` every 30s |
| Explicit stop | `prompt-cli daemon stop` sends `{"method":"shutdown"}` over socket |
| Signal | SIGTERM/SIGINT from OS, launchd, systemd, or kill |
| Web UI button | POST `/api/shutdown` (localhost only) |

### Activity Tracking

Every request from any listener resets the idle timer:

```go
var lastActivity atomic.Value // stores time.Time

func resetIdle() {
    lastActivity.Store(time.Now())
}

func idleWatchdog(timeout time.Duration) {
    for {
        time.Sleep(30 * time.Second)
        last := lastActivity.Load().(time.Time)
        if time.Since(last) > timeout {
            log.Info("idle timeout, shutting down")
            close(shutdownCh)
            return
        }
    }
}
```

WebSocket connections from the web UI send heartbeat pings — these count as activity. Closing the browser tab stops the heartbeats, and the idle timer begins.

### File Paths

| Path | Contents |
|---|---|
| `~/.cache/prompt/daemon.sock` | Unix domain socket |
| `~/.cache/prompt/daemon.pid` | PID file |
| `~/.cache/prompt/context.db` | SQLite database (indexes, embeddings, rules, transcripts) |
| `.mcp.json` (repo root) | MCP server registration for Claude Code |

The socket and PID paths can be overridden with `PROMPT_SOCKET_PATH` and `PROMPT_PID_PATH` environment variables.

---

## Unix Socket Protocol

### Transport

Unix domain socket, newline-delimited JSON (NDJSON). One JSON object per line, terminated by `\n`. Requests and responses are synchronous per connection — send a request, read one response.

```
Client                          Daemon
  │                               │
  │ {"method":"search",           │
  │  "params":{"query":"foo"}}    │
  │──────────────────────────────►│
  │                               │
  │ {"result":[...]}              │
  │◄──────────────────────────────│
  │                               │
```

### Request Format

```json
{
  "method": "search",
  "params": {
    "query": "handleAuth",
    "max": 10
  }
}
```

### Response Format

Success:
```json
{
  "result": [
    {"file": "src/server.go", "line": 42, "context": "func handleAuth(w http.ResponseWriter, r *http.Request) {"}
  ]
}
```

Error:
```json
{
  "error": {"code": -1, "message": "index not ready"}
}
```

### Methods

#### search

Trigram-accelerated full-text search across all indexed files.

```json
// Request
{"method": "search", "params": {"query": "handleAuth", "glob": "*.go", "max": 10}}

// Response
{"result": [
  {"file": "src/server.go", "line": 42, "context": "func handleAuth(w http.ResponseWriter, r *http.Request) {", "context_before": ["", "// handleAuth validates the JWT token"], "context_after": ["\ttoken := r.Header.Get(\"Authorization\")"]},
]}
```

| Param | Type | Required | Default | Description |
|---|---|---|---|---|
| query | string | yes | | Substring or pattern to search |
| glob | string | no | `*` | File glob filter |
| max | int | no | 10 | Maximum results |

#### word

O(1) inverted word index lookup. Exact identifier match.

```json
// Request
{"method": "word", "params": {"word": "AgentRegistry"}}

// Response
{"result": [
  {"file": "src/agent.go", "line": 30},
  {"file": "src/main.go", "line": 15}
]}
```

| Param | Type | Required | Description |
|---|---|---|---|
| word | string | yes | Exact word/identifier to find |

#### tree

File tree with metadata. Cached in memory, rebuilt on file changes.

```json
// Request
{"method": "tree", "params": {"path": "packages/editor", "depth": 2}}

// Response
{"result": {
  "entries": [
    {"path": "packages/editor/src", "type": "dir", "children": [
      {"path": "packages/editor/src/components", "type": "dir", "file_count": 12},
      {"path": "packages/editor/src/hooks", "type": "dir", "file_count": 5}
    ]},
    {"path": "packages/editor/package.json", "type": "file", "size": 1842, "mod_time": 1711900000}
  ]
}}
```

| Param | Type | Required | Default | Description |
|---|---|---|---|---|
| path | string | no | `.` | Directory relative to repo root |
| depth | int | no | 2 | Max depth |
| glob | string | no | `*` | Filter pattern |

#### outline

Structural symbols in a file. Powered by tree-sitter (when available) or regex fallback.

```json
// Request
{"method": "outline", "params": {"path": "src/server.go"}}

// Response
{"result": {
  "path": "src/server.go",
  "language": "go",
  "symbols": [
    {"name": "Server", "kind": "struct", "line": 12},
    {"name": "NewServer", "kind": "function", "line": 25},
    {"name": "handleAuth", "kind": "function", "line": 42, "receiver": "Server"},
    {"name": "Start", "kind": "function", "line": 89, "receiver": "Server"}
  ],
  "imports": ["net/http", "encoding/json", "github.com/you/pkg/auth"]
}}
```

| Param | Type | Required | Description |
|---|---|---|---|
| path | string | yes | File path relative to repo root |

#### read

Read file contents. Supports line ranges for large files.

```json
// Request
{"method": "read", "params": {"path": "src/server.go", "start_line": 40, "end_line": 60}}

// Response
{"result": {
  "path": "src/server.go",
  "start_line": 40,
  "end_line": 60,
  "content": "// handleAuth validates...\nfunc handleAuth(..."
}}
```

| Param | Type | Required | Default | Description |
|---|---|---|---|---|
| path | string | yes | | File path |
| start_line | int | no | 1 | Start line (1-indexed) |
| end_line | int | no | EOF | End line (inclusive) |

#### deps

Reverse dependency lookup. Which files import this file.

```json
// Request
{"method": "deps", "params": {"path": "src/auth/jwt.go"}}

// Response
{"result": [
  {"file": "src/server.go", "import": "github.com/you/pkg/auth"},
  {"file": "src/middleware.go", "import": "github.com/you/pkg/auth"}
]}
```

| Param | Type | Required | Description |
|---|---|---|---|
| path | string | yes | File to find dependents of |

#### search_context

Hybrid semantic + keyword search over past conversation transcripts.

```json
// Request
{"method": "search_context", "params": {"query": "drag and drop decision", "max": 3}}

// Response
{"result": [
  {
    "turn_id": 87,
    "relevance": 0.91,
    "user": "Should we use react-beautiful-dnd or @dnd-kit?",
    "assistant": "We settled on @dnd-kit because react-beautiful-dnd is unmaintained..."
  }
]}
```

| Param | Type | Required | Default | Description |
|---|---|---|---|---|
| query | string | yes | | What to search for |
| max | int | no | 3 | Maximum results |

Requires nomic-embed-text via Ollama for semantic matching. Falls back to FTS5 keyword search if Ollama is unavailable.

#### rules

Score and retrieve relevant project rules for a given query and optional file paths.

```json
// Request
{"method": "rules", "params": {
  "query": "make tracks reorderable",
  "files": ["packages/editor/src/components/TrackEditor.tsx"],
  "max": 5
}}

// Response
{"result": [
  {
    "rule_id": "editor/drag-and-drop",
    "score": 0.94,
    "path": "packages/editor/.rules/drag-and-drop.md",
    "summary": "Governs all drag-and-drop interactions. Mandates @dnd-kit with keyboard accessibility.",
    "content": "## Drag and Drop\n\nUse @dnd-kit for all...",
    "dependencies": ["editor/accessibility", "editor/state-management"]
  },
  {
    "rule_id": "editor/component-structure",
    "score": 0.72,
    "path": "packages/editor/.rules/component-structure.md",
    "summary": "React component patterns for the editor package.",
    "content": "## Component Structure\n\n..."
  }
]}
```

| Param | Type | Required | Default | Description |
|---|---|---|---|---|
| query | string | yes | | User intent or task description |
| files | []string | no | `[]` | File paths for glob matching against `applies_to` |
| max | int | no | 5 | Maximum rules to return |

Uses three-signal scoring (path match, FTS5 keyword, semantic embedding) with dependency expansion as specified in the architecture doc.

#### status

Daemon health and index statistics.

```json
// Request
{"method": "status", "params": {}}

// Response
{"result": {
  "uptime_seconds": 3421,
  "repo": "/Users/justin/Developer/monorepo",
  "files_indexed": 847,
  "trigram_entries": 124832,
  "word_entries": 53291,
  "turns_indexed": 342,
  "rules_indexed": 28,
  "last_file_change": "2026-04-01T14:23:01Z",
  "ollama_available": true,
  "sqlite_size_bytes": 14532608
}}
```

#### shutdown

Graceful shutdown. Daemon flushes pending writes, removes socket, exits.

```json
// Request
{"method": "shutdown", "params": {}}

// Response
{"result": {"ok": true}}
// (connection closes)
```

---

## MCP Shim

### What It Is

`prompt-cli mcp` is a short-lived process that Claude Code spawns as a child. It speaks JSON-RPC 2.0 over stdio (MCP protocol) and proxies every request to the daemon over the Unix socket. It holds no state.

### Registration

**Via `.mcp.json` in repo root (automatic for team):**

```json
{
  "mcpServers": {
    "codedb": {
      "command": "prompt-cli",
      "args": ["mcp"]
    }
  }
}
```

**Via plugin (automatic on plugin install):**

The plugin writes this entry to `.mcp.json` or `~/.claude.json` as part of its hook registration. No user action needed.

**Via CLI (manual, one-time):**

```bash
claude mcp add --scope project codedb -- prompt-cli mcp
```

### Startup Sequence

```
Claude Code starts
       │
       ▼
Reads .mcp.json, finds "codedb"
       │
       ▼
Spawns: prompt-cli mcp
       │
       ▼
┌──────────────────────────┐
│  prompt-cli mcp           │
│                           │
│  1. ensureDaemon()        │ ← starts daemon if needed
│  2. Connect to socket     │
│  3. Send MCP initialize   │
│     response to stdout    │
│  4. Enter JSON-RPC loop:  │
│     read stdin → translate│
│     → send to daemon      │
│     → read response       │
│     → translate → write   │
│       stdout              │
│  5. On stdin EOF: exit    │
└──────────────────────────┘
```

### MCP Tool Definitions

The shim responds to `tools/list` with these tool definitions. Claude Code sees them as `mcp__codedb__<name>`.

```json
{
  "tools": [
    {
      "name": "search",
      "description": "Trigram-accelerated full-text search across the codebase. Returns matching lines with file path, line number, and surrounding context. Use for finding function usages, string patterns, or any text across files.",
      "inputSchema": {
        "type": "object",
        "required": ["query"],
        "properties": {
          "query": {"type": "string", "description": "Text to search for"},
          "glob": {"type": "string", "description": "File glob filter, e.g. '*.go' or 'packages/editor/**'"},
          "max": {"type": "integer", "description": "Maximum results (default 10)"}
        }
      }
    },
    {
      "name": "word",
      "description": "O(1) exact identifier lookup. Instantly finds all locations where a specific word/identifier appears. Use for finding where a function, type, variable, or import is used.",
      "inputSchema": {
        "type": "object",
        "required": ["word"],
        "properties": {
          "word": {"type": "string", "description": "Exact identifier to find"}
        }
      }
    },
    {
      "name": "tree",
      "description": "File tree with metadata. Shows directory structure, file counts, and sizes. Use to orient yourself in the codebase before diving into files.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "path": {"type": "string", "description": "Directory path relative to repo root (default: root)"},
          "depth": {"type": "integer", "description": "Max directory depth (default 2)"},
          "glob": {"type": "string", "description": "Filter pattern"}
        }
      }
    },
    {
      "name": "outline",
      "description": "Structural symbols in a file: functions, types, structs, imports, with line numbers. Use before editing a file to understand its structure without reading the entire file.",
      "inputSchema": {
        "type": "object",
        "required": ["path"],
        "properties": {
          "path": {"type": "string", "description": "File path relative to repo root"}
        }
      }
    },
    {
      "name": "read",
      "description": "Read file contents or a specific line range. Use start_line/end_line for large files to avoid loading unnecessary content.",
      "inputSchema": {
        "type": "object",
        "required": ["path"],
        "properties": {
          "path": {"type": "string", "description": "File path relative to repo root"},
          "start_line": {"type": "integer", "description": "Start line, 1-indexed"},
          "end_line": {"type": "integer", "description": "End line, inclusive"}
        }
      }
    },
    {
      "name": "deps",
      "description": "Reverse dependency lookup. Shows which files import or depend on the given file. Use to understand impact before modifying a file.",
      "inputSchema": {
        "type": "object",
        "required": ["path"],
        "properties": {
          "path": {"type": "string", "description": "File to find dependents of"}
        }
      }
    },
    {
      "name": "search_context",
      "description": "Search past conversations for relevant prior decisions, approaches, or constraints. Returns matching prompt/response pairs ranked by relevance. Use to check if a topic was previously discussed before making decisions.",
      "inputSchema": {
        "type": "object",
        "required": ["query"],
        "properties": {
          "query": {"type": "string", "description": "Topic, decision, or concept to search for"},
          "max": {"type": "integer", "description": "Maximum results (default 3)"}
        }
      }
    },
    {
      "name": "rules",
      "description": "Find project rules and conventions relevant to a task. Returns scored rules with full contents. Use before implementing features to ensure code follows project conventions.",
      "inputSchema": {
        "type": "object",
        "required": ["query"],
        "properties": {
          "query": {"type": "string", "description": "Task description or intent"},
          "files": {"type": "array", "items": {"type": "string"}, "description": "File paths involved in the task (improves rule matching)"},
          "max": {"type": "integer", "description": "Maximum rules (default 5)"}
        }
      }
    },
    {
      "name": "status",
      "description": "Daemon health and index statistics. Shows file count, index sizes, uptime, and Ollama availability.",
      "inputSchema": {
        "type": "object",
        "properties": {}
      }
    }
  ]
}
```

### JSON-RPC Translation

The MCP shim translates between MCP JSON-RPC and the daemon's NDJSON protocol:

```
MCP (stdin) → Daemon (socket):

{"jsonrpc":"2.0","method":"tools/call",           {"method":"search",
 "params":{"name":"search",                  →     "params":{"query":"foo",
           "arguments":{"query":"foo",                       "max":10}}
                        "max":10}},
 "id":1}


Daemon (socket) → MCP (stdout):

{"result":[                                       {"jsonrpc":"2.0",
  {"file":"src/a.go","line":42,               →    "result":{"content":[
   "context":"func foo() {"}                         {"type":"text",
]}                                                    "text":"src/a.go:42 func foo() {"}
                                                   ]},
                                                   "id":1}
```

The shim formats daemon results into MCP `content` blocks. Each result is rendered as human-readable text that Claude can parse. For structured results (like `outline` or `rules`), the shim formats them as readable text rather than raw JSON — Claude works better with formatted text than deeply nested JSON.

---

## HTTP Server (Observability)

### Endpoints

All read-only. Serves a static dashboard SPA plus a JSON API.

| Method | Path | Description |
|---|---|---|
| GET | `/` | Dashboard SPA (static HTML/JS/CSS) |
| GET | `/api/status` | Same as daemon `status` method |
| GET | `/api/stats` | Query latency histograms, request counts |
| GET | `/api/rules` | List all enriched rules with metadata |
| GET | `/api/turns` | Browse indexed transcript turns |
| GET | `/api/files` | File index with trigram/word counts per file |
| GET | `/api/health` | `{"ok": true}` for monitoring |
| WS | `/ws` | WebSocket for live file change events and index updates |

### Configuration

```bash
prompt-cli daemon --http=:7719         # default
prompt-cli daemon --http=:0            # random port, printed to stdout
prompt-cli daemon --http=false         # disable web server entirely
```

HTTP binds to `localhost` only. No auth needed for local observability.

---

## Indexing

### Cold Start

On first `daemon run`, or when `context.db` doesn't exist:

```
1. Walk repo (respecting .gitignore)           ~50ms for 1000 files
2. Read all file contents                      ~100ms
3. Build trigram index                         ~150ms
4. Build word index                            ~100ms
5. Parse outlines (regex or tree-sitter)       ~200ms
6. Load existing embeddings from SQLite        ~10ms
7. Total cold start                            ~500ms for 1000 files
```

### Incremental (file watcher)

On fsnotify event:

```
1. Read changed file                           ~1ms
2. Remove old trigrams for this file           ~0.1ms
3. Extract + insert new trigrams               ~1ms
4. Update word index                           ~0.5ms
5. Re-parse outline                            ~1ms
6. Update file tree cache                      ~0.1ms
7. Total per file change                       ~3-5ms
```

### Transcript & Rule Indexing

Handled by `prompt-cli index` subcommand, which connects to the daemon and triggers re-indexing. Can also run standalone (daemon not required — writes directly to SQLite).

```bash
prompt-cli index transcripts ./transcripts/    # index new turns
prompt-cli index rules                         # re-enrich changed rules
prompt-cli index all                           # everything
```

The daemon exposes an `index` method over the socket for programmatic triggering:

```json
{"method": "index", "params": {"what": "rules"}}
{"method": "index", "params": {"what": "transcripts", "path": "./transcripts/"}}
```

---

## File Watcher

Uses `github.com/fsnotify/fsnotify` for kernel-level events (inotify on Linux, FSEvents on macOS, ReadDirectoryChanges on Windows).

### Filtered Paths

Ignores by default:

```
.git/
node_modules/
zig-cache/
zig-out/
__pycache__/
.next/
dist/
build/
vendor/
*.lock
*.sum
```

Respects `.gitignore` at repo root and in subdirectories. Additional ignores configurable via `.promptignore` file at repo root.

### Event Handling

```go
func (idx *Index) Watch() {
    watcher, _ := fsnotify.NewWatcher()
    defer watcher.Close()

    // Add all directories (not files — fsnotify watches dirs)
    filepath.WalkDir(idx.repoPath, func(path string, d fs.DirEntry, err error) error {
        if d.IsDir() && !idx.isIgnored(path) {
            watcher.Add(path)
        }
        return nil
    })

    // Debounce: batch rapid changes (e.g. git checkout)
    var debounceTimer *time.Timer

    for {
        select {
        case event := <-watcher.Events:
            if idx.isIgnored(event.Name) {
                continue
            }
            // Debounce: wait 100ms for more events before re-indexing
            if debounceTimer != nil {
                debounceTimer.Stop()
            }
            debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
                idx.reindexFile(event.Name)
                resetIdle()
            })

        case <-idx.stopCh:
            return
        }
    }
}
```

### New Directory Handling

When a new directory is created, the watcher adds it to the watch list and indexes all files within it. When a directory is deleted, its files are removed from all indexes.

---

## Data Structures

### Trigram Index

```go
type TrigramIndex struct {
    mu       sync.RWMutex
    postings map[uint32]*roaring.Bitmap  // trigram → file IDs (roaring bitmap)
    files    []FileEntry                 // file ID → path, size, mod_time
    pathToID map[string]uint32           // path → file ID
}

// Trigram from 3 bytes packed into uint32
func trigram(a, b, c byte) uint32 {
    return uint32(a)<<16 | uint32(b)<<8 | uint32(c)
}
```

Using roaring bitmaps for posting lists instead of `[]uint32` slices gives better memory efficiency and faster intersection for large repos. The `github.com/RoaringBitmap/roaring` package is pure Go.

### Word Index

```go
type WordIndex struct {
    mu    sync.RWMutex
    index map[string][]WordHit
}

type WordHit struct {
    FileID uint32
    Line   uint32
}
```

Words are extracted by splitting on non-alphanumeric boundaries, preserving camelCase splits (`handleAuth` → `handle`, `Auth`, `handleAuth`), and keeping special identifiers intact (`@dnd-kit` stays as one token).

### File Tree

```go
type FileTree struct {
    mu   sync.RWMutex
    root *TreeNode
}

type TreeNode struct {
    Name     string
    Path     string
    IsDir    bool
    Size     int64
    ModTime  time.Time
    Children []*TreeNode
    Symbols  int  // count from outline, 0 for dirs
    Language string
}
```

Rebuilt incrementally on file watcher events. Serialized to JSON on `tree` requests.

### Symbol Outlines

```go
type OutlineCache struct {
    mu       sync.RWMutex
    outlines map[uint32]FileOutline  // file ID → outline
}

type FileOutline struct {
    Language string
    Symbols  []Symbol
    Imports  []string
    Exports  []string
}

type Symbol struct {
    Name     string
    Kind     string  // "function", "struct", "type", "const", "var", "interface", "component"
    Line     int
    Receiver string  // Go method receiver, empty for functions
}
```

Parsed with tree-sitter via `gotreesitter` (pure Go) when available for the file's language. Falls back to regex extraction for unsupported languages.

---

## Thread Model

| Thread / Goroutine | Role | Lifetime |
|---|---|---|
| Main | Daemon setup, signal handling, shutdown coordination | Process lifetime |
| Unix socket listener | Accept loop, spawns per-connection goroutine | Process lifetime |
| Per-connection handler | Read request, dispatch, write response, close | Per request |
| HTTP server | `http.ListenAndServe` on localhost | Process lifetime |
| File watcher | fsnotify event loop with debounce | Process lifetime |
| Idle watchdog | Checks `lastActivity` every 30s | Process lifetime |
| Index rebuild | Triggered by watcher or `index` command, holds write lock briefly | Per event |

All shared state is protected by `sync.RWMutex`. Reads (queries) take read locks and run concurrently. Writes (re-index) take write locks and block briefly. The typical write lock duration is <5ms for a single file re-index.

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PROMPT_SOCKET_PATH` | `~/.cache/prompt/daemon.sock` | Unix socket path |
| `PROMPT_PID_PATH` | `~/.cache/prompt/daemon.pid` | PID file path |
| `PROMPT_DB_PATH` | `~/.cache/prompt/context.db` | SQLite database path |
| `PROMPT_HTTP_ADDR` | `:7719` | HTTP listen address (`false` to disable) |
| `PROMPT_IDLE_TIMEOUT` | `30m` | Idle shutdown timeout |
| `PROMPT_REPO` | `.` (cwd) | Repository root path |
| `OLLAMA_HOST` | `http://localhost:11434` | Ollama API endpoint |

### CLI Flags

```bash
prompt-cli daemon run [flags]

Flags:
  --repo string        Repository root (default: current directory)
  --http string        HTTP listen address (default: ":7719", "false" to disable)
  --idle-timeout dur   Idle shutdown timeout (default: 30m)
  --verbose            Verbose logging
```

---

## Dependencies

| Package | Purpose | CGo? |
|---|---|---|
| `github.com/ollama/ollama/api` | Ollama SDK for embeddings | No |
| `modernc.org/sqlite` | Pure Go SQLite with FTS5 | No |
| `github.com/fsnotify/fsnotify` | Kernel-level file watching | No |
| `github.com/RoaringBitmap/roaring` | Compressed bitmap posting lists | No |
| `github.com/bmatcuk/doublestar` | Glob matching for rule `applies_to` | No |
| `github.com/odvcencio/gotreesitter` | Pure Go tree-sitter (optional) | No |

Zero CGo. Single binary. Cross-compiles to any platform Go supports.

---

## Directory Structure

```
tools/prompt-cli/
├── cmd/
│   ├── root.go              # cobra root command
│   ├── daemon.go            # `daemon run` / `daemon stop`
│   ├── mcp.go               # `mcp` — MCP shim for Claude Code
│   ├── search.go            # `search` — CLI search command
│   ├── index.go             # `index` — trigger indexing
│   ├── prompt.go            # `prompt` — prompt builder pipeline
│   ├── tree.go              # `tree` — file tree
│   ├── outline.go           # `outline` — file symbols
│   └── setup.go             # `setup` — install + register MCP
│
├── internal/
│   ├── daemon/
│   │   ├── daemon.go        # main daemon loop, lifecycle
│   │   ├── socket.go        # Unix socket listener + NDJSON protocol
│   │   ├── http.go          # HTTP server (observability)
│   │   ├── dispatch.go      # method routing
│   │   └── idle.go          # idle watchdog
│   │
│   ├── index/
│   │   ├── trigram.go       # trigram index
│   │   ├── wordidx.go       # inverted word index
│   │   ├── filetree.go      # in-memory file tree
│   │   ├── outline.go       # symbol extraction (tree-sitter + regex)
│   │   ├── watcher.go       # fsnotify file watcher
│   │   ├── deps.go          # import/dependency graph
│   │   └── index.go         # unified index (owns all sub-indexes)
│   │
│   ├── search/
│   │   ├── embeddings.go    # cosine similarity, vector I/O
│   │   ├── fts.go           # FTS5 queries
│   │   └── hybrid.go        # score fusion for transcript + rule search
│   │
│   ├── scorer/
│   │   └── rules.go         # three-signal rule scoring
│   │
│   ├── mcp/
│   │   ├── server.go        # JSON-RPC 2.0 stdio loop
│   │   ├── translate.go     # MCP ↔ daemon protocol translation
│   │   └── tools.go         # tool definitions
│   │
│   ├── prompt/
│   │   ├── refine.go        # qwen3 refinement agent call
│   │   └── assemble.go      # final prompt assembly for Claude
│   │
│   └── db/
│       └── sqlite.go        # schema, migrations, queries
│
├── go.mod
└── go.sum
```

---

## Claude Code Skill File

Place at `.claude/skills/codedb.md` in the monorepo to teach Claude how to use the tools effectively:

```markdown
# Codebase Intelligence Tools

This project has a codebase intelligence daemon that provides indexed
search, structural parsing, and project rule lookup. Use these tools
instead of grep or manual file browsing.

## Before implementing any feature:

1. **Find applicable rules first.**
   Call mcp__codedb__rules with a description of the task and any
   file paths involved. Read the returned rules carefully — they
   contain required libraries, prohibited patterns, and architectural
   constraints.

2. **Check prior decisions.**
   Call mcp__codedb__search_context to find if this topic was
   previously discussed. Prior decisions should be respected unless
   there's a good reason to change.

3. **Understand file structure before editing.**
   Call mcp__codedb__outline on any file before modifying it. This
   shows you the functions, types, and imports without reading the
   entire file.

4. **Check impact before modifying.**
   Call mcp__codedb__deps to see what other files depend on the file
   you're about to change.

## Search tips:

- Use mcp__codedb__word for exact identifier lookups (function names,
  type names, variable names). It's O(1) and instant.
- Use mcp__codedb__search for substring/pattern matching when you
  don't know the exact identifier.
- Use mcp__codedb__tree to browse directory structure when you need
  to orient yourself.
```
