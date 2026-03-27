# M6: Caching & Performance

## Goal

Avoid redundant LLM calls through smart caching, and ensure the model is warm when needed. The goal is NOT to minimize latency at all costs — a thorough 10s refinement that prevents rework is better than a fast 1s refinement that misses context. Caching targets the case where the exact same prompt+rules combination has already been refined — no point re-running it.

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
3. If miss → run Ollama refinement       (5-30s depending on prompt complexity)
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

### 5.5 — Pipeline Stage Timing (Observability)

**What:** Measure and report timing for each pipeline stage for dashboard visibility and debugging — NOT for enforcement.

**Stages to time:**
1. Rules loading
2. Cache lookup
3. Git context gathering
4. Prompt building
5. Ollama inference
6. Output validation
7. Cache write

**Implementation:**
- Each stage records start/end time
- Total pipeline time logged at INFO level
- Individual stage times logged at DEBUG level
- `restruct refine --timing` flag prints timing breakdown to stderr
- All timing data recorded to SQLite for dashboard analytics (M4)

**No latency budget enforcement.** The safety-net timeout from M2 (120s default, stall detection at 30s no-tokens) handles genuinely stuck requests. A healthy refinement that takes 20s because the prompt is complex should complete successfully.

---

## Acceptance Criteria

- [ ] Cache keys normalized and model-aware
- [ ] TTL-based expiration with LRU eviction
- [ ] Model preloading works via SessionStart hook
- [ ] Every pipeline stage is timed and logged (observability, not enforcement)

## Files Modified

- `cli/internal/cache/store.go` — normalization, TTL, eviction, manifest
- `cli/internal/cache/store_test.go` — new test cases
- `cli/internal/pipeline/pipeline.go` — timing (observability)
- `cli/internal/config/config.go` — cache TTL, max entries config
- `cli/cmd/refine.go` — `--timing` flag

## Risk

**Low.** Caching is well-understood. The main risk is premature optimization — measure first (5.5), and remember that LLM inference time is not wasted time. A cache hit is great, but a cache miss that produces a thorough refinement is still a win.
