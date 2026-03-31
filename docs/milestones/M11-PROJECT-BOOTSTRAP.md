# M11: Project Bootstrap & Deep Context System

## Goal

At SessionStart, scan the project for all CLAUDE.md and rules files, pre-process them into categorized "deep context documents" with keyword indices and summaries, and return a lightweight project map as `additionalContext`. During prompt refinement, the local LLM uses this project map to select which deep-context docs to include in the composed output, replacing the current flat-concatenation rules approach with a retrieval-augmented one.

## Depends On

M1 (Hook Protocol), M2 (Core Pipeline), M5 (Rules Engine — extends the loader), M4 (Server — for SSE broadcasting of bootstrap events)

---

## Problem Statement

The current rules system has three scaling limitations:

1. **Flat concatenation.** `rules/loader.go` walks up to the git root, finds all configured rule files, and concatenates them. For projects with 5+ CLAUDE.md files across directories, this produces a large blob that the local LLM must process on every prompt. The LLM's context window fills with rules, leaving less room for the user's actual request.

2. **No awareness of rule scope.** All rules are treated equally. A CLAUDE.md in `web/` with TypeScript conventions is sent to the LLM even when the user's prompt is about Go code in `cli/`. The LLM's rule selection by index works but is limited by what it's shown.

3. **Cold start blindness.** Claude's first prompt in a session has no project awareness beyond what's in CLAUDE.md files Claude loads natively. The restruct plugin currently records the session and preloads the model but does not inject any project context until the first `UserPromptSubmit` fires.

## Solution

A two-phase approach:

**Phase 1 — SessionStart bootstrap:** Scan the project, classify rules into documents, build a project map (index + summaries), return it as `additionalContext` so Claude starts every session with awareness of available deep context. Return `watchPaths` for all source CLAUDE.md files so changes trigger re-processing.

**Phase 2 — Refinement-time retrieval:** During `UserPromptSubmit`, the local LLM sees the project map and selects which deep-context documents are relevant. The compose step loads those documents and includes their content in `additionalContext`, replacing the current flat rules blob with targeted, scoped rules.

---

## Architecture

```
SessionStart hook (sync, ~3s budget)
      │
      ▼
restruct bootstrap
      │
      ├── 1. Discover all CLAUDE.md files (git ls-files + walk)
      │
      ├── 2. Parse each file into sections (by ## headers)
      │
      ├── 3. Classify each section → category (context, constraints, workflow, anti-patterns)
      │       Uses the existing ParseRules() logic, extended to per-file granularity
      │
      ├── 4. Generate per-file document in .restruct/links/<hash>.md
      │       Each doc contains:
      │       - Source path
      │       - Category tags
      │       - Keyword index (extracted terms)
      │       - Section count and rule count
      │       - Full classified content
      │
      ├── 5. Build project map (JSON in SQLite + markdown summary)
      │       The map is a lightweight index:
      │       - File path → document hash
      │       - File path → keywords, categories, summary line
      │       - Total rule count across all files
      │
      ├── 6. Return to Claude Code:
      │       - additionalContext: project map as formatted markdown
      │       - watchPaths: absolute paths of all discovered CLAUDE.md files
      │
      └── 7. Broadcast bootstrap event via SSE for dashboard
```

```
UserPromptSubmit hook (existing refine flow, enhanced)
      │
      ▼
pipeline.Refine() — enhanced
      │
      ├── rules_load stage (CHANGED)
      │   Instead of flat-loading all rules:
      │   1. Load the project map from SQLite/file
      │   2. Pass the map to the LLM as part of the user message
      │   3. LLM's JSON output gains a new field: "relevant_docs": [0, 2]
      │      (indices into the project map)
      │
      ├── compose stage (CHANGED)
      │   1. Load selected deep-context documents from .restruct/links/
      │   2. Extract rules from those documents only
      │   3. Present numbered rules from the selected scope
      │   4. Compose XML context as before, but with scoped rules
      │
      └── (rest of pipeline unchanged)
```

### File Changed Hook (automatic re-bootstrap)

