---
label: Milestones
icon: tasklist
order: 800
---

# Restruct: Implementation Milestones

## Overview

10 milestones taking the scaffolded Go codebase to a production-ready Claude Code plugin with a monitoring dashboard and self-improvement capabilities. Each milestone has a dedicated spec file with detailed requirements.

---

## Milestone Map

| # | Milestone | Status | Depends On | Summary |
|---|-----------|--------|------------|---------|
| M1 | [Hook Protocol & Claude Code Integration](M1-HOOK-PROTOCOL.md) | **Done** | — | Verify and fix the hook contract; document session_id, transcript_path, and hook I/O schema |
| M2 | [Core Pipeline Hardening](M2-CORE-PIPELINE.md) | **Done** | M1 | Production-ready pipeline: error handling, timeouts, streaming Ollama, graceful degradation |
| M3 | [Prompt Engineering & Output Format](M3-PROMPT-ENGINE.md) | **~40% Done** | M2 | Prompt builder + system template implemented. Needs injection framing, passthrough detection, optimization |
| M4 | [Server & Dashboard](M4-SERVER-DASHBOARD.md) | **~90% Done** | M1, M2 | Chi server, React SPA, SQLite data layer, SSE hub, daemon management all implemented |
| M5 | [Rules Engine & Context Gathering](M5-RULES-ENGINE.md) | **~30% Done** | M2 | Rules loader with hierarchical search implemented. Needs LLM filtering, git context, conversation-aware |
| M6 | [Caching & Performance](M6-CACHE-PERF.md) | **~30% Done** | M2, M4 | File-based cache with SHA256 keys implemented. Needs SQLite migration, TTL, LRU eviction |
| M7 | [CLI UX & Configuration](M7-CLI-UX.md) | **~70% Done** | M2 | Doctor, model, config commands implemented. Needs auto-fix, error polish, completions, version check |
| M8 | [Plugin Distribution & Installation](M8-PLUGIN-DIST.md) | **~60% Done** | M1, M7 | Plugin manifest, skills, cross-platform builds done. Needs install flow, uninstall, release automation |
| M8.1 | [Build System (Make → xmake)](M8.1-BUILD-SYSTEM.md) | **Done** | — | xmake + pnpm workspaces, incremental builds, cross-compilation, debug/release modes |
| M9 | [Testing & Calibration](M9-TESTING.md) | **~10% Done** | M3, M4 | 26 tests across 5 files (M1, M2). Needs coverage expansion, integration tests, eval framework |
| M10 | [Self-Improvement Loop](M10-SELF-IMPROVE.md) | Not Started | M4, M9 | Dashboard-driven feedback loop: rating analysis, system prompt refinement, rules suggestions |

---

## Task Tracking

### M1: Hook Protocol & Claude Code Integration (Done)

| Task | Status | Notes |
|------|--------|-------|
| 1.1 — Verify hook contract via live testing | ✅ Done | Researched + verified live. `docs/HOOK-CONTRACT.md` written |
| 1.2 — Fix `hook/protocol.go` | ✅ Done | Rewritten: `HookInput` with all fields, `HookOutput` with `hookSpecificOutput.additionalContext`, `suppressOutput: true` |
| 1.3 — Fix `hook/install.go` | ✅ Done | `UserPromptSubmit` (was `UserPrompt`), empty matcher, `SessionStart`/`SessionEnd` hooks, `.gitignore` auto-update |
| 1.4 — End-to-end smoke test | ✅ Done | Hook fires in live Claude Code session. additionalContext injection confirmed visible in system-reminder |
| 1.5 — Per-project `.restruct/` directory | ✅ Done | `internal/session/` package: session files at `.restruct/sessions/<id>.json`, stale cleanup, full test coverage |
| 1.6 — Session tracking foundation | ✅ Done | `cmd/session.go` (start/end), `cmd/refine.go` tracks sessions, session_id + transcript_path extracted |
| 1.7 — Graceful degradation | ✅ Done | Ollama down = exit 0, empty stdout (passthrough). Malformed input, empty stdin all handled. Tested |
| 1.8 — Transcript parsing | ↗️ Deferred | Moved to M5 (conversation-aware refinement). Not needed for core hook functionality |

