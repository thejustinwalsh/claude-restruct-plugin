# M10: Self-Improvement Loop

## Goal

Build a feedback loop where refinement outcomes inform future refinement quality — the system gets better the more it's used. The dashboard (M4) provides the rating UI; this milestone builds the analysis and self-refinement engine on top of that data.

## Depends On

- M4 (Server & Dashboard) — ratings and refinement data live in SQLite, accessed via the dashboard
- M9 (Testing & Calibration) — need the evaluation framework to measure improvement

---

## Tasks

### 10.1 — Outcome Tracking (Built on M4 Data)

**What:** Enrich the refinement records from M4's SQLite with outcome signals.

**M4 already captures:** raw prompt, refined prompt, model, latency, cache hit, session_id, and user ratings.

**This milestone adds:**
- Whether the user sent a correction follow-up after refinement (detected via subsequent UserPromptSubmit hooks with correction patterns: "no", "that's wrong", "I meant", "try again")
- Session duration after each refinement
- Whether the session ended successfully (Stop event) or was abandoned
- Computed outcome score: weighted combination of explicit rating + implicit signals

**Privacy:**
- All data stays local (already in M4's SQLite)
- `restruct config set tracking.enabled false` to disable outcome tracking
- No telemetry, no network calls

### 10.2 — Refinement Outcome Hooks

**What:** Use Claude Code hooks to capture post-refinement signals.

**Hook events to monitor:**
- **UserPromptSubmit (subsequent):** If the user sends a follow-up that looks like a correction ("no, I meant...", "that's wrong", "try again"), mark the previous refinement as potentially low-quality.
- **Session end:** Record session length after refinement.

**Implementation:**
- Add a lightweight post-refinement hook that logs outcomes
- Keep processing minimal — this must not add latency
- Write to outcomes DB asynchronously (fire-and-forget goroutine or separate command)

### 10.3 — LLM-as-Evaluator Pipeline

**What:** Periodically evaluate accumulated refinements to identify patterns.

**`restruct improve analyze`:**
1. Load recent outcomes from DB (last N sessions or last 7 days)
2. Group by quality signal (success vs. correction-needed)
3. For successful refinements: identify what made them work
4. For failed refinements: identify what went wrong
5. Use Claude (API) or local LLM to analyze patterns
6. Output: list of suggested improvements to the system prompt or rules

**Analysis prompt for the evaluator LLM:**
```
Here are 10 prompt refinements that led to good outcomes and 5 that needed correction.

Good outcomes:
[raw → refined pairs with success signals]

Needed correction:
[raw → refined pairs with correction signals]

Analyze the patterns. What did successful refinements do well? What did failed ones get wrong? Suggest specific changes to the system prompt template to improve future refinements.
```

### 10.4 — System Prompt Suggestions

**What:** Based on analysis, suggest concrete changes to the system prompt.

**Output format:**
```
restruct improve suggest

Suggestions based on 47 refinements (last 7 days):

1. ADD few-shot example for refactoring requests
   Reason: 3/5 refactoring refinements were over-scoped. Users had to narrow them down.
   Confidence: High (consistent pattern)

2. MODIFY uncertainty threshold for bug reports
   Reason: Too many "ask the user" directives for bug reports that had enough context.
   Confidence: Medium (4 cases, 2 ambiguous)

3. ADD anti-pattern: don't split single-file changes into multi-step workflows
   Reason: Simple changes were over-structured, adding unnecessary steps.
   Confidence: High (6 cases)

Apply suggestion 1? [y/n/all/skip]
```

**Safety:**
- Suggestions are presented, never auto-applied
- Each suggestion shows reasoning and confidence
- User approves individually or in batch
- Applied changes are tracked (can be reverted)

### 10.5 — Rules File Suggestions

**What:** Suggest additions to the project's `agents.md` based on observed patterns.

**When patterns emerge from evaluations:**
- A constraint is consistently added by refinement → suggest making it a permanent rule
- A specific anti-pattern keeps appearing → suggest adding it to rules
- A workflow step is always relevant → suggest codifying it

**`restruct improve rules`:**
```
Based on your refinement history, these patterns appear consistently:

1. "All database changes require migration files"
   - Appeared in 8/12 database-related refinements
   - Suggest adding to agents.md

2. "API endpoints must include error response documentation"
   - Appeared in 5/7 API-related refinements
   - Suggest adding to agents.md

Add to agents.md? [y/n/edit/skip]
```

### 10.6 — Improvement Metrics Dashboard

**What:** Track whether the self-improvement loop is actually improving things.

**`restruct improve stats`:**
```
Refinement Quality Trends (last 30 days):

  Structural compliance:  92% → 96% (+4%)
  Intent preservation:    85% → 91% (+6%)
  User corrections:       23% → 12% (-11%)  ← fewer corrections = better
  Avg refinement latency: 1.4s → 1.2s (-14%)
  Cache hit rate:         34% → 52% (+18%)

System prompt versions: 3 (2 improvements applied)
Rules suggestions: 4 accepted, 1 rejected
```

**Data source:** Outcomes DB + periodic evaluation runs.

### 10.7 — Self-Refining System Prompt

**What:** The system uses its own LLM pipeline to refine its own system prompt based on accumulated feedback data.

**This is the core self-improvement mechanism:**
1. Collect rated refinements from SQLite (via M4 dashboard ratings)
2. Group into high-rated (4-5 stars) and low-rated (1-2 stars) sets
3. Send both sets to the local LLM with a meta-meta-prompt:
   ```
   You are analyzing the effectiveness of a system prompt used for prompt refinement.

   Here is the current system prompt: [current template]

   Here are refinements that users rated highly: [high-rated pairs]
   Here are refinements that users rated poorly: [low-rated pairs]

   Produce a revised system prompt that would improve the low-rated cases
   while preserving what works in the high-rated cases.
   Output ONLY the revised system prompt.
   ```
4. The revised prompt becomes a new version in the `system_prompts` table
5. Dashboard shows the diff between current and proposed version
6. User approves via dashboard → new version becomes active
7. Future refinements use the new version, creating a continuous improvement cycle

**Safety:**
- New versions are NEVER auto-activated
- Dashboard shows side-by-side diff of old vs new
- User can A/B test: run the same prompt through both versions and compare
- Rollback is one click: reactivate any previous version
- Each version tracks its own aggregate rating so regression is detectable

### 10.8 — Automated Evaluation Runs

**What:** Schedule periodic evaluation to track quality trends.

**Options:**
- `restruct improve watch` — daemon that runs evaluation after every N refinements
- Cron-based: user adds `restruct improve analyze --quiet` to their crontab
- Manual: user runs `restruct improve analyze` when they want insights

**Recommendation:** Start with manual + a nudge. After 50 refinements, `restruct refine` prints once: "You've done 50 refinements. Run `restruct improve analyze` to see how quality is trending."

---

## Acceptance Criteria

- [ ] Outcomes tracked in local SQLite DB with full privacy controls
- [ ] Post-refinement hooks capture correction signals
- [ ] `restruct improve analyze` identifies patterns in successes/failures
- [ ] `restruct improve suggest` proposes system prompt changes with reasoning
- [ ] `restruct improve rules` suggests project rules based on patterns
- [ ] `restruct improve stats` shows quality trends over time
- [ ] All suggestions require user approval (never auto-applied)

## Files Modified

- New: `cli/cmd/improve.go` — improve command with subcommands
- New: `cli/internal/outcomes/` — outcome tracking, SQLite storage
- New: `cli/internal/eval/analyzer.go` — pattern analysis
- New: `cli/internal/eval/suggester.go` — prompt/rules suggestions
- `cli/cmd/refine.go` — outcome recording hook
- `cli/internal/config/config.go` — tracking config
- `go.mod` — SQLite dependency (`modernc.org/sqlite`)

## Risk

**High.** This is the most experimental milestone. Risks:
1. **Signal quality:** User corrections are a noisy proxy for refinement quality. A follow-up doesn't always mean the refinement was bad.
2. **Analysis quality:** The evaluator LLM may produce vague or wrong suggestions. Mitigate with structured analysis prompts and human-in-the-loop approval.
3. **Scope creep:** Easy to over-engineer. Start with manual analysis and simple suggestions. Add automation only after validating the approach works.

**Mitigation:** Ship 9.1-9.3 first. If the analysis produces useful insights, continue to 9.4-9.7. If not, revisit the approach before building more.
