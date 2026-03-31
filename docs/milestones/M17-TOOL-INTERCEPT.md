# M17: Tool Input Interception & Correction

## Goal

Intercept and correct concrete tool mistakes before they execute — wrong package manager, wrong build tool, wrong test runner, wrong shell commands. Uses deterministic mappings from user-editable config and project discovery, not fuzzy heuristics. Low priority but high precision.

## Depends On

M1 (Hook Protocol), M11 (Project Bootstrap — for project discovery data)

## Priority

Low. Implement after M11-M16 are stable.

---

## Problem Statement

Claude frequently uses the wrong tool for a project's conventions. Common examples:
- Running `npm install` in a pnpm workspace
- Using `make` when the project uses xmake
- Running `npx` instead of `pnpm exec`
- Using `yarn add` in an npm project
- Running `pytest` when the project uses `go test`

These are deterministic, correctable mistakes — the project's toolchain is discoverable from lock files, config files, and user settings. A PreToolUse hook can intercept the Bash command and either:
1. **Rewrite** the command to the correct tool (via `updatedInput`)
2. **Warn** Claude with the correct alternative (via `additionalContext`)

## Solution

### PreToolUse Hook on Bash

```json
"PreToolUse": [{
  "matcher": "Bash",
  "hooks": [{
    "type": "command",
    "command": "${CLAUDE_PLUGIN_ROOT}/bin/restruct intercept",
    "timeout": 1
  }]
}]
```

### Correction Mappings

Two sources of truth:

**1. Auto-discovered from project** (at bootstrap time, stored in `.restruct/links/toolchain.json`):

```json
{
  "package_manager": "pnpm",
  "detected_from": "pnpm-lock.yaml",
  "build_tool": "xmake",
  "detected_from": "xmake.lua",
  "test_runner": "go test",
  "detected_from": "go.mod"
}
```

**2. User-editable config** (`.restruct/config.yaml`):

```yaml
intercept:
  # Command corrections: wrong → right
  corrections:
    npm install: pnpm install
    npm run: pnpm run
    npm exec: pnpm exec
    npx: pnpm exec
    yarn add: pnpm add
    yarn install: pnpm install
    make: xmake
    make build: pnpm build
    make test: pnpm test

  # Additional custom corrections
  custom:
    - match: "pip install"
      replace: "uv pip install"
      reason: "Project uses uv for Python package management"

  # Disable specific corrections
  disabled: []
```

### Interception Logic

```
PreToolUse (Bash) fires
    │
    ├── Parse command from tool_input.command
    │
    ├── Check against correction map (longest prefix match)
    │   npm install foo → pnpm install foo
    │   make build → pnpm build (or xmake build)
    │
    ├── If match found:
    │   Option A (rewrite): Return updatedInput with corrected command
    │   Option B (advise): Return additionalContext warning Claude
    │   → Default: rewrite (faster, no round-trip)
    │
    ├── If no match: exit 0, no output (passthrough)
    │
    └── Budget: <50ms (string prefix matching, no I/O)
```

### Rewrite vs Advise

**Rewrite** (`updatedInput`): The corrected command runs directly. Claude sees the result. Fast, no round-trip. But Claude doesn't learn from the correction.

**Advise** (`additionalContext`): Claude receives "Note: this project uses pnpm, not npm. Use `pnpm install` instead." Claude retries with the correct command. Slower (one extra turn) but teaches Claude for subsequent commands.

**Default behavior: Rewrite** for exact matches (npm→pnpm), **Advise** for ambiguous matches where the replacement might change semantics. Configurable per correction via `mode: rewrite | advise`.

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 17.1 — Toolchain discovery | 2h | New `internal/intercept/discover.go`: detect package manager (package-lock.json → npm, yarn.lock → yarn, pnpm-lock.yaml → pnpm, bun.lockb → bun), build tool (Makefile, xmake.lua, CMakeLists.txt), test runner (from go.mod, package.json scripts). Run during M11 bootstrap, store in `.restruct/links/toolchain.json` |
| 17.2 — Correction map loader | 1.5h | New `internal/intercept/corrections.go`: load auto-discovered corrections + user config corrections from `.restruct/config.yaml`. Merge with auto-discovered taking precedence unless user explicitly overrides. Build a prefix-match tree for fast lookup |
| 17.3 — Command matcher | 2h | New `internal/intercept/matcher.go`: given a bash command string, find the longest matching correction prefix. Handle edge cases: commands with flags, quoted arguments, subshells. Conservative — only match clear prefixes, don't try to parse complex pipelines |
| 17.4 — Intercept CLI command | 2h | New `cmd/intercept.go`: PreToolUse hook handler. Reads hook input (tool_name=Bash, tool_input.command). Runs correction match. Returns either `updatedInput` (rewrite) or `additionalContext` (advise) based on correction mode. Exit 0 with no output on no match |
| 17.5 — Plugin.json wiring | 0.5h | Add PreToolUse hook for Bash tool with 1s timeout |
| 17.6 — Dashboard + telemetry | 1h | Record interceptions to DB: command, correction, mode (rewrite/advise), session_id. Dashboard shows correction frequency — helps users see what Claude keeps getting wrong |
| 17.7 — Tests | 2h | Correction matching: exact, prefix, with flags, no match, disabled corrections. Toolchain discovery: each lock file type. Integration: pipe PreToolUse input through intercept, verify updatedInput or additionalContext |

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Match strategy | Longest prefix match | `npm install foo` matches `npm install` → `pnpm install foo`. Preserves arguments |
| Default mode | Rewrite | Faster, no round-trip. Claude sees the correct output immediately |
| Complex commands | Skip (passthrough) | Pipelines, subshells, variable expansion — too risky to rewrite. Only match simple command prefixes |
| Auto-discovery | Lock file based | Deterministic, no false positives. pnpm-lock.yaml = pnpm, period |
| User config | `.restruct/config.yaml` under `intercept:` key | Consistent with team config (M13). Team can share corrections |
| Performance | <50ms, no I/O beyond config cache | Fires on every Bash call. Must be invisible |

## Acceptance Criteria

- `npm install` is rewritten to `pnpm install` when pnpm-lock.yaml exists
- User-defined corrections in config are applied
- Complex commands (pipes, subshells) pass through untouched
- Rewrite mode changes the command; advise mode injects context
- Disabled corrections are respected
- Toolchain auto-detected from lock files at bootstrap
- Dashboard shows correction frequency

## Risk

**Very low.** Deterministic prefix matching on simple commands. Complex commands pass through. Worst case: a correction is wrong and the user disables it in config.

## Files

**New:**
- `cli/internal/intercept/discover.go`
- `cli/internal/intercept/corrections.go`
- `cli/internal/intercept/matcher.go`
- `cli/cmd/intercept.go`

**Modified:**
- `plugin/.claude-plugin/plugin.json` — add PreToolUse Bash hook
- `cli/internal/config/config.go` — intercept config section