```
FileChanged hook (triggered by watchPaths)
      │
      ▼
restruct bootstrap --incremental
      │
      ├── Re-process only the changed file
      ├── Update its document in .restruct/links/
      ├── Update the project map
      └── (no additionalContext return — FileChanged doesn't support it)
```

---

## Storage Design

### Option A: File-based (.restruct/links/) — RECOMMENDED

```
.restruct/
  links/
    index.json              ← project map: [{path, hash, keywords, summary, categories}]
    a1b2c3d4.md             ← deep-context doc for CLAUDE.md (root)
    e5f6g7h8.md             ← deep-context doc for web/CLAUDE.md
    i9j0k1l2.md             ← deep-context doc for cli/CLAUDE.md
  sessions/
    ...existing...
  verify.yaml
```

**Why files over SQLite:**
- Team sharing: `.restruct/links/` can be checked into version control (generated but deterministic). Teams get consistent project maps without running bootstrap.
- Debuggability: developers can read the generated docs directly.
- No migration overhead: no new SQLite tables or schema changes.
- Performance: file reads are faster than SQLite queries for document-sized blobs.

**What goes in SQLite:**
- Bootstrap event records (session_id, timestamp, file_count, duration_us) — for the dashboard.
- Nothing else. The project map is the file-based `index.json`.

### Deep-Context Document Format

Each `.restruct/links/<hash>.md` file:

```markdown
---
source: CLAUDE.md
path: /Users/dev/project/CLAUDE.md
keywords: [typescript, shadcn, no-as-assertions, pnpm, xmake]
categories: [context, constraints, workflow, anti-patterns]
sections: 6
rules: 14
generated: 2026-03-31T12:00:00Z
---

## Context Rules
1. TypeScript: no `as` type assertions — they mask type errors. Fix the types instead
2. Web components: use shadcn (`@shadcn/ui`) before building custom components
3. Go: pure Go only, no CGO. SQLite via `modernc.org/sqlite` for cross-compilation
...

## Constraints
1. CLI writes to SQLite (sessions, refinements, pipeline events). Server only reads + writes ratings
2. All inference is local via Ollama — no data leaves the machine
...

## Workflow
1. Conventional commits
2. If you added new behavior, add or update tests to cover it
...

## Anti-Patterns
1. Do not use `as` in TypeScript to fix type errors
2. Do not use xmake `set_languages("go")` — use phony targets
...
```

### Project Map Format (index.json)

```json
{
  "version": 1,
  "generated": "2026-03-31T12:00:00Z",
  "files": [
    {
      "source": "CLAUDE.md",
      "path": "/Users/dev/project/CLAUDE.md",
      "hash": "a1b2c3d4",
      "keywords": ["typescript", "shadcn", "pnpm", "xmake", "go"],
      "categories": ["context", "constraints", "workflow", "anti-patterns"],
      "summary": "Root project rules: build system (xmake+pnpm), code style (TS/Go), architecture constraints, workflow conventions",
      "rule_count": 14
    },
    {
      "source": "web/CLAUDE.md",
      "path": "/Users/dev/project/web/CLAUDE.md",
      "hash": "e5f6g7h8",
      "keywords": ["react", "shadcn", "tailwind", "vite"],
      "categories": ["context", "constraints"],
      "summary": "Web frontend rules: React patterns, component library, styling conventions",
      "rule_count": 8
    }
  ],
  "total_rules": 22
}
```

### Project Map as additionalContext (SessionStart)

The formatted markdown returned to Claude:

```markdown
[Restruct project context index — 3 rule documents, 22 rules total]

Available project rule documents:
0. CLAUDE.md — Root project rules: build system (xmake+pnpm), code style (TS/Go), architecture constraints, workflow conventions [14 rules: context, constraints, workflow, anti-patterns]
1. web/CLAUDE.md — Web frontend rules: React patterns, component library, styling conventions [8 rules: context, constraints]
2. cli/CLAUDE.md — CLI rules: Go patterns, Cobra conventions, test organization [6 rules: context, anti-patterns]

Keywords across all documents: typescript, shadcn, pnpm, xmake, go, react, tailwind, vite, cobra, sqlite
```

---