**Tests:** 14 passing (protocol: 7, install: 3, session: 4)

### M2: Core Pipeline Hardening (Done)

| Task | Status | Notes |
|------|--------|-------|
| 2.1 — Ollama client robustness | ✅ Done | Separate connect (5s) / request (120s) / stall (30s) timeouts. `EnsureModel()` pre-check. Retry on transient errors |
| 2.2 — Pipeline error handling | ✅ Done | Interface-based deps (`LLMClient`, `RulesLoader`, `GitProvider`, `CacheStore`). Every failure returns error → caller passthroughs |
| 2.3 — Output validation | ✅ Done | `validateOutput()`: empty check, length check, system prompt leak detection |
| 2.4 — Structured logging | ✅ Done | `log/slog` throughout, all to stderr. DEBUG stage timings, INFO refinement start/complete, WARN degradation |
| 2.5 — Context timeout (safety net) | ✅ Done | 120s via `cfg.Ollama.RequestTimeout`. Not a performance target |
| 2.6 — Streaming Ollama client | ✅ Done | `ChatStream()` with raw HTTP NDJSON parsing, `TokenSink` interface, 30s stall detection. `ChatWithRetry()` wraps with single retry |
| 2.7 — Pipeline tests | ✅ Done | 12 test cases: happy path, cache hit, unavailable, error, empty output, short output, leak, no rules, no git, context cancel, long-running success, ensure failure |

**Tests:** 12 pipeline + 5 validation = 17 new tests (26 total across all packages)

**Verified end-to-end:** "fix the auth bug where tokens expire too fast" → 22.8s inference → 210-word structured output with objective, constraints, workflow, uncertainty protocol, anti-patterns. Injected as hidden additionalContext.

### M3: Prompt Engineering & Output Format (~40% Done)

| Task | Status | Notes |
|------|--------|-------|
| 3.1 — Injection framing | ❌ | Design the bridge text between user prompt and structured additionalContext |
| 3.2 — System prompt optimization | ✅ Done | `system_prompt.tmpl` embedded, instructs model to output `<structured_prompt>` block |
| 3.3 — Context builder optimization | ✅ Done | `prompt/builder.go` — assembles system + user message with Developer's Request, Project Rules, Repository State |
| 3.4 — Prompt template as configurable asset | ❌ | User-overridable `system_prompt.tmpl` |
| 3.5 — Passthrough detection | ❌ | Skip injection on follow-ups ("y", "try option 2"), commands |
| 3.6 — Refinement quality smoke tests | ❌ | 10+ test cases verifying injection format |

### M4: Server & Dashboard (~90% Done)

| Task | Status | Notes |
|------|--------|-------|
| 4.1 — SQLite data layer | ✅ Done | `internal/db` package: WAL mode, embedded migrations, Session/Refinement/PipelineEvent models, full CRUD, metrics aggregation |
| 4.2 — Go server (Chi) | ✅ Done | `internal/server`: Chi router, `/api/events` (SSE), `/api/health`, `/api/metrics`, `/api/sessions/*`, `/api/refinements/*`. Middleware: logging, recovery, compression |
| 4.3 — Pipeline recording (direct to SQLite) | ✅ Done | `db/recorder.go`: `RecordSession()`, `RecordRefinement()`, `RecordPipelineEvent()`. CLI writes on every hook invocation |
| 4.4 — React SPA (Vite) | ✅ Done | Dashboard, Sessions, SessionDetail, RefinementDetail pages. API client, SSE hook, ShadcN components, Tailwind |
| 4.5 — Dev server flow | ✅ Done | Build tags: debug → CORS + nil FS (Vite proxy), release → embedded SPA. `pnpm dev` runs both |
| 4.6 — Daemon management | ✅ Done | `restruct serve --daemon`, PID file, `serve stop`, `serve status`. Setsid for background process |
| 4.7 — Streaming Ollama integration | ⚠️ Partial | `ChatStream()` available in Ollama client. Not yet wired to SSE hub for live dashboard view |
| 4.8 — SSE live updates (DB polling) | ✅ Done | `sse/hub.go`: pub-sub pattern, 1s DB poll, `refinement:new` broadcasts to all connected clients |

