# M15: PostCompact Rule Persistence & Session Rule Statistics

## Goal

Ensure project rules survive context compaction by re-injecting the most impactful rules after every compaction event. Use session-scoped statistical tracking of rule selections across refinements to intelligently select which rules to reinject — prioritizing rules that were repeatedly selected by the LLM during this session, weighted by recency relative to compaction generations.

## Depends On

M1 (Hook Protocol), M11 (Project Bootstrap — .restruct/links/ system, project map)

---

## Problem Statement

When a conversation triggers auto-compaction, our `additionalContext` from earlier turns is summarized or dropped. Claude loses the structured project rules we injected. Long conversations gradually drift from project conventions because the rules that guided early turns are no longer in the context window.

Naively re-injecting all rules wastes the limited post-compaction budget. We need to reinject the rules that *actually mattered* during this session — the ones the local LLM repeatedly selected as relevant.

## Solution

### Two Components

**1. Session Rule Statistics** — Track which rules and documents are selected across refinements within a session. Maintain generation counters that reset accumulation at each compaction, so post-compaction reinject reflects the most recent working context, not stale early-session patterns.

**2. PostCompact Hook** — After compaction, load session rule stats, select the highest-impact rules, and inject a condensed "always-on" rule summary as additionalContext.

### Architecture

```
Every UserPromptSubmit refinement:
  → LLM selects rules by index + documents by index
  → Record selections in session_rule_stats (in-memory + DB)
  → Increment hit counts, track generation counter

PostCompact hook fires:
  → Increment generation counter (marks compaction boundary)
  → Load session_rule_stats for current session
  → Score rules: hits_since_last_compact * recency_weight
  → Select top-N rules (budget: ~1.5KB of additionalContext)
  → Load full rule text from .restruct/links/
  → Compose condensed reinject context
  → Return as additionalContext

PreCompact hook fires (optional enhancement):
  → Inject hint into compaction: "These rules are critical, preserve in summary"
```

### Session Rule Statistics Model

```go
type SessionRuleStats struct {
    SessionID   string
    Generation  int                    // incremented at each compaction
    Rules       map[string]*RuleStat   // key: "doc_hash:rule_index"
    Documents   map[string]*DocStat    // key: doc_hash
}

type RuleStat struct {
    RuleText       string  // the actual rule text (for reinject)
    Source         string  // source file path
    TotalHits      int     // total selections this session
    HitsByGen      []int   // hits per generation [gen0_hits, gen1_hits, ...]
    LastSelectedAt time.Time
}

type DocStat struct {
    DocHash        string
    Source         string
    TotalHits      int
    HitsByGen      []int
    LastSelectedAt time.Time
}
```

### Scoring Algorithm

After compaction (entering generation N), score each rule:

```
score = hits_in_gen[N-1] * 1.0        // most recent generation (full weight)
      + hits_in_gen[N-2] * 0.5        // previous generation (half weight)
      + hits_in_gen[N-3] * 0.25       // two generations back (quarter weight)
      + ...                            // exponential decay
```

Rules with score > threshold are selected for reinject. Budget cap: top 10 rules or ~1.5KB of text, whichever is smaller. "After Every Change" process guardrails are always included regardless of score.

### Generation Counter Logic