## Interaction with Existing Systems

### Model Preload (async)

The current `plugin.json` has two SessionStart hooks:
1. `restruct session start` — sync, 5s timeout
2. `restruct model load` — async, 30s timeout

The bootstrap command replaces the `session start` command. Model preload continues unchanged as an async hook — both run in parallel (all SessionStart hooks execute simultaneously).

### Timeout Budget

The SessionStart hook timeout is currently 5s. Bootstrap needs to:
- Discover CLAUDE.md files (git ls-files: ~100ms)
- Read and parse each file (~10ms per file)
- Generate document hashes and write to .restruct/links/ (~50ms per file)
- Build project map (~10ms)
- Format additionalContext (~5ms)

For a project with 10 CLAUDE.md files, total: ~700ms. For 50 files: ~3s. The 5s timeout is sufficient for projects with up to ~70 rule files. For truly massive projects (monorepos with hundreds of CLAUDE.md files), we cap discovery at 50 files sorted by proximity to project root (closer = higher priority).

**No timeout increase needed.** If bootstrap exceeds 4s, it returns whatever it has processed so far (partial map is better than no map).

### Rules Loader Interaction

The current `rules/loader.go` is kept for backward compatibility. The bootstrap system wraps it:
- If `.restruct/links/index.json` exists and is fresh (mtime of all source files matches), use the pre-built index.
- If no index exists (first run, or plugin just installed), fall back to the current flat loader.
- During refinement, if the index is available, use retrieval-augmented rules instead of flat concatenation.

### LLM Classification Changes

The `LLMClassification` struct gains one field:

```go
type LLMClassification struct {
    // ...existing fields...
    RelevantDocs []int `json:"relevant_docs"` // indices into project map
}
```

The LLM's system prompt is updated to explain the project map and instruct it to select relevant documents by index. The compose step loads the selected documents and extracts their rules for numbered presentation.

### Large Projects

For monorepos with many CLAUDE.md files:
1. **Discovery cap:** Max 50 files, sorted by distance from project root (ascending).
2. **Summary-only mode:** If total rules exceed 500, the project map shows only summaries (no keyword lists). This keeps the SessionStart additionalContext under 2KB.
3. **LLM budget awareness:** The LLM prompt explains the document count and advises selecting at most 3 documents to keep context focused.

---

## Tasks

### Phase 1: Bootstrap at SessionStart

| Task | Estimate | Description |
|------|----------|-------------|
| 11.1 — CLAUDE.md discovery | 2h | New `internal/bootstrap/discover.go`: find all CLAUDE.md, AGENTS.md, .claude/rules.md files from project root. Use `git ls-files` with glob patterns, fall back to filepath.Walk. Cap at 50 files, sort by depth. |
| 11.2 — Document generator | 3h | New `internal/bootstrap/document.go`: parse a rules file into a deep-context document using extended `ParseRules()`. Extract keywords (split on spaces, filter stopwords, deduplicate). Generate summary line (first sentence or first header content). Write to `.restruct/links/<hash>.md` with YAML frontmatter. |
| 11.3 — Project map builder | 2h | New `internal/bootstrap/map.go`: aggregate all documents into `index.json`. Include version, generated timestamp, per-file metadata. Format as markdown for additionalContext. |
| 11.4 — Bootstrap CLI command | 2h | New `cmd/bootstrap.go`: hook handler for SessionStart. Reads hook input, runs discovery + generation + map building. Returns JSON with `additionalContext` (formatted project map) and `watchPaths` (absolute paths of all source files). Also records session (existing logic from `cmd/session.go`). |
| 11.5 — Hook output for SessionStart | 1h | Extend `hook/protocol.go` with `SessionStartOutput()` that produces `hookSpecificOutput` with `hookEventName: "SessionStart"`, `additionalContext`, and `watchPaths`. |
| 11.6 — Plugin.json wiring | 0.5h | Replace `restruct session start` with `restruct bootstrap` in the SessionStart hooks. Keep `restruct model load` as-is (async). Add `restruct bootstrap --incremental` as a FileChanged hook handler. |
| 11.7 — Incremental re-bootstrap | 2h | `cmd/bootstrap.go --incremental` flag: re-process only the changed file (received via FileChanged hook input's `file_path`), update its document, rebuild the project map. No additionalContext return (FileChanged doesn't support it). |
| 11.8 — SSE broadcasting | 1h | POST bootstrap events to the server for dashboard display. Event shape: `{session_id, event_type: "bootstrap", files_discovered, files_processed, duration_us}`. |
| 11.9 — LLM-assisted classification (async) | 4h | New `internal/bootstrap/classify.go`: after the fast structural bootstrap completes, spawn an async background task that sends each discovered file to the local LLM for classification. The LLM generates: a one-line summary, keyword list, category tags, and scope hints (global vs directory-specific, when to apply). Results update the document frontmatter and index.json in place. Uses the existing Ollama client. If Ollama is unavailable, static classification is retained (graceful degradation). |
| 11.10 — InstructionsLoaded hook handler | 2h | New hook handler: when Claude Code loads a CLAUDE.md file, record the event and trigger classification for that file if not already in the index. This provides a second discovery path — even files our git-based scan missed get classified when Claude actually loads them. Async, non-blocking. |
| 11.11 — Bootstrap tests | 3h | Unit tests for discover, document, map, classify. Integration test: create temp dir with multiple CLAUDE.md files at different depths, run full bootstrap, verify index.json and link files. Test cap at 50 files. Test incremental update. Test graceful timeout. Test LLM classification fallback when Ollama is down. |