### M5: Rules Engine & Context Gathering (~30% Done)

| Task | Status | Notes |
|------|--------|-------|
| 5.1 — Hierarchical rules loading | ✅ Done | `internal/rules/loader.go`: configurable search paths, file concatenation, SHA256 hash for cache keying |
| 5.2 — Rules relevance filtering (LLM-assisted) | ❌ | Local LLM selects relevant sections, not keyword matching |
| 5.3 — Smart git context | ⚠️ Partial | `internal/git/context.go` exists. Needs changed files, file-prompt correlation, branch parsing |
| 5.4 — Conversation-aware refinement | ❌ | Transcript parsing, session memory, follow-up detection |
| 5.5 — File mention detection | ❌ | Extract context for mentioned files |
| 5.6 — Rules loader tests | ❌ | >80% coverage |

### M6: Caching & Performance (~30% Done)

| Task | Status | Notes |
|------|--------|-------|
| 6.1 — Cache key improvements | ⚠️ Partial | SHA256(prompt + rules_hash) implemented in `internal/cache/store.go`. Needs normalization, model-scoping |
| 6.2 — Cache expiration & eviction | ❌ | TTL, LRU, rules invalidation |
| 6.3 — Cache storage (SQLite) | ❌ | Still file-based JSON. Needs migration to shared `internal/db` cache table |
| 6.4 — Model preloading | ✅ Done | SessionStart hook calls `restruct model load` with keep_alive. Wired in `plugin.json` |
| 6.5 — Pipeline stage timing (observability) | ✅ Done | Pipeline events recorded to SQLite with timing data. Visible via `/api/refinements/{id}/events` |

### M7: CLI UX & Configuration (~70% Done)

| Task | Status | Notes |
|------|--------|-------|
| 7.1 — Doctor command enhancement | ✅ Done | `cmd/doctor.go`: JSON status report, Ollama install/running/version checks, model availability, config path, `AllGood` flag. Needs human-readable output + auto-fix |
| 7.2 — Error message quality | ❌ | Every error includes remediation steps |
| 7.3 — Config command polish | ✅ Done | `cmd/config_cmd.go`: Viper-based `set`/`get`/`list`. Needs validation, reset, edit, source annotation |
| 7.4 — Model command polish | ✅ Done | `cmd/model.go`: `pull` (with progress), `load` (preload with keep_alive), `status` (list available). Needs hardware-based recommendation |
| 7.5 — Shell completions | ❌ | bash/zsh/fish/powershell |
| 7.6 — Version & update check | ❌ | `restruct version --check` |

### M8: Plugin Distribution & Installation (~60% Done)

| Task | Status | Notes |
|------|--------|-------|
| 8.1 — Plugin manifest finalization | ✅ Done | `plugin.json` with inline hooks (UserPromptSubmit, SessionStart, SessionEnd), `marketplace.json` with owner field |
| 8.1.1 — Build system migration | ✅ Done | Make → xmake + pnpm workspaces. Incremental builds, cross-compile, debug/release modes. See [M8.1-BUILD-SYSTEM.md](M8.1-BUILD-SYSTEM.md) |
| 8.2 — Cross-platform binary strategy | ✅ Done | darwin-arm64, darwin-x86_64, linux-x86_64 via `pnpm build`. Binaries committed to repo (Claude Code installs from GitHub) |
| 8.3 — Installation flow | ❌ | `/plugin marketplace add`, guided setup |
| 8.4 — Setup skill | ✅ Done | `skills/setup/SKILL.md` — auto-executes: diagnostics, install Ollama, detect RAM, select model, pull, warm |
| 8.5 — Uninstall & cleanup | ❌ | `restruct uninstall` |
| 8.6 — Plugin release automation | ❌ | Changelog, version bumping, CI commits binaries |

### M9: Testing & Calibration (~10% Done)

