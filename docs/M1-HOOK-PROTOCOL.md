# M1: Hook Protocol & Claude Code Integration

## Goal

Verify and establish the exact contract between restruct and Claude Code's hook system so that refined prompts actually replace user input in Claude's context window.

## Why This Is First

Nothing else matters if the hook doesn't work. The existing code assumes a `refined_prompt` field in the output JSON, but Claude Code's hook system may expect a different structure. This milestone eliminates that risk. Additionally, the `session_id` and `transcript_path` fields from hooks are critical for the server/dashboard (M4).

---

## Research Findings (Pre-Work Complete)

Claude Code hooks provide these fields on every event:
```json
{
  "session_id": "string",
  "transcript_path": "/path/to/transcript.jsonl",
  "cwd": "/working/directory",
  "permission_mode": "default|plan|auto|...",
  "hook_event_name": "UserPromptSubmit",
  "prompt": "user's raw input"
}
```

**Prompt modification options:**
1. `stdout` JSON with `hookSpecificOutput.additionalContext` — appends context to Claude
2. Plain text on `stdout` (exit 0) — may replace prompt entirely
3. Exit 2 + stderr — blocks prompt processing

**Environment variables:** `CLAUDE_PLUGIN_DATA`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PROJECT_DIR`

**Available hook events:** UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure, SessionStart, SessionEnd, Stop, Notification, and many more.

These findings need **verification via live testing** (Task 1.1).

---

## Tasks

### 1.1 — Verify Hook Contract via Live Testing

**What:** Confirm the researched hook schema by installing a test hook and capturing real data.

**How:**
- Read Claude Code's hook documentation (https://docs.anthropic.com/en/docs/claude-code/hooks)
- Create a test hook that logs raw stdin to a file, install it, and send test prompts
- Document: input fields, output fields, how prompt replacement works, error handling behavior

**Output:** `docs/HOOK-CONTRACT.md` with the verified schema

### 1.2 — Fix hook/protocol.go to Match Reality

**What:** Update `HookInput` and `HookOutput` structs to match the actual Claude Code hook contract discovered in 1.1.

**Current code (hook/protocol.go):**
```go
type HookInput struct {
    HookName  string `json:"hook_name"`
    Prompt    string `json:"prompt"`
    SessionID string `json:"session_id"`
}

type HookOutput struct {
    OK            bool   `json:"ok"`
    RefinedPrompt string `json:"refined_prompt,omitempty"`
    Error         string `json:"error,omitempty"`
}
```

**Action:** Align these structs with the real contract. If Claude Code uses different field names, rename them. If it expects additional fields (like `result`, `message`, `continue`), add them.

### 1.3 — Fix hook/install.go Hook Configuration

**What:** Ensure the hook entry written to `.claude/settings.json` uses the correct hook event name, matcher pattern, and command format.

**Current code writes:**
```json
{
  "hooks": {
    "UserPrompt": [{
      "matcher": ".*",
      "hooks": [{
        "type": "command",
        "command": "restruct refine"
      }]
    }]
  }
}
```

**Verify:**
- Is `UserPrompt` the correct event? Or is it `UserPromptSubmit`? Or `PreToolUse`?
- Does the command receive stdin correctly?
- Does the matcher pattern work as expected?
- Does the command need to be an absolute path?

### 1.4 — End-to-End Smoke Test

**What:** Install the hook, send a real prompt through Claude Code, and verify the refined prompt reaches Claude.

**Test procedure:**
1. Build restruct binary
2. Run `restruct install`
3. Open Claude Code in a test project with an `agents.md`
4. Send a casual prompt ("fix the login bug")
5. Verify via logs/output that:
   - The hook fires
   - Ollama receives the request
   - The refined prompt replaces the original
   - Claude responds based on the refined prompt (not the original)

**Fallback:** If Ollama isn't available during testing, add a `--dry-run` mode that returns a hardcoded refined prompt to isolate hook integration from LLM availability.

### 1.5 — Per-Project `.restruct/` Directory

**What:** Establish a per-project directory for runtime state that enables multi-instance tracking.

**Problem:** Multiple Claude Code instances may run concurrently — same project or different projects. The global SQLite DB stores all data, but each project needs a local anchor for fast session lookup and project-scoped caching.

**Directory structure:**
```
<project-root>/
└── .restruct/
    ├── sessions/
    │   └── <session_id>.json    # Active session state
    ├── cache/                   # Project-scoped prompt cache (fast local lookup)
    └── restruct.log             # Per-project debug log