### Phase 2: Retrieval-Augmented Refinement

| Task | Estimate | Description |
|------|----------|-------------|
| 11.12 — Project map loader | 1h | New `internal/bootstrap/loader.go`: load `index.json`, validate freshness by checking source file mtimes. Return structured map or nil (triggers fallback to flat loader). Prefer LLM-enriched metadata when available, fall back to structural metadata. |
| 11.13 — LLM prompt update for document selection | 2h | Update system prompt template (`versions/v5_*.tmpl`): explain the project map, instruct LLM to output `relevant_docs` array. Add project map as a new section in the user message built by `builder.go`. LLM-generated summaries make selection more accurate than keyword matching alone. |
| 11.14 — Pipeline integration | 3h | Modify `pipeline.go` Refine(): add a `map_load` stage before `rules_load`. If map exists, present it to LLM and parse `relevant_docs` from output. Load selected documents from `.restruct/links/`. Feed their parsed rules into compose. Fall back to flat loader if no map or no documents selected. |
| 11.15 — Compose with scoped rules | 2h | Update `compose.go`: accept rules from multiple documents. Present rules with source attribution: `[from CLAUDE.md] No as assertions`. Keep the numbered-index selection within each document. |
| 11.16 — Project map in additionalContext for UserPromptSubmit | 1h | When the project map is available, include a condensed version in the UserPromptSubmit additionalContext alongside the composed rules. This gives Claude ongoing awareness of the project structure. |
| 11.17 — End-to-end integration test | 3h | Test full flow: bootstrap → refinement with document selection → compose with scoped rules. Verify that a Go-related prompt only loads CLI rules, not web rules. Verify fallback when no map exists. |

### Phase 3: Polish & Dashboard

