# M3: Prompt Engineering & Output Format

## Goal

Design the `additionalContext` injection that steers Claude toward structured, rules-aware execution. This is NOT prompt replacement — Claude always sees the user's original prompt first, then our injected context. We exploit **recency bias** (LLMs weight later context more heavily) and **rule saturation** (keeping relevant constraints in the active context window) to improve execution quality without the user seeing or managing the structured output.

## How It Actually Works

```
┌─────────────────────────────────────────────────────┐
│ What Claude sees (in order):                        │
│                                                     │
│ 1. User's original prompt:                          │
│    "fix the auth bug where tokens expire too fast"  │
│                                                     │
│ 2. [additionalContext injected by restruct]:         │
│    Structured plan, relevant rules, constraints,    │
│    workflow directives, anti-patterns               │
│                                                     │
│ Claude weights #2 more heavily (recency bias)       │
│ and follows the structured plan while still         │
│ understanding the user's intent from #1.            │
└─────────────────────────────────────────────────────┘
```

The `additionalContext` field is injected via the hook's JSON output and is **not displayed to the user** in the Claude Code UI (we set `suppressOutput: true`). This keeps the UX clean — the user types casually and gets structured execution without seeing the machinery.

## The Value Proposition (Reframed)

We are NOT "replacing bad prompts with good ones." We are:

1. **Saturating the context with relevant rules** — project constraints that the user knows but didn't type, injected at the point of highest attention (end of context).
2. **Injecting workflow structure** — ensuring Claude investigates before implementing, plans before coding, verifies after changing.
3. **Preventing common failure modes** — explicit anti-patterns and uncertainty protocols that reduce rework cycles.
4. **Bridging intent to execution** — the user says "fix the auth bug," we add "here's what the project rules say about auth, here's the git context showing recent auth changes, here's the investigation-first workflow."

## Depends On

M2 (Core Pipeline) — need a reliable pipeline before tuning prompt quality.

---

## Tasks

### 3.1 — Injection Framing

**What:** Design the framing text that bridges the user's original prompt and our structured additions.

The injection must clearly signal to Claude that:
- The user's message above is the original intent
- The structured content below is supplementary guidance to follow during execution
- The structured content takes precedence for workflow and constraints

**Framing template (to be tested and iterated):**

```
---
The above is the user's request. Below are structured execution guidelines
synthesized from the project's rules, repository context, and best practices.
Follow the plan below to fulfill the user's intent.
---

<structured_context>
  <objective>...</objective>
  <relevant_rules>...</relevant_rules>
  <workflow>...</workflow>
  <constraints>...</constraints>
  <uncertainty>...</uncertainty>
</structured_context>
```

**Key design decisions to test:**
- Does the separator (`---`) help Claude distinguish original prompt from injection?
- Does explicit "follow the plan below" language effectively steer behavior?
- Does XML vs markdown matter for the injected structure? (XML likely better since it's visually distinct from the user's casual text)
- How much framing text is needed? Minimal is better — the structure speaks for itself.

### 3.2 — System Prompt Optimization (for the local LLM)

**What:** Iterate on `prompt/system_prompt.tmpl` — the instructions for the **local LLM** that generates the additionalContext.

The local LLM's job has changed. It is NOT generating a "replacement prompt." It is generating **supplementary steering context** that will be appended after the user's original message. The system prompt must reflect this:

1. **Role reframe:** "You generate structured execution context that will be appended to a developer's casual request. Your output steers an AI coding agent (Claude) toward thorough, rules-aware execution."

2. **Output awareness:** The local LLM must know its output sits AFTER the user's prompt. It should NOT repeat the user's request — Claude already has it. Instead, it should add what's missing: structure, rules, workflow, constraints.

3. **Few-shot examples:** 2-3 concrete examples showing:
   - User prompt (for context)
   - → Injected additionalContext (the output we want)
   - Show that the output complements rather than restates

4. **Rules injection:** When project rules are provided, the local LLM should extract and embed the relevant constraints verbatim. This is the primary value — keeping rules in Claude's active context.

5. **Calibrated uncertainty:** Too many "ask the user" directives make the tool annoying. The LLM should only flag genuine ambiguity — when there are multiple plausible interpretations that would lead to meaningfully different implementations.

6. **Context-aware depth:** The injection should scale with prompt complexity:
   - Simple, clear request → light touch (just rules + basic workflow)
   - Vague or complex request → full structure (objective, rules, workflow, uncertainty, anti-patterns)

