# M3: Prompt Engineering & Output Format

## Goal

Optimize the system prompt and determine the output format that yields the most consistent, high-quality refinements. This is the core intellectual work of the project — the quality of the system prompt directly determines the value of the entire tool.

## Depends On

M2 (Core Pipeline) — need a reliable pipeline before tuning prompt quality.

---

## Tasks

### 3.1 — Output Format Evaluation

**What:** Empirically determine whether XML, markdown, or a hybrid format produces the most consistent and effective refined prompts.

**Approach:**
1. Create a test harness that sends 20+ diverse prompts through the pipeline
2. Test three output format variants:
   - **XML:** Current approach (`<structured_prompt><objective>...`)
   - **Markdown:** Headers and bullets (`## Objective\n- ...`)
   - **Hybrid:** Markdown with XML for key directives only
3. For each format, evaluate:
   - **Consistency:** Does the LLM reliably produce the expected structure?
   - **Claude compliance:** Does Claude follow the embedded constraints?
   - **Readability:** Can users understand what was sent on their behalf?
   - **Parseability:** Can we validate the output programmatically?

**Decision criteria:**
- Consistency is king. The format that the local LLM produces most reliably wins.
- If consistency is tied, prefer the format Claude responds to most reliably.
- Document the decision and rationale in `docs/OUTPUT-FORMAT.md`.

### 3.2 — System Prompt Optimization

**What:** Iterate on `prompt/system_prompt.tmpl` to maximize refinement quality.

**Current system prompt analysis:**
The existing template is solid but generic. Improvements needed:

1. **Few-shot examples:** Add 2-3 concrete input→output examples directly in the system prompt. Research shows few-shot dramatically improves structured output adherence for smaller models.

2. **Format enforcement:** Add explicit format instructions with the chosen format from 3.1. Include a "your output MUST start with X and end with Y" directive.

3. **Negative examples:** Add one example of what NOT to produce (over-verbose, loses user intent, adds hallucinated constraints).

4. **Calibrated uncertainty:** Current prompt says "if ambiguous, tell agent to ask." Refine this: define what counts as ambiguous vs. what the LLM should resolve itself. Too many "ask the user" directives make the tool annoying.

5. **Context-aware instructions:** The system prompt should behave differently based on:
   - Whether rules files exist (generic vs. rule-reinforced refinement)
   - Whether git context is available (repo-aware vs. standalone)
   - Prompt length/complexity (light touch vs. full restructuring)

### 3.3 — User Prompt Template Optimization

**What:** Improve `prompt/builder.go`'s user message construction.

**Current approach:** Concatenates raw prompt + rules + git context with markdown headers.

**Improvements:**
- **Priority ordering:** Put the raw prompt first (most important), then rules, then git context. LLMs attend more to the beginning.
- **Rules relevance:** Don't dump all rules. If rules are long (>500 lines), include a summary or use the LLM to select relevant sections. (Simple approach first: truncate to most recent/relevant sections.)
- **Git context formatting:** Current format is raw text. Structure it so the LLM can parse branch name, recent commits, and changed files distinctly.
- **Token budget management:** Estimate total input tokens. If approaching the model's context limit, truncate rules and git context (never truncate the user's prompt).

### 3.4 — Prompt Template as Configurable Asset

**What:** Allow advanced users to customize the system prompt template.

**Requirements:**
- Default template ships embedded in the binary (current behavior via `embed`)
- Users can override by placing a custom template at `~/.config/restruct/system_prompt.tmpl` or `.restruct/system_prompt.tmpl` in the project
- Template uses Go `text/template` syntax with access to variables: `{{.RawPrompt}}`, `{{.Rules}}`, `{{.GitContext}}`, `{{.HasRules}}`, `{{.HasGitContext}}`
- `restruct config get system-prompt` prints the active template
- Document template customization in the setup skill

### 3.5 — Passthrough Detection

**What:** Detect when a prompt is already well-structured and skip or minimize refinement.

**Heuristics for passthrough:**
- Prompt already contains structural markers (XML tags, numbered steps, explicit constraints)
- Prompt is a follow-up that references prior conversation ("yes do that", "try option 2")
- Prompt is a command, not a request ("git status", "/help", "exit")

**Implementation:**
- Add a `ShouldRefine(prompt string) bool` function in the pipeline
- Check for structural markers, very short prompts (existing <5 word check), and command patterns
- Log when passthrough is triggered and why

### 3.6 — Refinement Quality Smoke Tests

**What:** A small, curated set of prompt pairs (input → expected output characteristics) that run as part of `make test`.

**Test cases (10 minimum):**
| Input | Expected Output Must Contain |
|-------|------------------------------|
| "fix the auth bug" | uncertainty directive (which auth bug?), investigation step |
| "add dark mode" | scope question, UI framework reference |
| "refactor the database layer" | plan-before-implement, risk assessment |
| "why is the app slow" | investigation workflow, not immediate fix |
| "update the README" | minimal refinement (simple task) |
| "" (empty) | passthrough |
| "y" | passthrough (follow-up) |
| Already-structured XML prompt | passthrough or minimal touch |
| Very long prompt (500+ words) | preserves all user intent, doesn't truncate meaning |
| Prompt with code snippets | preserves code blocks verbatim |

**Note:** These test the template/heuristics, not the LLM output (that's M8).

---

## Acceptance Criteria

- [ ] Output format decided and documented with empirical evidence
- [ ] System prompt includes few-shot examples and format enforcement
- [ ] Passthrough detection prevents over-refinement
- [ ] Template is configurable for advanced users
- [ ] 10+ prompt-pair smoke tests pass

## Files Modified

- `cli/internal/prompt/system_prompt.tmpl` — optimized system prompt
- `cli/internal/prompt/builder.go` — improved message construction
- `cli/internal/prompt/template.go` — custom template loading
- `cli/internal/pipeline/pipeline.go` — passthrough detection
- `docs/OUTPUT-FORMAT.md` — format decision documentation

## Risk

**High impact.** This milestone determines the core value proposition. Bad prompt engineering = useless tool regardless of how well everything else works. Plan for multiple iteration cycles.