```

**Session file (`.restruct/sessions/<session_id>.json`):**
```json
{
  "session_id": "abc-123",
  "project_path": "/Users/dev/my-project",
  "started_at": "2026-03-26T10:00:00Z",
  "transcript_path": "/path/to/transcript.jsonl",
  "pid": 12345,
  "last_refinement_at": "2026-03-26T10:05:00Z",
  "refinement_count": 3
}
```

**Lifecycle:**
- Created on first `UserPromptSubmit` hook for a new session_id
- Updated on each subsequent refinement
- Cleaned up on `SessionEnd` hook (or stale-checked on startup: if session file is >24h old and PID is dead, remove it)

**Gitignore:** The `restruct install` command adds `.restruct/` to `.gitignore` automatically. This directory is fully local/ephemeral runtime state.

**Why per-project, not just global?**
- The `cwd` from hook input identifies the project, but reading the global DB to find "which sessions are active in this project" on every hook invocation adds latency
- The local session file is a fast O(1) lookup: "is this session known? → read JSON"
- Project-scoped cache means the same prompt refines differently per project (different rules files)
- Write-through: all data also goes to global SQLite for the dashboard

### 1.6 — Session Tracking Foundation

**What:** Extract and forward `session_id` and `transcript_path` from hook input so downstream systems (server, dashboard, outcome tracking) can correlate refinements to Claude Code sessions.

**Requirements:**
- `HookInput` struct includes `session_id`, `transcript_path`, and `cwd` fields
- `refine` command creates/updates `.restruct/sessions/<session_id>.json`
- When the server is running (M4), session_id is written through to global SQLite
- When the server is NOT running, local session file still captures state (synced on next server start)
- Validate that `transcript_path` points to a readable JSONL file
- Parse transcript to extract last 2-3 conversation turns (for conversation-aware refinement in M5)

**Transcript format (JSONL):**
```json
{"role": "user", "content": "..."}
{"role": "assistant", "content": "..."}
```

### 1.6 — Graceful Degradation on Hook Failure

**What:** When the hook script fails (Ollama down, timeout, crash), Claude Code must still work — the raw prompt passes through.

**Requirements:**
- Non-zero exit code = Claude Code uses original prompt (verify this is how hooks work)
- If hooks support a "passthrough" output, use that instead
- Log errors to stderr (Claude Code shows these as warnings)
- Never block the user's workflow

---

## Acceptance Criteria

- [ ] Hook contract is documented with verified JSON schemas
- [ ] `protocol.go` matches the real contract
- [ ] `install.go` writes correct hook configuration
- [ ] End-to-end test passes: casual prompt in → structured prompt reaches Claude
- [ ] Hook failure degrades gracefully (raw prompt passes through)
- [ ] session_id and transcript_path extracted and available to pipeline
- [ ] Transcript parsing works for recent conversation turns

## Files Modified

- `cli/internal/hook/protocol.go` — struct alignment
- `cli/internal/hook/install.go` — config format fixes
- `cli/cmd/refine.go` — stdin/stdout handling adjustments
- `docs/HOOK-CONTRACT.md` — new documentation
- `plugin/hooks/hooks.json` — plugin hook config alignment

## Risk

**High risk if deferred.** Everything downstream assumes the hook works. If the contract is wrong, all other milestones build on a broken foundation.
