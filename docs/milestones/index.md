---
label: Milestones
icon: tasklist
order: 800
---

# Restruct: Implementation Milestones

## Overview

19 milestones taking the scaffolded Go codebase to a production-ready Claude Code plugin with a monitoring dashboard and self-improvement capabilities. Each milestone has a dedicated spec file with detailed requirements.

---

## Milestone Map

| # | Milestone | Status | Depends On | Summary |
|---|-----------|--------|------------|---------|
| M1 | [Hook Protocol & Claude Code Integration](M1-HOOK-PROTOCOL.md) | **Done** | — | Verify and fix the hook contract; document session_id, transcript_path, and hook I/O schema |
| M2 | [Core Pipeline Hardening](M2-CORE-PIPELINE.md) | **Done** | M1 | Production-ready pipeline: error handling, timeouts, streaming Ollama, graceful degradation |
| M3 | [Prompt Engineering & Output Format](M3-PROMPT-ENGINE.md) | **Done** | M2 | v4: LLM outputs JSON classification, Go composes XML via compose.go. Dynamic footer, type-gated guardrails, LLM-aware of downstream effects. Passthrough detection, versioned prompts |
| M4 | [Server & Dashboard](M4-SERVER-DASHBOARD.md) | **Done** | M1, M2 | Chi server, React SPA (wouter routing), SQLite data layer, SSE hub, streaming pipeline, /api/info endpoint, 4-panel refinement detail, `input_prompt` + `llm_output` DB columns |
| M5 | [Rules Engine & Context Gathering](M5-RULES-ENGINE.md) | **~60% Done** | M2 | Rules loader walks to git root, LLM selects rules by index, "After Every Change" = process guardrails, git stripped to branch+commits, LLM generates recent_activity summary, session clips from DB. Needs file mention detection |
| M6 | [Caching & Performance](M6-CACHE-PERF.md) | **~40% Done** | M2, M4 | File-based cache, SHA256(prompt+rulesHash). Needs SQLite migration, TTL, LRU eviction |
| M7 | [CLI UX & Configuration](M7-CLI-UX.md) | **~70% Done** | M2 | Doctor, model, config commands implemented. Needs auto-fix, error polish, completions, version check |
| M8 | [Plugin Distribution & Installation](M8-PLUGIN-DIST.md) | **~60% Done** | M1, M7 | Plugin manifest, skills, cross-platform builds done. Needs install flow, uninstall, release automation |
| M8.1 | [Build System (Make → xmake)](M8.1-BUILD-SYSTEM.md) | **Done** | — | xmake + pnpm workspaces, debug/release modes, `pnpm build` respects config, `pnpm release` for explicit release |
| M8.2 | [Verification System](M8.2-VERIFICATION.md) | **Done** | M1, M4 | Mechanical enforcement via hooks: snapshot on TaskCreated, verify on TaskCompleted/Stop, exit code 2 blocks task on failure. Per-project `.restruct/verify.yaml` config |
| M9 | [Testing & Calibration](M9-TESTING.md) | **~50% Done** | M3, M4 | 213 tests across 13 packages. Integration tests use real CLAUDE.md + git. Needs eval framework, >80% coverage |
| M10 | [Self-Improvement Loop](M10-SELF-IMPROVE.md) | Not Started | M4, M9 | Dashboard-driven feedback loop: rating analysis, system prompt refinement, rules suggestions |
| M11 | [Project Bootstrap & Deep Context](M11-PROJECT-BOOTSTRAP.md) | Not Started | M1, M2, M5 | SessionStart scans CLAUDE.md files, generates deep-context docs in .restruct/links/, returns project map as additionalContext + watchPaths. Refinement uses retrieval-augmented rule selection |
| M12 | [Smart Tool Permissions](M12-TOOL-PERMISSIONS.md) | Not Started | M1, M8 | PreToolUse hook auto-approves read-only + project-scoped tools, gates writes by auto-mode, detects network data exfiltration. Agent review for borderline cases. `.restruct/permissions.yaml` for allowed paths |
| M13 | [Team Configuration](M13-TEAM-CONFIG.md) | Not Started | M7, M8 | Layered config: `.restruct/config.yaml` (team, checked in) → user config → env vars → plugin options (secrets). `restruct init` scaffolding. Selective `.gitignore` |
| M14 | [Interactive Clarification](M14-INTERACTIVE-CLARIFICATION.md) | Not Started | M1, M2, M3 | Prompt Request Protocol surfaces LLM-detected ambiguity to user during hook execution. Answers folded into additionalContext. Eliminates Claude round-trip for ambiguous prompts |
| M15 | [PostCompact Rule Persistence](M15-POSTCOMPACT-REINJECT.md) | Not Started | M1, M11 | Session-scoped rule statistics with generation counters track which rules matter. PostCompact hook reinjects top-scored rules with exponential decay weighting. PreCompact hint for compaction summary quality |
| M16 | [Subagent Rule Alignment](M16-SUBAGENT-ALIGNMENT.md) | Not Started | M1, M11, M8.2 | SubagentStart injects tailored rules based on agent type + task context. Agent-type profiles (Explore/Plan/general). SubagentStop extends verification to subagent output |
| M17 | [Tool Input Interception](M17-TOOL-INTERCEPT.md) | Not Started | M1, M11 | PreToolUse rewrites wrong package manager/build tool/test runner commands. Auto-discovered from lock files + user-editable corrections in config. Low priority |
| M18 | [Verification Failure Guidance](M18-TEST-FAILURE-GUIDANCE.md) | Not Started | M1, M11 | PostToolUse injects behavioral directives when tests/linter/typecheck/vet fail. Prevents suppression shortcuts (nolint, as casts, ts-ignore). Links to relevant deep-context docs. Configurable command patterns |

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

