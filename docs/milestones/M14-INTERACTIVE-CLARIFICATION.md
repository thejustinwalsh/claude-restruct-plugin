# M14: Interactive Clarification During Prompt Refinement

## Goal

When the local LLM detects ambiguity in a user's prompt, surface clarification questions to the user during hook execution — before Claude sees anything. The user answers inline via Claude Code's Prompt Request Protocol. Answers are folded into the additionalContext so Claude receives fully-resolved instructions on the first turn, eliminating a round-trip.

## Depends On

M1 (Hook Protocol), M2 (Core Pipeline), M3 (Prompt Engine — clarification field exists)

---

## Problem Statement

The current pipeline generates `clarification` questions that are passed to Claude as `<clarification_needed>` XML. Claude then asks the user, the user answers, and Claude proceeds. This wastes:
- One Claude API round-trip (ask + answer)
- User time (waiting for Claude to formulate the question)
- Context window space (Claude's question + user's answer consume tokens)

The Prompt Request Protocol in Claude Code hooks allows hooks to request user input during execution via a JSON protocol on stdout/stdin. This eliminates the round-trip.

## Solution

```
User prompt → restruct refine hook
  1. Parse stdin (HookInput)
  2. Pipeline.Refine() → LLM returns JSON with clarification[]
  3. IF clarification[] is non-empty:
     a. For each question, write PromptRequest JSON to stdout
     b. Read PromptResponse JSON from stdin
     c. Collect answers
  4. Fold answers into additionalContext as <resolved_clarifications>
  5. Write final HookOutput to stdout (exit 0)
```

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 14.1 — Prompt Request Protocol types | 2h | Add `PromptRequest`, `PromptOption`, `PromptResponse` types to `hook/protocol.go`. Add `WritePromptRequest()` and `ReadPromptResponse()` functions using buffered I/O (stdin stays open for the prompt-response loop) |
| 14.2 — Clarification resolver | 2h | New `pipeline/clarify.go`: `ClarificationResult` struct with `Resolved` and `Skipped` slices. `composeClarificationContext()` produces `<resolved_clarifications>` XML section |
| 14.3 — Interactive I/O coordinator | 3h | New `hook/interactive.go`: `Prompter` struct with `AskClarifications(questions)` method. For each question: generates option keys from LLM-provided options, writes PromptRequest, reads PromptResponse. Every question gets a "Let Claude decide" escape option |
| 14.4 — Modify `refine.go` for interactive mode | 3h | Create `bufio.Reader` wrapping stdin. Use `ParseInputBuffered()` for initial read. After refinement, if clarifications exist: create Prompter, ask questions, compose resolved context, append to refined output. Final HookOutput is the last JSON line on stdout (prompt request lines are stripped by Claude Code) |
| 14.5 — LLM generates answerable questions | 2h | New prompt version (v5): `clarification` field becomes `[{question, options}]` instead of `[]string`. LLM instructed to generate 1-3 questions max with 2-4 discrete answer options each. Always include "Not sure" option. Backward compatible parser handles old `[]string` format |
| 14.6 — Compose with resolved clarifications | 2h | Modify `compose.go` to accept optional `*ClarificationResult`. Resolved answers become `<resolved_clarifications>` section. Skipped questions remain in `<clarification_needed>`. All-answered = no `<clarification_needed>` |
| 14.7 — Configuration and feature gate | 1h | Add `interactive_clarification: true` and `max_clarifications: 3` to config. When disabled, existing passthrough behavior preserved |
| 14.8 — DB recording | 1.5h | Migration for `clarifications` table (refinement_id, question, options, selected, skipped). Record all interactions. API endpoint extension for refinement detail |
| 14.9 — Tests | 3h | Protocol round-trip, buffered ParseInput, Prompter with mock stdin/stdout, compose with resolved/skipped/mixed, parse old+new clarification format, feature gate off, end-to-end integration |

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Question format | Multiple-choice only | Prompt Request Protocol uses discrete options, not free-text. Questions needing free-text get "Let Claude decide" and pass through |
| Max questions | 3 | Avoid survey fatigue. Most prompts have 0-1 genuine ambiguities |
| Answer folding | Direct compose (no re-LLM) | Answers compose into XML directly. No second LLM call needed — stays within timeout budget |
| Cache behavior | Never cache prompts with clarifications | Answers vary per invocation. Cache only the final resolved output as a future optimization |
| Stdin handling | Buffered reader, kept open | Initial HookInput consumed, then same reader used for prompt-response loop. Final HookOutput is last stdout line |
| Backward compatibility | Parse both `[]string` and `[{question, options}]` | Old LLM output still works. New format enables better interactive UX |
| Feature gate | Default on | The feature is transparent — if no clarifications are generated, no prompts shown. Users who dislike it can turn it off |

## Acceptance Criteria

- LLM-generated clarification questions are surfaced to the user during hook execution
- User can answer (selecting an option) or skip ("Let Claude decide") each question
- Answered questions appear in `<resolved_clarifications>` section of additionalContext
- Skipped questions remain in `<clarification_needed>` for Claude to ask
- All-answered prompts have no `<clarification_needed>` section
- Feature gate disables the interactive flow, preserving existing behavior
- Dashboard shows clarification interactions per refinement
- No regression in passthrough detection or non-ambiguous prompts

## Risk

| Risk | Mitigation |
|------|-----------|
| Prompt Request Protocol is undocumented/unstable | Observed in Claude Code source. Degrades to passthrough if protocol changes |
| LLM generates bad options | Cap at 3 questions, require "Not sure". Users can always skip |
| Multiple-choice too restrictive | By design — free-text questions pass through to Claude |
| Timeout pressure | 10-minute default for command hooks. Even with 3 questions at 30s each, generous budget |

## Files

**New:**
- `cli/internal/pipeline/clarify.go`
- `cli/internal/hook/interactive.go`
- `cli/internal/prompt/versions/v5_clarify.tmpl`

**Modified:**
- `cli/internal/hook/protocol.go` — PromptRequest/PromptResponse types
- `cli/internal/pipeline/pipeline.go` — LLMClassification struct update
- `cli/internal/pipeline/compose.go` — ClarificationResult parameter
- `cli/cmd/refine.go` — interactive I/O loop
- `cli/internal/config/config.go` — feature gate config
- `cli/internal/db/` — clarifications table migration
