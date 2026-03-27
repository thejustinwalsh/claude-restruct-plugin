# Claude Code Hook Contract

Verified reference for the hook I/O protocol used by restruct.

## UserPromptSubmit

Fires **before** Claude processes the user's prompt. Cannot replace the prompt — can only **append additional context**.

### Input (stdin)

```json
{
  "session_id": "unique-session-id",
  "transcript_path": "/path/to/transcript.jsonl",
  "cwd": "/working/directory",
  "permission_mode": "default",
  "hook_event_name": "UserPromptSubmit",
  "prompt": "user's raw input text"
}
```

### Output (stdout)

**Passthrough (no modification):** Exit 0, write nothing to stdout.

**Inject additional context:** Exit 0, write JSON:
```json
{
  "hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": "structured instructions appended to Claude's context",
    "suppressOutput": true
  }
}
```

**Block the prompt:** Exit 2, write reason to stderr.

### Key Behaviors

| Exit Code | Behavior |
|-----------|----------|
| 0 | Success. stdout parsed for JSON. Non-JSON text also injected as context. |
| 2 | Block. Prompt rejected. stderr shown to user. |
| Other | Non-blocking error. stderr logged in verbose mode only. Prompt proceeds. |

### Matcher

UserPromptSubmit does **not** support matchers. The `matcher` field must be an empty string. The hook fires on every user prompt.

### How additionalContext Works (Critical Design Constraint)

The original user prompt **always** reaches Claude first. `additionalContext` is **appended after it** in Claude's context window. This is actually ideal for restruct's purposes:

1. **Recency bias works in our favor.** LLMs weight later context more heavily. Our structured injection — placed after the casual prompt — naturally takes priority for execution planning.
2. **`suppressOutput: true`** hides the injection from the user's Claude Code UI. The user types casually and gets structured execution without seeing the machinery.
3. **The injection complements, never restates.** Since Claude already has the user's prompt, the additionalContext should add what's missing: relevant project rules, workflow structure, constraints, and anti-patterns — not restate "the user wants to fix the auth bug."
4. **Rule saturation is the primary value.** The user knows their project rules but doesn't type them. We inject them at the point of highest attention in Claude's context window.

## SessionStart / SessionEnd

Fire when a Claude Code session begins/ends.

### Input (stdin)

Same base fields as UserPromptSubmit, minus `prompt`.

### Output

Write nothing to stdout. These hooks are fire-and-forget for session tracking.

## Environment Variables

Available in all hooks:
- `CLAUDE_PLUGIN_DATA` — persistent data directory for the plugin
- `CLAUDE_PLUGIN_ROOT` — plugin installation directory
- `CLAUDE_PROJECT_DIR` — project root path

## settings.json Format

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/restruct refine"
          }
        ]
      }
    ]
  }
}
```

Only `type: "command"` is supported for UserPromptSubmit.

## Transcript Format

The file at `transcript_path` is JSONL (one JSON object per line):
```json
{"role": "user", "content": "previous message"}
{"role": "assistant", "content": "previous response"}
```

The current prompt is NOT yet in the transcript when the hook fires.