### M3: Prompt Engineering & Output Format (Done)

| Task | Status | Notes |
|------|--------|-------|
| 3.1 — Injection framing | ✅ Done | `prompt/framing.go` — preamble header. Dynamic footer built by `compose.go` — only references sections present in output |
| 3.2 — System prompt optimization | ✅ Done | v4: LLM outputs JSON (type, intent, recent_activity, analysis, relevant_rules[int], relevant_anti_patterns[int], clarification). Prompt explains downstream consequences. Versioned at `versions/v4_classify.tmpl` |
| 3.3 — Context builder optimization | ✅ Done | `builder.go` ParseRules → numbered context rules + anti-patterns + process guardrails. `compose.go` assembles XML from LLM JSON + static data. Rules priority in budget: rules > session > git |
| 3.4 — Prompt template as configurable asset | ✅ Done | Search path: `.restruct/system_prompt.tmpl` → `~/.config/restruct/system_prompt.tmpl` → embedded default. Version history in `versions/` |
| 3.5 — Passthrough detection | ✅ Done | `pipeline/passthrough.go` — heuristic detection: affirmatives, follow-ups, numbered selections, slash commands. 64 test cases |
| 3.6 — Refinement quality smoke tests | ✅ Done | 32 tests across pipeline + prompt packages (passthrough, framing, builder, template, validation, NoContext sentinel) |

### M4: Server & Dashboard (Done)

| Task | Status | Notes |
|------|--------|-------|
| 4.1 — SQLite data layer | ✅ Done | `internal/db` package: WAL mode, embedded migrations, Session/Refinement/PipelineEvent models, full CRUD, metrics aggregation |
| 4.2 — Go server (Chi) | ✅ Done | `internal/server`: Chi router, `/api/events` (SSE), `/api/health`, `/api/metrics`, `/api/sessions/*`, `/api/refinements/*`. Middleware: logging, recovery, compression |
| 4.3 — Pipeline recording (direct to SQLite) | ✅ Done | `db/recorder.go`: `RecordSession()`, `RecordRefinement()`, `RecordPipelineEvent()`. CLI writes on every hook invocation |
| 4.4 — React SPA (Vite) | ✅ Done | Dashboard, Sessions, SessionDetail, RefinementDetail pages. API client, SSE hook, ShadcN components, Tailwind |
| 4.5 — Dev server flow | ✅ Done | Build tags: debug → CORS + nil FS (Vite proxy), release → embedded SPA. `pnpm dev` runs both |
| 4.6 — Daemon management | ✅ Done | `restruct serve --daemon`, PID file, `serve stop`, `serve status`. Setsid for background process |
| 4.7 — Streaming Ollama integration | ✅ Done | `ChatStream()` → `HttpTokenSink` → POST to server → SSE hub → browser `useStreamingTokens` hook → `StreamingCard`. Full chain wired |
| 4.8 — SSE live updates (DB polling) | ✅ Done | `sse/hub.go`: pub-sub pattern, 1s DB poll, `refinement:new` broadcasts to all connected clients |

### M5: Rules Engine & Context Gathering (~60% Done)