| Event | Generation Counter | Accumulation |
|-------|-------------------|--------------|
| Session start | gen = 0 | Fresh start |
| Each refinement | gen unchanged | Accumulate hits in current gen bucket |
| Compaction | gen++ | New bucket starts accumulating. Previous buckets frozen |
| Session end | — | Stats persisted to DB for dashboard. Not carried to next session |

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 15.1 — Session rule stats tracker | 3h | New `internal/stats/session_rules.go`: in-memory `SessionRuleStats` with `RecordSelection(docHash, ruleIndices)`, `IncrementGeneration()`, `ScoreRules()`. Thread-safe (hook invocations are parallel). Serializable to JSON for DB persistence |
| 15.2 — Pipeline integration for stat recording | 2h | After `compose` stage in `pipeline.go`, call `stats.RecordSelection()` with the LLM's selected rule indices and document indices. Pass session stats tracker through pipeline context |
| 15.3 — Reinject command | 3h | New `cmd/reinject.go`: PostCompact hook handler. Reads hook input (session_id, trigger: auto/manual). Increments generation. Scores rules. Loads top-N rule text from `.restruct/links/`. Composes condensed `<reinject>` XML. Returns as additionalContext |
| 15.4 — Reinject context composition | 2h | New `internal/pipeline/reinject.go`: `composeReinjectContext(topRules, generation)` produces XML with source attribution and generation metadata. Format: `<reinject generation="3">` with `<rule source="CLAUDE.md">...</rule>` children. Always includes process guardrails |
| 15.5 — PreCompact hint (optional) | 1.5h | PreCompact hook handler: inject a system message hinting which rules are critical. "The following project rules have been consistently applied this session and should be preserved in the compaction summary: [list]". Low-effort, may improve compaction summary quality |
| 15.6 — Plugin.json hook wiring | 0.5h | Add PostCompact and PreCompact hooks to plugin.json. PostCompact: `restruct reinject`, timeout 5s. PreCompact: `restruct reinject --hint`, timeout 3s |
| 15.7 — DB persistence | 2h | Migration for `session_rule_stats` table: session_id, generation, stats_json (serialized SessionRuleStats), created_at, updated_at. Persist on each compaction and at session end. Used by dashboard, not for cross-session carry-over |
| 15.8 — Dashboard integration | 2h | Session detail view shows: rule selection frequency heatmap across generations, compaction boundaries marked, which rules were reinjected after each compaction. Helps users understand which rules are "working" |
| 15.9 — Tests | 3h | Stats tracker: record, score, generation increment, decay weighting. Reinject composer: top-N selection, budget cap, process guardrails always included. Integration: multi-generation session with compaction, verify reinject selects correct rules |

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Stats scope | Session-only, not cross-session | Each session has different context. Cross-session stats would require normalization for rule changes between sessions |
| Stats storage | In-memory during session, DB at compaction + session end | Fast recording during refinement. DB only for persistence/dashboard |
| Generation model | Counter incremented at compaction | Clean boundary for "what happened before vs after compact". Exponential decay weights recent generations higher |
| Reinject budget | Top 10 rules or ~1.5KB | Post-compaction context is already tight. Must be concise. 1.5KB ≈ 375 tokens — small but impactful at max recency position |
| Process guardrails | Always reinjected | "After Every Change" workflow rules are structural, not statistical. Always included regardless of score |
| PreCompact hint | Optional enhancement | May or may not improve compaction summary. Low effort to try. Separate from the core reinject mechanism |
| Scoring algorithm | Exponential decay by generation | Recent usage is more predictive than historical. Rules selected right before compaction are most likely needed after |

## Acceptance Criteria

- Rule selections are tracked per refinement with generation counters
- Compaction increments the generation counter
- PostCompact hook fires and reinjcts top-scored rules as additionalContext
- Process guardrails are always included in reinject
- Budget stays under 1.5KB
- Rules from the most recent generation are weighted highest
- Dashboard shows rule frequency across generations with compaction boundaries
- Session stats are persisted to DB at compaction and session end
- Stats do not carry over between sessions
- Graceful degradation: if no stats exist (first compaction before any refinement), reinject falls back to process guardrails only

## Risk

**Low-Medium.** PostCompact is a well-defined hook point. The stats model adds in-memory state to the refinement pipeline, but it's append-only and small. The main risk is the reinject context competing for space in an already-pressured context window — the 1.5KB budget cap mitigates this.

## Files

**New:**
- `cli/internal/stats/session_rules.go` — session rule statistics tracker
- `cli/internal/pipeline/reinject.go` — reinject context composition
- `cli/cmd/reinject.go` — PostCompact/PreCompact hook handler

**Modified:**
- `cli/internal/pipeline/pipeline.go` — record selections after compose
- `plugin/.claude-plugin/plugin.json` — add PostCompact + PreCompact hooks
- `cli/internal/db/` — migration for session_rule_stats table
