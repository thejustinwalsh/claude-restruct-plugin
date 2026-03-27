# M5: Rules Engine & Context Gathering

## Goal

Build a robust rules loading system that intelligently extracts and prioritizes project constraints, and enrich the refinement pipeline with smarter context gathering.

## Depends On

M2 (Core Pipeline) — needs stable pipeline to integrate into.

---

## Tasks

### 5.1 — Hierarchical Rules Loading

**What:** Support rules from multiple levels with clear precedence.

**Current behavior:** Loads from a flat list of files (`agents.md`, `CLAUDE.md`, `.claude/rules.md`), concatenates everything.

**Required behavior:**
- **Project-level rules:** `agents.md`, `CLAUDE.md`, `.claude/rules.md` in the project root
- **Directory-level rules:** `.claude/rules.md` or `AGENTS.md` in subdirectories (applied when the user's prompt references files in that directory)
- **Global rules:** `~/.config/restruct/rules.md` (user's personal defaults across all projects)
- **Precedence:** Project > Directory > Global (more specific wins on conflict)
- **Deduplication:** If the same rule appears at multiple levels, include it once

**Implementation:**
- `rules/loader.go` gains a `LoadHierarchical(promptMentionedPaths []string) ([]RuleSet, error)` method
- Each `RuleSet` has: source file, priority level, content
- The prompt builder receives structured rules, not a raw concatenation

### 5.2 — Rules Relevance Filtering

**What:** When rules files are large, extract only the sections relevant to the user's request.

**Approach (LLM-assisted):**
1. Parse rules files into sections (by markdown headers)
2. If total rules content < 500 lines, skip filtering and include everything
3. For large rules files: send the section headers + user prompt to the local LLM as a lightweight pre-pass: "Given this user request, which of these rule sections are relevant? Return the section numbers."
4. Include: all relevant sections + always-include sections (marked with `<!-- always -->` or `## [ALWAYS]`)

**Why use the LLM instead of keyword matching?** The local LLM is free and fast enough. Keyword matching produces brittle results — it misses semantic relevance (e.g., a rule about "error handling" is relevant to a prompt about "fix the crash" but shares no keywords). A 1-2 second LLM call to select the right rules is a better investment than shipping a half-relevant refinement that causes Claude to ask clarifying questions. Reduced rework is the goal, not minimal latency.

**Fallback:** If the pre-pass LLM call fails, include all rules (same as <500 line case).

### 5.3 — Smart Git Context

**What:** Improve git context gathering beyond basic branch/log/diff.

**Current behavior:** Branch name, last 5 commit messages, diff stat from HEAD~1.

**Improvements:**
- **Changed files list:** Include `git diff --name-only HEAD` (unstaged) and `git diff --name-only --cached` (staged) — tells the LLM what the user is actively working on
- **File-prompt correlation:** If the user mentions a file name, include that file's recent git history (`git log --oneline -5 -- <file>`)
- **Branch context:** If on a feature branch, include the branch name parsed for intent (e.g., `feature/auth-improvements` → "auth improvements feature")
- **Stash awareness:** Note if there are stashed changes (might be relevant context)
- **Budget:** Total git context should not exceed 50 lines. Truncate least-relevant parts first.

### 5.4 — Conversation-Aware Refinement

**What:** When the user's prompt is a follow-up in a conversation, include enough context for the local LLM to produce a good refinement.

**Challenge:** The hook receives only the current prompt, not conversation history.

**Approach:**
- Claude Code hooks may include session context — investigate in M1 what data is available
- If session context is available: include last 2-3 turns as context for the refinement LLM
- If not available: detect follow-up patterns ("yes", "do that", "try option 2", "actually...") and:
  - For very short follow-ups: passthrough (don't refine)
  - For medium follow-ups with new information: refine with a note that this continues a conversation
- **Session memory (lightweight):** Cache the last refined prompt per session. When a new prompt arrives, include the previous refined prompt as context. This helps the LLM understand what "that" or "it" refers to.

### 5.5 — File Mention Detection

**What:** When the user mentions specific files, extract file-level context to improve refinement.

**Detection:**
- Match patterns like `src/auth/token.go`, `./config.yaml`, `the auth module`
- Check if mentioned files exist in the repo

**Context extraction for mentioned files:**
- File's first 10 lines (usually imports/header comments showing purpose)
- File's directory-level rules (from 5.1)
- File's recent git history (2-3 commits)

**Budget:** Max 30 lines of file context per mentioned file, max 3 files.

### 5.6 — Rules Loader Tests

**What:** Unit tests for the rules engine.

**Test cases:**
- Single rules file exists → loaded correctly
- Multiple rules files → concatenated with source headers
- No rules files → empty rules, no error
- Large rules file → filtered to relevant sections
- Hierarchical loading → precedence is correct
- Rules hash → changes when any file changes
- Invalid file paths → skipped gracefully

---

## Acceptance Criteria

- [ ] Hierarchical rules loading with project > directory > global precedence
- [ ] LLM-assisted relevance filtering for large rules files (with fallback to include-all)
- [ ] Git context includes changed files and file-prompt correlation
- [ ] Follow-up prompts detected and handled (passthrough or context-aware)
- [ ] File mentions extract targeted context
- [ ] Rules loader test coverage >80%

## Files Modified

- `cli/internal/rules/loader.go` — hierarchical loading, relevance filtering
- `cli/internal/git/context.go` — enhanced git context
- `cli/internal/pipeline/pipeline.go` — session memory, follow-up detection
- `cli/internal/prompt/builder.go` — structured rules in prompt
- New: `cli/internal/rules/filter.go` — LLM-assisted section filtering
- New: `cli/internal/context/file_context.go` — file mention detection and context

## Risk

**Medium.** Individually each task is straightforward. The risk is in the LLM pre-pass for rules filtering — it needs to be reliable and its prompt well-tested. If it produces bad selections, the refinement quality degrades. Mitigate with the fallback (include everything on failure) and test with the evaluation corpus from M9.