| Task | Status | Notes |
|------|--------|-------|
| 5.1 — Hierarchical rules loading | ✅ Done | `rules/loader.go`: walks up directory tree to git root. Finds CLAUDE.md even when hook cwd is a subdirectory |
| 5.2 — Rules relevance filtering (LLM-assisted) | ✅ Done | LLM selects rules by numbered index. All rules (except "After Every Change") presented as numbered context list. Anti-patterns in separate numbered list. LLM's system prompt explains consequences of empty selection |
| 5.3 — Smart git context | ✅ Done | Stripped to branch + 5 commit messages only. LLM generates `recent_activity` summary from commits, flagging breaking changes from conventional commit syntax. No file lists |
| 5.4 — Conversation-aware refinement | ⚠️ Partial | Session clip extraction from DB (`GetRecentIntents()`), intent parsing from refined prompts. Transcript parsing still needed |
| 5.5 — File mention detection | ❌ | Extract context for mentioned files |
| 5.6 — Rules loader tests | ⚠️ Partial | Integration tests use real CLAUDE.md + git. Walk-up-to-root tested. Needs >80% coverage |

### M6: Caching & Performance (~40% Done)

| Task | Status | Notes |
|------|--------|-------|
| 6.1 — Cache key improvements | ⚠️ Partial | SHA256(prompt + rulesHash). Git state removed from key (LLM summary varies anyway). Session context excluded. Needs normalization, model-scoping |
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

### M9: Testing & Calibration (~45% Done)

| Task | Status | Notes |
|------|--------|-------|
| 9.1 — Unit test coverage | ⚠️ Partial | 213 tests across 13 packages. Integration tests exercise real CLAUDE.md, real git, full compose pipeline. Compose tests verify type-gated sections, index resolution, dynamic footer. Needs >80% across all packages |
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
| Verification enforcement | Hook exit code 2, not LLM instructions. `restruct verify` runs shell checks and blocks on failure. Claude cannot ignore a hook exit code | M8.2 implementation |
| Verification scope | Per-check globs filter which checks run against changed files. TypeScript changes don't trigger Go vet | M8.2 implementation |
| Snapshot strategy | `git ls-files` for discovery (respects .gitignore), mtime+size for diff (fast, no content hashing). Task scope promotes to prompt scope on pass | M8.2 implementation |
| Debug/release | Go build tags (`//go:build debug`). `embed_debug.go` returns nil FS + dev mode, `embed_release.go` embeds web dist | M8.1 implementation |
| Dev workflow | `pnpm dev` → `xmake watch -d cli -r` (auto-rebuild + restart Go server) + Vite HMR, in parallel via pnpm workspaces | M8.1 implementation |

## Open Decisions (Remaining)

- **M5:** Transcript parsing depth for conversation-aware refinement
- **M10:** How to safely auto-suggest system prompt changes

## Recently Resolved

