# M6: Caching & Performance

## Goal

Ensure the refinement pipeline adds minimal latency (<2s target) through smart caching, model preloading, and pipeline optimization. Cache is two-tier: fast local files in `.restruct/cache/` with write-through to global SQLite (M4).

## Depends On

- M2 (Core Pipeline) — needs stable pipeline to optimize.
- M4 (Server & Dashboard) — SQLite schema includes `cache` table; this milestone implements the cache logic.

---

## Tasks

### 5.1 — Cache Key Improvements

**What:** Make cache keys more intelligent so cache hits are useful.

**Current behavior:** `SHA256(rawPrompt + rulesHash)` — exact match only.

**Improvements:**
- **Normalize before hashing:** Lowercase, strip extra whitespace, remove trailing punctuation. "Fix the auth bug" and "fix the auth bug." should hit the same cache entry.
- **Include model in key:** Different models produce different outputs. Cache key: `SHA256(normalize(prompt) + rulesHash + model)`.
- **Project-scoped:** Cache key includes `project_path`. Same prompt in different projects (different rules) = different cache entries.
- **Git-context exclusion:** Don't include git context in the cache key. Git context changes constantly; a cached refinement for the same prompt+rules is still valid even if the branch changed.
- **Consider fuzzy matching (stretch):** For near-identical prompts (edit distance < 3 words), return cached result.

### Two-Tier Cache Architecture

```
Lookup path:
1. Check .restruct/cache/<hash>.json  (local file, <1ms)
2. If miss → check SQLite cache table   (global DB, <5ms)
3. If miss → run Ollama refinement       (500ms-2s)
4. Write result → .restruct/cache/ AND SQLite cache table

Why two tiers:
- Local files: zero-latency for hot-path; work even if server/DB is unavailable
- SQLite: survives .restruct/ cleanup; enables dashboard cache analytics; shared across sessions
```

### 5.2 — Cache Expiration & Eviction

**What:** Prevent unbounded cache growth.

**Requirements:**
- **TTL:** Entries expire after configurable duration (default: 7 days)
- **Max entries:** Configurable cap (default: 1000 entries)
- **Eviction policy:** LRU — when at capacity, remove least recently accessed entries
- **Rules invalidation:** When `rulesHash` changes (user edited agents.md), all entries with the old hash are stale. Don't actively purge (expensive), but skip stale entries on lookup.
- **Cache warming:** No precomputation. Caching is purely reactive.

### 5.3 — Cache Storage Optimization

**What:** Improve the file-based cache for faster access.

**Current behavior:** Each entry is a separate JSON file. Fine for small caches, slow at scale.

**Options:**
1. **Keep file-per-entry** but add an in-memory index (loaded at startup from a manifest file). Lookup = read index → read single file.
2. **Switch to SQLite** (via `modernc.org/sqlite`, pure Go). Single file, indexed lookups, built-in TTL via timestamp column.
3. **Keep simple.** 1000 JSON files with SHA256 names is fast enough on any modern filesystem. Just add the manifest for stats.

**Recommendation:** Option 2 (SQLite). M4 introduces SQLite as a dependency for the server. Consolidate the cache into the same SQLite database — add a `cache` table alongside the server's tables. This simplifies the codebase (one storage backend), enables richer cache queries (TTL, LRU via timestamp columns), and the dependency is already paid for.

### 5.4 — Model Preloading

**What:** Reduce first-prompt latency by preloading the model into GPU/RAM.

**Current behavior:** `model load` command exists but must be run manually.

**Improvements:**
- **Plugin SessionStart hook:** Already configured in `plugin/hooks/hooks.json`. Verify it fires and actually preloads the model.
- **Keep-alive management:** Default keep_alive is 60m. This means if the user is idle for >60m, the next prompt pays cold-load latency. Consider:
  - Setting keep_alive to match typical work sessions (4h)
  - Or implementing a heartbeat that refreshes keep_alive periodically while Claude Code is active
- **Background loading:** The `model load` call must be non-blocking (fire-and-forget). If the model isn't loaded when the first prompt arrives, the pipeline should wait (up to timeout) rather than failing.

### 5.5 — Pipeline Stage Timing

**What:** Measure and report timing for each pipeline stage to identify bottlenecks.

**Stages to time:**
1. Rules loading (expect: <10ms)
2. Cache lookup (expect: <5ms)
3. Git context gathering (expect: <100ms)
4. Prompt building (expect: <1ms)
5. Ollama inference (expect: 500ms-2s)
6. Output validation (expect: <1ms)
7. Cache write (expect: <10ms)

**Implementation:**
- Each stage records start/end time
- Total pipeline time logged at INFO level
- Individual stage times logged at DEBUG level
- `restruct refine --timing` flag prints timing breakdown to stderr

### 5.6 — Latency Budget Enforcement

**What:** If total pipeline time exceeds the latency budget, abort and return raw prompt.

**Requirements:**
- Default budget: 5s (configurable)
- Ollama inference gets its own sub-budget (budget - overhead, typically budget - 500ms)
- If Ollama inference exceeds sub-budget, cancel the request and return raw prompt
- Log a warning with timing breakdown when budget is exceeded
- Users can increase budget via config for slower hardware

---

## Acceptance Criteria

- [ ] Cache keys normalized and model-aware
- [ ] TTL-based expiration with LRU eviction
- [ ] Model preloading works via SessionStart hook
- [ ] Every pipeline stage is timed and logged
- [ ] Latency budget enforced; over-budget returns raw prompt
- [ ] Median refinement latency <2s on 16GB M-series Mac

## Files Modified

- `cli/internal/cache/store.go` — normalization, TTL, eviction, manifest
- `cli/internal/cache/store_test.go` — new test cases
- `cli/internal/pipeline/pipeline.go` — timing, budget enforcement
- `cli/internal/config/config.go` — cache TTL, max entries, latency budget config
- `cli/cmd/refine.go` — `--timing` flag

## Risk

**Low.** Caching and performance work is well-understood. The main risk is premature optimization — measure first (5.5), then optimize what's actually slow.