### 3.3 — Context Builder Optimization

**What:** Improve `prompt/builder.go`'s construction of the message sent to the local LLM.

**Current approach:** Concatenates raw prompt + rules + git context with markdown headers.

**Improvements:**
- **Prompt first:** The user's raw prompt comes first in the local LLM's input so it understands intent before seeing rules.
- **Rules in full:** Don't over-filter rules at this stage. Give the local LLM all available rules and let it select what's relevant for the injection. (M5 adds LLM-assisted pre-filtering for very large rule sets.)
- **Git context:** Structure git info so the local LLM can reference specific recent changes, active branch, and modified files in the injected context.
- **Token budget:** If approaching the local model's context limit, truncate git context first, then older/less-relevant rules sections. Never truncate the user's prompt.

### 3.4 — Prompt Template as Configurable Asset

**What:** Allow advanced users to customize the system prompt template.

**Requirements:**
- Default template ships embedded in the binary (current behavior via `embed`)
- Users can override by placing a custom template at `~/.config/restruct/system_prompt.tmpl` or `.restruct/system_prompt.tmpl` in the project
- Template uses Go `text/template` syntax with access to variables: `.RawPrompt`, `.Rules`, `.GitContext`, `.HasRules`, `.HasGitContext`
- `restruct config get system-prompt` prints the active template
- Document template customization in the setup skill

### 3.5 — Passthrough Detection

**What:** Detect when injecting additional context would be unhelpful or harmful.

**Passthrough cases:**
- Prompt is a follow-up that references prior conversation ("yes do that", "try option 2", "looks good") — injecting structure here would confuse Claude about what "that" refers to
- Prompt is a command, not a request ("git status", "/help", "exit")
- Very short acknowledgments ("ok", "y", "thanks")

**NOT passthrough (even if simple):**
- Short but actionable requests ("fix the auth bug") — these benefit most from rule injection
- Already-structured prompts — we can still append relevant rules they may have missed

**Implementation:**
- Add a `ShouldRefine(prompt string) bool` function in the pipeline
- Check for follow-up patterns, commands, and acknowledgments
- Log when passthrough is triggered and why

### 3.6 — Refinement Quality Smoke Tests

**What:** A small, curated set of test cases verifying the injection format and framing.

**Test cases (10 minimum):**
| Input | Expected additionalContext Must... |
|-------|-------------------------------------|
| "fix the auth bug" | contain relevant auth rules, investigation workflow, NOT restate "fix the auth bug" |
| "add dark mode" | contain UI-related rules, scope clarification prompt |
| "refactor the database layer" | contain plan-before-implement workflow, relevant DB rules |
| "why is the app slow" | contain investigation workflow, NOT jump to implementation |
| "update the README" | be lightweight (just basic workflow, minimal rules) |
| "" (empty) | passthrough (no injection) |
| "y" | passthrough (follow-up) |
| "try option 2" | passthrough (follow-up) |
| Very long detailed prompt | complement with rules, NOT repeat the user's detailed instructions |
| Prompt with code snippets | preserve code context, add relevant rules |

**Note:** These test the template/heuristics, not the LLM output (that's M9).

---

## Acceptance Criteria

- [ ] Injection framing tested and documented — Claude reliably follows injected structure over casual prompt
- [ ] System prompt reframed for additionalContext injection (not replacement)
- [ ] Few-shot examples demonstrate complement-not-restate pattern
- [ ] Passthrough detection prevents injection on follow-ups and commands
- [ ] Template is configurable for advanced users
- [ ] 10+ smoke tests verify injection format

## Files Modified

- `cli/internal/prompt/system_prompt.tmpl` — v3: renamed XML tags, scaled depth, few-shot examples
- `cli/internal/prompt/builder.go` — 4-arg Build (prompt, rules, git, session), token budget
- `cli/internal/prompt/template.go` — custom template loading
- `cli/internal/prompt/framing.go` — injection framing with `<context>` wrapper
- `cli/internal/pipeline/pipeline.go` — passthrough detection, `appendConstraints()` post-process
- `cli/internal/prompt/versions/v3_considerations.tmpl` — version snapshot
- `docs/reference/PROMPTING-RESEARCH.md` — research findings and citations

## Risk

**High impact.** This milestone determines the core value proposition. The injection framing — how we bridge "user's casual prompt" to "structured execution plan" — is the single most important design decision. If Claude ignores the injection or treats it as conflicting with the user's prompt, the tool is useless. Plan for multiple iteration cycles with real Claude sessions.