| Decision | Resolution | Resolved In |
|----------|-----------|-------------|
| Injection framing text | One-line preamble: `[Project rules analysis for the request above. Follow these constraints during implementation.]` + `<context>` XML block (renamed from `<context_supplement>` for stronger signal) | M3 implementation |
| System prompt rewrite | v3: renamed tags (`<context>`, `<analysis>`, `<clarification_needed>`), post-process constraint injection, plan-mode behavioral directives. Versioned at `versions/v3_considerations.tmpl` | M3 v3 update |
| Workflow section | Retained. ReAct research supports it (+34% on ALFWorld). Recency bias requires re-injection to counteract drift. Kept concise: 4-step Investigate → Plan → Implement → Verify | M3 implementation |
| RE2 re-reading | Applied. Local LLM generates `<intent>` section restating request in precise terms. Claude reads casual prompt (pass 1), then precise restatement alongside rules (pass 2). Xu et al. EMNLP 2024: +2-5 pts across reasoning benchmarks | M3 implementation |
| XML tag naming | No tag names carry special weight in Claude — compliance comes from content, not tag names. Renamed for clarity: `<context_supplement>` → `<context>`, `<considerations>` → `<analysis>`, `<ambiguities>` → `<clarification_needed>`. Based on Anthropic docs research | M3 v3 update |
| Post-process constraints | System-level directives (plan mode, sub-agent delegation) injected after LLM output via `appendConstraints()`, not baked into the local LLM's system prompt. Keeps local LLM focused; avoids prompt leakage | M3 v3 + M5 update |
| Plan mode is a permission mode | Claude Code plan mode is a harness-level permission setting, not a prompt trigger. No magic keywords exist. Used behavioral instruction ("present a plan and wait for approval") instead | M3 v3 update |
| Cache key includes git state | `buildCacheKey()` includes staged + working diff stats. Same prompt with different edits = different cache entry. Session context excluded (doesn't affect structural output) | M5/M6 update |
| Session context via DB | Recent refinement intents queried from SQLite, formatted as session clips ("2m ago: Fixed auth..."). Avoids transcript parsing overhead. Capped at 400 chars | M5 update |
| Git calls parallelized | 5 concurrent goroutines (branch, log, HEAD~1 stat, staged stat, working stat) instead of 3 sequential calls. ~50% faster git context gathering | M5 update |
| v4 JSON classification | LLM outputs JSON (not XML). Go composes final XML via `compose.go`. LLM tokens drop ~70% — only produces classification + analysis, not formatting | M3 v4 rewrite |
| LLM-selected rules by index | All rules (except "After Every Change") presented as numbered list. LLM selects by index. Empty selection = Claude gets no rules. System prompt explains consequences | M3/M5 v4 |
| Process guardrails | Only "## After Every Change" items are always injected (in `<constraints>`). All other rules are LLM-selected context. Build commands, code style, architecture = context rules | M5 v4 |
| Git context stripped | Removed file lists (staged, working, HEAD~1 stat). LLM only sees branch + commit messages. Produces `recent_activity` one-line summary from conventional commits | M5 v4 |
| LLM knows its downstream effects | System prompt explains: type controls guardrails, empty rules = no rules for Claude, clarification triggers MUST-ask, analysis is Claude's only window into git/session | M3 v4 |
| Dynamic footer | Footer only references XML sections actually present. Questions don't get told about `<constraints>`. Built by `compose.go:buildFooter()` | M3 v4 |
| Anti-patterns ungated | Available for all request types, not just implementation. LLM can select anti-patterns for questions too | M3 v4 |
| `input_prompt` + `llm_output` columns | DB stores full LLM input (system + user message) and raw LLM response. Migrations 003, 004. Visible in web 4-panel detail view | M4 v4 |
| `/api/info` endpoint | Returns version, build mode (debug/release), DB path, plugin ID. Shown in web footer | M4 v4 |
| wouter routing | Web SPA uses wouter for URL-based navigation. Browser back/forward works. Routes: /, /sessions, /sessions/:id, /refinements/:id, /stats | M4 v4 |
| xmake release group fix | Release group targets now respect `is_mode("debug")` tag. `pnpm build` no longer reconfigures to release. `pnpm release` is new command for explicit release builds | M8.1 v4 |
| Rules loader walks to git root | `rules/loader.go` searches from cwd up to git root for CLAUDE.md. Fixes bug where hook cwd in subdirectory missed project rules | M5 v4 |
| `repo_state` in Claude output | `<repo_state>Branch: main | LLM activity summary</repo_state>`. Branch is static, activity is LLM-generated from commit messages | M3/M5 v4 |
| compose.go extraction | Context composition logic extracted from pipeline.go. Single file for `composeContext()`, `buildFooter()`, `needsImplementationGuardrails()` | M3 v4 |

---

## Dependency Graph

```
M1  (Hook Protocol)        ██████████  Done
 │
 ├── M2  (Core Pipeline)   ██████████  Done
 │    ├── M3  (Prompt Engine)    ██████████  Done (v4 JSON)
 │    ├── M4  (Server)           ██████████  Done (4-panel, /api/info)
 │    ├── M5  (Rules Engine)     ██████░░░░  ~60%
 │    ├── M6  (Cache & Perf)     ████░░░░░░  ~40%
 │    └── M7  (CLI UX)           ███████░░░  ~70%
 │                                    │
 │                                    └──► M8  (Plugin Dist)  ██████░░░░  ~60%
 │
 ├── M8.1 (Build System)   ██████████  Done
 │
 ├── M8.2 (Verification)   ██████████  Done
 │    │    (M1 + M4)
 │    │
 │    M3 + M4 ──► M9  (Testing)      █████░░░░░  ~50%
 │                └──► M10 (Self-Improve)  ░░░░░░░░░░
 │
 ├── M11 (Project Bootstrap)  ░░░░░░░░░░
 │    (M1 + M2 + M5)
 │    SessionStart deep-context, retrieval-augmented refinement
 │
 └── M12 (Tool Permissions)   ░░░░░░░░░░
      (M1 + M8)
      PreToolUse auto-approval, exfil detection, permissions.yaml
```

**M1–M4, M8.1, and M8.2 are complete.** Major session changes: v4 JSON classification pipeline (M3), LLM-selected rules by index (M5), git stripped to branch+commits with LLM activity summary (M5), 4-panel web detail view with `input_prompt`/`llm_output` (M4), wouter routing (M4), compose.go extraction, dynamic footer, xmake build mode fix, mechanical verification via hooks (M8.2). M5 needs file mention detection. M6 needs SQLite cache migration. M9 at 213 tests with real-data integration tests.

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
