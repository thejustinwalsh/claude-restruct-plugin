# Restruct: Implementation Milestones

## Overview

10 milestones taking the scaffolded Go codebase to a production-ready Claude Code plugin with a monitoring dashboard and self-improvement capabilities. Each milestone has a dedicated spec file with detailed requirements.

---

## Milestone Map

| # | Milestone | Status | Depends On | Summary |
|---|-----------|--------|------------|---------|
| M1 | [Hook Protocol & Claude Code Integration](M1-HOOK-PROTOCOL.md) | Not Started | — | Verify and fix the hook contract; document session_id, transcript_path, and hook I/O schema |
| M2 | [Core Pipeline Hardening](M2-CORE-PIPELINE.md) | Not Started | M1 | Production-ready pipeline: error handling, timeouts, streaming Ollama, graceful degradation |
| M3 | [Prompt Engineering & Output Format](M3-PROMPT-ENGINE.md) | Not Started | M2 | Optimize system prompt, determine best output format, maximize consistency |
| M4 | [Server & Dashboard](M4-SERVER-DASHBOARD.md) | Not Started | M1, M2 | Chi server + React/Vite SPA for monitoring, rating, and feedback; SQLite persistence; daemon management |
| M5 | [Rules Engine & Context Gathering](M5-RULES-ENGINE.md) | Not Started | M2 | Robust rules loading, smart git context, conversation-aware refinement |
| M6 | [Caching & Performance](M6-CACHE-PERF.md) | Not Started | M2, M4 | Two-tier cache (.restruct/ + SQLite), latency optimization, model preloading |
| M7 | [CLI UX & Configuration](M7-CLI-UX.md) | Not Started | M2 | Polish doctor, config, model commands; improve error messages and onboarding |
| M8 | [Plugin Distribution & Installation](M8-PLUGIN-DIST.md) | Not Started | M1, M7 | Plugin packaging, cross-platform builds, setup wizard, first-run experience |
| M9 | [Testing & Calibration](M9-TESTING.md) | Not Started | M3, M4 | Comprehensive test suite, prompt-pair evaluation, quality scoring framework |
| M10 | [Self-Improvement Loop](M10-SELF-IMPROVE.md) | Not Started | M4, M9 | Dashboard-driven feedback loop: rating analysis, system prompt refinement, rules suggestions |

---

## Dependency Graph

```
M1 (Hook Protocol)
 ├── M2 (Core Pipeline)
 │    ├── M3 (Prompt Engine) ────────────────┐
 │    ├── M4 (Server & Dashboard) ───────────┤
 │    │    └── SSE streaming ◄── M2 Ollama   │
 │    ├── M5 (Rules Engine)                  ├── M9 (Testing) ──► M10 (Self-Improve)
 │    ├── M6 (Cache & Perf)                  │         ▲
 │    └── M7 (CLI UX) ──► M8 (Plugin Dist)  │         │
 │                                            │         │
 └── M4 (Server — needs session_id from M1) ─┘    M4 ──┘
```

**Parallelizable after M2:** M3, M4, M5, M6, M7 can all be worked concurrently.
**M4 is the new critical path** — M9 and M10 both depend on the server/dashboard for rating data and the feedback loop.

---

## Key Architecture Decisions (Locked)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Already scaffolded, single binary distribution |
| LLM Runtime | Ollama (local, streaming) | Cost, latency, privacy; streaming enables live dashboard |
| Default Model | Qwen 2.5 Coder 14B | Best code-aware local model; Apache 2.0 |
| Distribution | Claude Code plugin | Seamless hook integration |
| Integration | Claude Code hooks (UserPromptSubmit) | session_id, transcript_path available on every hook |
| CLI Framework | Cobra + Viper | Already scaffolded |
| Web Framework | Chi (go-chi/chi) | stdlib-compatible, native SSE/Flusher, minimal deps |
| Frontend | React + Vite + Tailwind | Fast dev iteration, go:embed for production |
| Database | SQLite (modernc.org/sqlite) | Pure Go, no CGO, single file, cross-platform |
| Frontend bundling | Vite → go:embed | Dev: Vite proxy to Go; Prod: embedded in binary |
| Server management | Daemon with PID file | CLI manages lifecycle; CLAUDE_PLUGIN_DATA for PID storage |
| Per-project state | `.restruct/` directory (gitignored) | Session mapping, local cache, debug logs; enables multi-instance tracking |
| Data strategy | Write-through: `.restruct/` → global SQLite | Fast local reads, global analytics; works with or without server running |
| Multi-instance | session_id + project_path partitioning | Multiple Claude instances write concurrently via SQLite WAL mode |

## Open Decisions (To Be Resolved Per-Milestone)

- **M1:** Verify additionalContext vs stdout for prompt replacement
- **M3:** XML vs markdown vs hybrid output format
- **M5:** Scope of conversation-aware refinement (transcript parsing depth)
- **M6:** File-based cache vs SQLite-backed cache (M4 introduces SQLite; may consolidate)
- **M10:** How to safely auto-suggest system prompt changes

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