| Task | Estimate | Description |
|------|----------|-------------|
| 11.18 — Dashboard bootstrap panel | 3h | New section in SessionDetail page: show bootstrap status, discovered files, document summaries, LLM classification status (pending/complete). Link to individual documents. Show which documents were selected per refinement. |
| 11.19 — `restruct bootstrap` standalone command | 1h | Allow running `restruct bootstrap` manually (not just as a hook) for debugging. Print the project map to stdout. `--verbose` flag shows full document content. `--classify` forces synchronous LLM classification. |
| 11.20 — Staleness detection | 1h | On `UserPromptSubmit`, if any source file's mtime is newer than the index, log a warning. The watchPaths mechanism should handle this automatically, but staleness detection is a safety net. |
| 11.21 — .gitignore management | 0.5h | Ensure `.restruct/links/` is in `.gitignore` by default (existing install.go logic handles `.restruct/`). Add a config option `bootstrap.check_in_links: true` to exclude links/ from .gitignore for team sharing. |

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage location | `.restruct/links/` (files) | Debuggable, sharable, no schema migration. SQLite only for event telemetry |
| Document format | Markdown with YAML frontmatter | Human-readable, easy to parse in Go, familiar format |
| Keyword extraction | LLM-assisted (async sub-agent) | Local LLM classifies, summarizes, and extracts keywords per file. Runs async — does not block SessionStart. Static extraction as fast fallback until LLM processing completes |
| Summary generation | LLM-generated per-file summaries | Local LLM provides better classification than heuristics. Async processing means SessionStart returns immediately with a structural map; LLM-enriched metadata follows via background update |
| LLM classification trigger | SessionStart (async) + FileChanged + InstructionsLoaded | Initial classification runs as async background task after bootstrap. File changes re-classify the changed file only. InstructionsLoaded hook provides additional observability |
| Discovery cap | 50 files | Prevents timeout in monorepos. Sorted by depth ensures closest rules are always included |
| Project map format | JSON file + formatted markdown | JSON for programmatic access, markdown for additionalContext injection |
| Freshness check | Source file mtime vs index mtime | Simple, fast, no hashing needed for staleness detection |
| Fallback strategy | Flat loader when no index exists | Zero-cost migration: plugin works identically until first bootstrap runs |
| watchPaths | All discovered CLAUDE.md absolute paths | FileChanged hook re-processes only the changed file |
| LLM document selection | Index-based (same pattern as rule selection) | Consistent with existing M3/M5 approach. LLM selects documents by number |
| Incremental update | FileChanged hook, single-file re-process | Avoids full re-scan on every edit. Map rebuild is O(n) but n is small (cap 50) |

---

## Acceptance Criteria

- [ ] `restruct bootstrap` discovers all CLAUDE.md files up to the project root
- [ ] Deep-context documents are generated in `.restruct/links/` with correct content and frontmatter
- [ ] `index.json` project map is built with keywords, summaries, and categories
- [ ] SessionStart hook returns `additionalContext` with formatted project map
- [ ] SessionStart hook returns `watchPaths` for all discovered source files
- [ ] FileChanged triggers incremental re-bootstrap of the changed file
- [ ] Refinement pipeline loads project map and presents it to the LLM
- [ ] LLM selects relevant documents; compose uses scoped rules
- [ ] Fallback to flat rules loading when no index exists
- [ ] Bootstrap completes within 5s for projects with up to 50 rule files
- [ ] Existing tests continue to pass (backward compatibility)
- [ ] New tests cover discovery, document generation, map building, and retrieval

## Files Modified

- `cli/cmd/session.go` — session start logic absorbed into bootstrap
- `cli/cmd/bootstrap.go` — NEW: bootstrap CLI command (SessionStart + FileChanged handler)
- `cli/internal/bootstrap/discover.go` — NEW: CLAUDE.md file discovery
- `cli/internal/bootstrap/document.go` — NEW: deep-context document generation
- `cli/internal/bootstrap/map.go` — NEW: project map builder
- `cli/internal/bootstrap/loader.go` — NEW: project map loader for refinement time
- `cli/internal/hook/protocol.go` — extend with SessionStart output helpers
- `cli/internal/pipeline/pipeline.go` — add map_load stage, document selection
- `cli/internal/pipeline/compose.go` — scoped rules with source attribution
- `cli/internal/prompt/builder.go` — project map section in user message
- `cli/internal/prompt/versions/v5_*.tmpl` — NEW: system prompt with document selection
- `plugin/.claude-plugin/plugin.json` — replace session start with bootstrap, add FileChanged hook

## Risk

**Low-Medium.** The bootstrap phase is straightforward file processing with no LLM dependency (keeping it fast). The riskier part is Phase 2 (retrieval-augmented refinement), which changes the LLM's output schema and the compose pipeline. Mitigation: the fallback to flat loading means any regression in document selection degrades gracefully to current behavior, not to broken behavior. The LLM prompt change (v4 to v5) should be tested with the evaluation corpus from M9 before merging.
