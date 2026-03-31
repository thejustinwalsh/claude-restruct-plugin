# M18: Verification Failure Behavioral Guidance

## Goal

When any verification tool fails after a Bash call — tests, linter, typecheck, vet — inject behavioral guidance that steers Claude toward fixing the root cause rather than suppressing the error. Claude commonly: adds `// nolint` comments to suppress lint errors, uses `as` type assertions to silence TypeScript errors, modifies tests to match broken behavior, or uses `@ts-ignore`. This milestone injects a directive at the moment of failure that blocks these shortcuts and points Claude at relevant project context.

## Depends On

M1 (Hook Protocol), M11 (Project Bootstrap — for deep-context links)

## Priority

Medium. Simple implementation, high behavioral impact.

---

## Problem Statement

When Claude runs tests and they fail, it frequently takes the path of least resistance: modify the test to match the new behavior rather than fix the code that broke the test. This is especially problematic when:
- The test was correct and the code change introduced a regression
- The test captures intentional business logic that shouldn't change
- Claude doesn't understand the system well enough to know whether the test or the code is wrong

The fix isn't better test output parsing — Claude reads test output fine. The fix is a behavioral directive injected at the moment of failure that reminds Claude of the correct approach and provides links to relevant project context.

## Solution

### PostToolUse Hook on Bash

```json
"PostToolUse": [{
  "matcher": "Bash",
  "hooks": [{
    "type": "command",
    "command": "${CLAUDE_PLUGIN_ROOT}/bin/restruct test-guidance",
    "timeout": 2
  }]
}]
```

### Detection

The hook receives the Bash tool's output (stdout/stderr) in the PostToolUse input. Detection is simple:
- Check the command string for test-related patterns (configurable)
- Check the exit code (non-zero = failure)
- Only inject guidance on failure — passing tests get no injection

### Injected Guidance

When a test command fails, inject as `additionalContext`:

```xml
<verification_failure_guidance>
A verification check failed. Before attempting to suppress or work around the error:

FOR TEST FAILURES:
1. Assume the test is correct until proven otherwise
2. Review the test to understand what behavior it verifies
3. If your code change broke the test, fix your code — not the test
4. Only modify a test if you can explain WHY the test's expectation is wrong
5. If unsure, ask the user whether the test or the code should change

FOR LINT/VET ERRORS:
1. Do NOT add suppression comments (// nolint, // eslint-disable, @ts-ignore)
2. Fix the underlying code issue that triggered the lint error
3. If the lint rule is genuinely wrong for this case, ask the user before suppressing

FOR TYPE ERRORS:
1. Do NOT use type assertions (as) or casts to silence type errors
2. Fix the actual type mismatch — update interfaces, function signatures, or generics
3. If the types are genuinely incompatible, redesign the approach

GENERAL PRINCIPLE: Fix the root cause. Suppression is not a fix.

Relevant project context:
- [cli/CLAUDE.md] Go testing patterns, go vet compliance
- [CLAUDE.md] "Do not use `as` in TypeScript to fix type errors"
- [web/CLAUDE.md] Component patterns, type safety rules
</verification_failure_guidance>
```

### Relevant Context Selection

The hook uses the test command and failing file paths (if detectable from output) to select relevant deep-context docs from M11's `.restruct/links/`:
- Test command contains `go test` → load CLI rules doc
- Test command contains `pnpm test` or `jest` → load web rules doc
- Test command path contains a specific directory → load that directory's rules

If M11 isn't available, the guidance is still injected — just without the deep-context links.

### Configurable Test Patterns

In `.restruct/config.yaml`:

```yaml
verification_guidance:
  # Commands that are verification tools (matched as prefixes)
  # Category determines which behavioral directive is injected
  commands:
    # Test runners
    - match: go test
      category: test
    - match: pnpm test
      category: test
    - match: npm test
      category: test
    - match: jest
      category: test
    - match: pytest
      category: test
    - match: cargo test
      category: test

    # Linters / vet
    - match: go vet
      category: lint
    - match: eslint
      category: lint
    - match: golangci-lint
      category: lint
    - match: pnpm lint
      category: lint

    # Type checkers
    - match: tsc
      category: typecheck
    - match: pnpm exec tsc
      category: typecheck
    - match: mypy
      category: typecheck

  # Additional behavioral directives per category
  extra_directives:
    test: []
    lint: []
    typecheck: []

  # Disable entirely
  enabled: true
```

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 18.1 — Test command detector | 1.5h | New `internal/guidance/detect.go`: given a Bash command string and exit code, determine if this was a failed test run. Match against configurable test command prefixes. Return false for passing tests (exit 0) and non-test commands |
| 18.2 — Context linker | 2h | New `internal/guidance/linker.go`: given a test command, map to relevant deep-context docs from `.restruct/links/index.json`. Use command prefix → directory rules mapping (go test → cli/, jest → web/). Extract relevant rule snippets (testing-related rules only). Graceful no-op when M11 index doesn't exist |
| 18.3 — Guidance composer | 1.5h | New `internal/guidance/compose.go`: `composeTestGuidance(command, links)` produces the `<test_failure_guidance>` XML. Includes behavioral directives + relevant doc links. Loads extra directives from config |
| 18.4 — Test guidance CLI command | 1.5h | New `cmd/test_guidance.go`: PostToolUse hook handler. Reads hook input (tool_name=Bash, tool_input.command, exit_code from tool output). Runs detection → linking → composition. Returns additionalContext on failure, exits cleanly on pass/non-test |
| 18.5 — Plugin.json wiring | 0.5h | Add PostToolUse hook for Bash with 2s timeout |
| 18.6 — Config integration | 0.5h | Add `test_guidance` section to config with test command patterns and enable flag |
| 18.7 — Tests | 2h | Detection: various test commands (pass/fail), non-test commands. Linking: command → doc mapping with and without M11 index. Compose: verify XML output with and without links. Integration: pipe PostToolUse input through handler |

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Trigger | Failed test commands only | Passing tests need no intervention. Non-test Bash commands need no intervention |
| Detection | Command prefix + non-zero exit | Simple, fast, no output parsing needed. Exit code is reliable signal |
| Guidance style | Behavioral directive, not output analysis | Claude reads test output fine. The problem is behavioral — it takes shortcuts. The fix is a directive |
| Deep-context links | Optional enhancement from M11 | Guidance works standalone. Links make it more targeted |
| No LLM call | Deterministic | <50ms. No latency added to the test cycle |
| Configurable patterns | User-editable test commands | Different projects use different test runners. User adds their own |

## Acceptance Criteria

- Failed test commands trigger guidance injection
- Passing tests and non-test commands produce no output
- Guidance includes "fix the code, not the test" behavioral directive
- Relevant deep-context links included when M11 index available
- Test command patterns are configurable
- Extra directives can be added via config
- Feature can be disabled
- No measurable latency impact on non-test Bash commands

## Risk

**Very low.** Simple prefix matching + static guidance text. No LLM, no complex parsing. Worst case: a non-test command matches a test pattern and gets unnecessary guidance (harmless — guidance only appears on failure).

## Files

**New:**
- `cli/internal/guidance/detect.go`
- `cli/internal/guidance/linker.go`
- `cli/internal/guidance/compose.go`
- `cli/cmd/test_guidance.go`

**Modified:**
- `plugin/.claude-plugin/plugin.json` — add PostToolUse Bash hook
- `cli/internal/config/config.go` — test_guidance config section
