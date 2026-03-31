# M16: Subagent Rule Alignment

## Goal

Ensure subagents (Agent tool, teammates, background tasks) follow project conventions by injecting a tailored rule brief at SubagentStart. Use the agent's type and task context to inform rule selection — an Explore agent gets different rules than an implementation agent. Extend verification to subagent completion via SubagentStop.

## Depends On

M1 (Hook Protocol), M11 (Project Bootstrap — .restruct/links/), M8.2 (Verification — for SubagentStop verification)

---

## Problem Statement

When Claude spawns subagents, they receive a fresh context with no project rules. The parent conversation may have strong rule adherence thanks to our UserPromptSubmit refinement, but subagents operate blind. This is especially problematic for implementation-focused subagents (Agent tool with "implement the auth fix") that write code without knowing the project's conventions.

## Solution

### SubagentStart Hook

Inject a tailored rule brief based on the agent's context:

```json
"SubagentStart": [{
  "hooks": [{
    "type": "command",
    "command": "${CLAUDE_PLUGIN_ROOT}/bin/restruct subagent-context",
    "timeout": 3
  }]
}]
```

### Available Context for Rule Selection

The SubagentStart hook input provides:

```json
{
  "hook_event_name": "SubagentStart",
  "session_id": "...",
  "agent_type": "general-purpose | Explore | Plan | ...",
  "agent_id": "a1b2c3d4e",
  "cwd": "/project/path",
  "transcript_path": "..."
}
```