| Task | Status | Notes |
|------|--------|-------|
| 9.1 — Unit test coverage | ⚠️ Partial | 26 tests across 5 files: protocol (7), install (3), session (4), pipeline (12), cache. Needs >80% across all packages |
| 9.2 — Integration test suite | ❌ | Mock Ollama server, end-to-end binary tests |
| 9.3 — Prompt quality evaluation framework | ❌ | `restruct eval` command, structural + LLM-as-judge |
| 9.4 — Evaluation corpus creation | ❌ | 30+ test prompts with quality criteria |
| 9.5 — Baseline measurement | ❌ | Scores documented in `docs/BASELINE.md` |
| 9.6 — Regression test gate | ❌ | CI gate on structural compliance |

### M10: Self-Improvement Loop (Not Started)

| Task | Status | Notes |
|------|--------|-------|
| 10.1 — Outcome tracking | ❌ | Enrich refinement records with outcome signals |
| 10.2 — Refinement outcome hooks | ❌ | Detect correction follow-ups |
| 10.3 — LLM-as-evaluator pipeline | ❌ | `restruct improve analyze` |
| 10.4 — System prompt suggestions | ❌ | `restruct improve suggest` with reasoning |
| 10.5 — Rules file suggestions | ❌ | Suggest additions to agents.md |
| 10.6 — Improvement metrics dashboard | ❌ | `restruct improve stats`, trend tracking |
| 10.7 — Self-refining system prompt | ❌ | LLM generates revised prompt from rating data |
| 10.8 — Automated evaluation runs | ❌ | Manual + nudge after N refinements |

---

## Resolved Decisions

These were previously open and have been resolved during implementation:

| Decision | Resolution | Resolved In |
|----------|-----------|-------------|
| additionalContext vs stdout | **additionalContext** — appends hidden context, doesn't replace prompt. `suppressOutput: true` keeps UX clean | M1 implementation |
| Hook event name | **UserPromptSubmit** (not UserPrompt). No matcher support — always fires | M1 implementation |
| Prompt replacement model | **Injection, not replacement.** Recency bias steers Claude toward our structured context | M1 + M3 rewrite |
| DB ownership model | **SQLite is a shared data store.** CLI writes directly, server reads + writes ratings. No server needed for data collection | M4 rewrite |
| Latency philosophy | **Thoroughness over speed.** 120s safety net, stall detection at 30s. No latency budgets | M2 rewrite |
| Rules filtering | **LLM-assisted**, not keyword matching. Local LLM is free, quality > speed | M5 rewrite |
| Timeout architecture | Three-tier: connect (5s fast-fail), request (120s safety net), stall (30s no-token detection). No per-stage budgets | M2 implementation |
| Pipeline dependencies | Interface-based (`LLMClient`, `RulesLoader`, `GitProvider`, `CacheStore`). Enables mocking and future swapping | M2 implementation |
| Plugin structure | Single-repo marketplace pattern matching ghostty-notify. `plugin.json` + `marketplace.json` in `.claude-plugin/`, hooks inline, everything else at root | M8 implementation |
| Setup skill | Auto-executes: checks RAM → selects model → installs Ollama → pulls model → warms. Uses `ollama` directly, not CLI wrappers | M8 implementation |
| Build system | xmake (via npm `xmake-build-system`) + pnpm workspaces. Custom DSL modules (`xmake/phony.lua`, `xmake/go_build.lua`) for phony Go targets | M8.1 implementation |
| Go build in xmake | Phony targets with direct `go build`, NOT `set_languages("go")`. xmake's native Go support is broken for Go 1.20+ ([#3586](https://github.com/xmake-io/xmake/issues/3586)) | M8.1 implementation |
| Debug/release | Go build tags (`//go:build debug`). `embed_debug.go` returns nil FS + dev mode, `embed_release.go` embeds web dist | M8.1 implementation |
| Dev workflow | `pnpm dev` → `xmake watch -d cli -r` (auto-rebuild + restart Go server) + Vite HMR, in parallel via pnpm workspaces | M8.1 implementation |

## Open Decisions (Remaining)

- **M3:** Exact injection framing text — how to bridge user prompt → structured context. Current system prompt produces XML (`<structured_prompt>`) which tested well end-to-end but hasn't been compared against markdown
- **M3:** Whether system prompt needs full rewrite for injection model or just framing adjustments. Current prompt says "output goes directly to the agent" which is technically wrong (it goes as additionalContext)
- **M5:** Transcript parsing depth for conversation-aware refinement
- **M10:** How to safely auto-suggest system prompt changes