Additionally, by querying the task system (via the session's active tasks), we can access:
- `task_subject` — the description of what the agent was asked to do (e.g., "Implement auth token refresh")
- `task_type` — local_agent, in_process_teammate, etc.

### Agent-Type-Aware Rule Profiles

| Agent Type | Rule Focus | Skip |
|-----------|-----------|------|
| `Explore` | Architecture context, file locations, project structure | Write anti-patterns, workflow steps |
| `Plan` | Workflow rules, constraints, architecture decisions | Code style details |
| `general-purpose` | Full rule brief — constraints, anti-patterns, workflow | Nothing (default profile) |
| Custom agent definitions | Match by agent name/description keywords against rule keywords from M11 | — |

### Rule Selection Pipeline

```
SubagentStart hook fires
    │
    ├── Load hook input (agent_type, agent_id, cwd)
    │
    ├── Load project map from .restruct/links/index.json
    │
    ├── Load session rule stats (if M15 available)
    │   → Bias toward rules that have been relevant this session
    │
    ├── Apply agent-type profile filter
    │   → Explore: context + architecture rules only
    │   → Plan: workflow + constraints
    │   → general-purpose: everything
    │
    ├── If task_subject available (query active tasks by agent_id):
    │   → Run lightweight keyword match against rule index
    │   → Boost rules whose keywords overlap with task subject
    │
    ├── Budget: top 8 rules, ~1KB text
    │   → Subagent context is smaller than main session
    │   → Process guardrails always included
    │
    └── Return additionalContext: <subagent_rules> XML block
```

### SubagentStop Verification

Extend the existing M8.2 verification system to subagent completion:

```json
"SubagentStop": [{
  "hooks": [{
    "type": "command",
    "command": "${CLAUDE_PLUGIN_ROOT}/bin/restruct verify --subagent",
    "timeout": 120
  }]
}]
```

When a subagent completes:
- Diff files changed since the SubagentStart snapshot
- Run matching verification checks (same system as TaskCompleted)
- Exit code 2 forces the subagent to continue and fix issues
- This catches convention violations before the subagent's output reaches the parent

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 16.1 — Agent type profiles | 2h | New `internal/subagent/profiles.go`: define rule selection profiles per agent type. Map agent_type string to profile (which rule categories to include/exclude). Extensible — custom profiles can be added to `.restruct/config.yaml` |
| 16.2 — Task context lookup | 1.5h | New `internal/subagent/context.go`: given an agent_id, look up the associated task's subject from the session's recent refinements in DB. Extract keywords from the task subject for rule matching |
| 16.3 — Subagent rule selector | 3h | New `internal/subagent/selector.go`: combines project map, session rule stats (if available), agent profile, and task context keywords to score and select rules. Budget: top 8 rules or ~1KB. Process guardrails always included |
| 16.4 — Subagent context composer | 2h | New `internal/subagent/compose.go`: `composeSubagentContext(rules, agentType, taskSubject)` produces `<subagent_rules>` XML. Includes agent-type-specific preamble ("You are operating as an Explore agent — focus on reading and understanding, not modifying") |
| 16.5 — SubagentStart CLI command | 2h | New `cmd/subagent.go`: hook handler for SubagentStart. Reads hook input, runs selection pipeline, returns additionalContext. Also takes a snapshot for SubagentStop verification |
| 16.6 — SubagentStop verification | 2h | Extend `cmd/verify.go` to handle SubagentStop events. Reuse existing diff + check runner logic. Scope: agent-specific snapshot. On failure: exit 2 with stderr feedback (subagent continues) |
| 16.7 — Plugin.json wiring | 0.5h | Add SubagentStart (`restruct subagent-context`, 3s) and SubagentStop (`restruct verify`, 120s) hooks to plugin.json |
| 16.8 — Session stats integration | 1h | If M15's session rule stats tracker is available, use it to bias subagent rule selection toward rules that have been effective this session. Graceful no-op if M15 isn't implemented yet |
| 16.9 — Tests | 3h | Profile selection for each agent type. Task context keyword extraction. Rule scoring with and without session stats. Compose output for different profiles. SubagentStop verification flow. Integration: spawn mock subagent, verify context injection + verification |

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Rule budget | 8 rules / ~1KB | Subagents have smaller context windows (especially Explore/Plan). Must be more concise than main session reinject |
| Agent type profiles | Hardcoded defaults + config overrides | Most agent types are predictable. Custom agents from plugins may need custom profiles |
| Task subject lookup | DB query for recent refinements | The task_subject isn't in the hook input directly, but we can infer from the most recent refinement's raw_prompt or from the task registry |
| SubagentStop verification | Reuse M8.2 verify system | Same diff + check runner, different scope (agent snapshot vs task snapshot). No new verification logic needed |
| Process guardrails | Always included for all agent types | "After Every Change" workflow rules are universal. Even Explore agents should follow the investigation pattern |
| Session stats bias | Optional, graceful degradation | M15 may not be implemented yet. Selection works fine with just profile + keyword matching |

## Acceptance Criteria

- SubagentStart hook fires and injects tailored rules for the agent type
- Explore agents get read-focused rules (no write anti-patterns)
- Plan agents get workflow + constraint rules
- General-purpose agents get the full brief
- Task subject keywords boost relevant rules
- Session rule stats (if available) bias selection toward effective rules
- SubagentStop runs verification checks on files changed during subagent execution
- Exit code 2 on SubagentStop forces subagent to continue and fix
- Budget stays under 1KB per subagent
- Graceful no-op when project map doesn't exist (M11 not run yet)

## Risk

**Low.** SubagentStart/SubagentStop are well-defined hook events. The rule selection pipeline is lightweight (no LLM call — just scoring and filtering). The main risk is the task_subject lookup — if the agent_id doesn't map to a known task, we fall back to profile-only selection.

## Files

**New:**
- `cli/internal/subagent/profiles.go` — agent type rule profiles
- `cli/internal/subagent/context.go` — task context lookup
- `cli/internal/subagent/selector.go` — rule scoring and selection
- `cli/internal/subagent/compose.go` — subagent context composition
- `cli/cmd/subagent.go` — SubagentStart hook handler

**Modified:**
- `cli/cmd/verify.go` — handle SubagentStop events
- `plugin/.claude-plugin/plugin.json` — add SubagentStart + SubagentStop hooks