---

## Dependency Graph

```
M1  (Hook Protocol)        ██████████  Done
 │
 ├── M2  (Core Pipeline)   ██████████  Done
 │    ├── M3  (Prompt Engine)    ████░░░░░░  ~40%
 │    ├── M4  (Server)           █████████░  ~90%
 │    ├── M5  (Rules Engine)     ███░░░░░░░  ~30%
 │    ├── M6  (Cache & Perf)     ███░░░░░░░  ~30%
 │    └── M7  (CLI UX)           ███████░░░  ~70%
 │                                    │
 │                                    └──► M8  (Plugin Dist)  ██████░░░░  ~60%
 │
 └── M8.1 (Build System)   ██████████  Done

 M3 + M4 ──► M9  (Testing)      █░░░░░░░░░  ~10%
              └──► M10 (Self-Improve)  ░░░░░░░░░░
```

**M1, M2, and M8.1 are complete.** M4 is nearly done (only streaming Ollama → SSE remaining). M3, M5, M6 need their advanced features (LLM-assisted filtering, passthrough detection, cache migration). M7 needs polish (auto-fix, completions). M9 needs major expansion from 26 tests to full coverage.

---

## Key Architecture Decisions (Locked)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Already scaffolded, single binary distribution |
| LLM Runtime | Ollama (local, streaming) | Cost, latency, privacy; streaming enables live dashboard |
| Default Model | Qwen 2.5 Coder 14B (RAM-dependent) | Best code-aware local model; Apache 2.0. Setup skill auto-selects based on RAM |
| Distribution | Claude Code plugin | Seamless hook integration |
| Integration | Claude Code hooks (UserPromptSubmit) | session_id, transcript_path available on every hook |
| Injection model | `additionalContext` (hidden, appended) | Recency bias steers Claude; `suppressOutput` keeps UX clean |
| CLI Framework | Cobra + Viper | Already scaffolded |
| Web Framework | Chi (go-chi/chi) | stdlib-compatible, native SSE/Flusher, minimal deps |
| Frontend | React + Vite + Tailwind | Fast dev iteration, go:embed for production |
| Build System | xmake + pnpm workspaces | Lua-scriptable Make replacement. Custom DSL in `xmake/` for phony targets. `pnpm dev` / `pnpm build` |
| Package Manager | pnpm | Workspaces for web/ + cli/, unified scripts |
| Database | SQLite (modernc.org/sqlite), shared | Pure Go, no CGO. CLI writes directly, server reads + rates. WAL mode |
| Per-project state | `.restruct/` directory (gitignored) | Session mapping for fast local lookup |
| Multi-instance | session_id + project_path + WAL mode | Partitioned writes avoid contention |

## Core Philosophy

**Injection, not replacement.** Claude always sees the user's original prompt. We append `additionalContext` (hidden from the user's UI) that steers Claude toward structured, rules-aware execution. Recency bias means our injected context — placed after the user's prompt — carries heavy weight.

**Thoroughness over speed.** The local LLM is free and has no rate limits. A 10-15s refinement that prevents 3 rounds of clarification is a massive net win. The pipeline should never sacrifice quality for speed.

**Rule saturation is the core value.** The user knows their project's rules but doesn't type them every time. We inject relevant constraints into Claude's active context window at the point of highest attention, ensuring Claude follows project conventions without being told.

**Use the LLM generously.** For rules filtering, context selection, and multi-pass refinement — keyword matching and heuristics are anti-patterns when a free local LLM can do better.

## Hook Data Available (from M1 research)

Every hook receives:
```json
{
  "session_id": "unique-session-id",
  "transcript_path": "/path/to/transcript.jsonl",
  "cwd": "/working/directory",
  "hook_event_name": "UserPromptSubmit",
  "prompt": "user's raw input"
}
```

Environment variables: `CLAUDE_PLUGIN_DATA`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PROJECT_DIR`.

This gives us session tracking, conversation history access, and a persistent data directory for the server.
