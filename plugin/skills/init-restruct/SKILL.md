---
name: init-restruct
description: Generate a minimal, research-backed CLAUDE.md for this project. Analyzes the codebase for non-obvious commands and conventions, interviews you for team knowledge, and produces a concise file that stays under 30 rules.
---

You are generating a CLAUDE.md file for this project. Your goal is a **minimal,
high-signal file** — not a comprehensive overview. Research shows:

- LLM-generated context files that are too verbose **reduce** agent success rates
  and increase costs 20%+ (Gloaguen et al., 2025)
- Codebase overviews are redundant — agents find files equally fast without them
- Top LLMs degrade with linear decay past ~150 instructions; Claude's system
  prompt already has ~50, so CLAUDE.md should add **fewer than 30 rules**
- Optimized project instructions improved SWE-Bench by +10% (Arize, 2026)
- Anti-patterns ("do NOT") are as valuable as positive instructions

**The test for every line: "Would Claude make a mistake without this?"**
If Claude would figure it out from the code, don't include it.

---

## Phase 0: User Context (if provided)

The user may pass an initial message with this skill invocation: $ARGUMENTS

If $ARGUMENTS is non-empty, read it carefully before starting discovery. It
may contain critical context like:
- What the project is or does
- What's unusual about the setup
- What they care most about
- A migration or refactor in progress

Incorporate this into your discovery — it should steer what you look for
and what you prioritize in the final CLAUDE.md. Do not ask the user to
repeat anything they already said here.

---

## Phase 1: Automated Discovery

Execute these steps using tools. Do not ask the user anything yet.

### 1. Detect project type

Read the root directory for: `package.json`, `go.mod`, `Cargo.toml`,
`pyproject.toml`, `pom.xml`, `build.gradle`, `Makefile`, `xmake.lua`,
`CMakeLists.txt`, `Gemfile`, `.csproj`. Identify language(s), framework(s),
and package manager.

### 2. Find build, test, and lint commands

Search these locations:
- `package.json` scripts
- `Makefile` / `xmake.lua` / `justfile` targets
- CI configs: `.github/workflows/*.yml`, `.gitlab-ci.yml`, `Jenkinsfile`
- `pyproject.toml` [tool.pytest] or [scripts]
- `Cargo.toml` / `build.rs`

**Verify each command works** by running it (use `--dry-run` or `--help` if
destructive). Only keep commands that Claude couldn't guess from the toolchain.
For example: `npm test` is obvious; `pnpm test` calling xmake which calls
`go test` is not.

### 3. Detect code style divergences

Look for: `.eslintrc*`, `.prettierrc*`, `biome.json`, `rustfmt.toml`,
`.editorconfig`, `golangci-lint` config, `.clang-format`. Only note rules
that **override language defaults**. Skip if the config is standard/minimal.

### 4. Find architecture docs

Look for: `ARCHITECTURE.md`, `docs/architecture*`, `ADR/`, `doc/design*`.
If found, note them as `@import` references. Do NOT copy their contents.

### 5. Identify environment requirements

Check: `.env.example`, `docker-compose.yml`, `Dockerfile`, required services
(databases, message queues), and any `REQUIRED_*` or `*_URL` env vars.

### 6. Audit existing CLAUDE.md

If a CLAUDE.md already exists, read it and evaluate each rule against the
research criteria. Flag rules that are:
- Redundant (Claude would do this anyway)
- Too verbose (could be shortened)
- Better as a hook (deterministic, must happen every time)
- Better as a skill (domain knowledge, not needed every session)

Present a summary of what you found and what you'd keep vs. cut.

---

## Phase 2: User Interview

Now ask the user questions to fill gaps that code analysis can't cover.
Use AskUserQuestion for each. Ask **3-5 questions max**, skipping any
already answered by Phase 1.

### Questions to ask:

1. **Anti-patterns:** "Are there patterns, libraries, or approaches that
   have caused problems in this project and should be avoided?"

2. **Workflow conventions:** "Any deployment, branching, PR, or commit
   conventions Claude should follow?"

3. **Undocumented architecture decisions:** "Any architectural constraints
   that aren't in the code but matter? (e.g., 'no ORMs', 'all state in
   Redux', 'never use library X')"

### Socratic gut-check

If a user response sounds over-specified (paragraph-length rules, overly
detailed style guides), challenge once with:

> "Would Claude actually make a mistake without this specific detail, or
> would it figure it out from the code?"

Accept their answer after one challenge. Don't belabor it.

---

## Phase 3: Generate CLAUDE.md

Assemble the file using this template. Omit any section with no content.
**Hard limit: 50 lines maximum.**

```markdown
# {project name}

## Build & Test
- `{build command}` — {what it does, if non-obvious}
- `{test command}`
- `{single test command}` — prefer running single tests over the full suite
- `{lint/typecheck command}`

## Code Style
{only rules that diverge from language defaults — skip if standard}

## Architecture
{2-3 sentences max summarizing key decisions}
{@import references to detailed docs}

## Environment
{required env vars, services, setup steps that aren't in README}

## Workflow
- If you added new behavior, add or update tests to cover it
- {other process steps — e.g., "Prefer focused unit tests", "Add a regression test before fixing a bug"}

## Constraints
- {design/architectural constraints — e.g., "Performance is critical", "All operations must go through the CLI"}

## Do NOT
- {specific anti-pattern from user interview or codebase analysis}
- {another specific anti-pattern}
- {etc — 3-5 items max}
```

**Important:** Do NOT include build/test/lint verification commands (like
"run tests after every change") in any section. These are discovered and
enforced automatically by `.restruct/verify.yaml`.

**Section semantics:**
- **Workflow** — process steps Claude should follow (always shown for code changes)
- **Constraints** — design/architectural rules that apply selectively (shown when relevant)
- **Do NOT** — things to avoid (shown when relevant)

### Rules for the output:
- **No codebase overview or file tree** — agents find files without them
- **No standard practices** Claude already follows (e.g., "write tests",
  "use descriptive names", "handle errors")
- **No file-by-file descriptions** — Claude reads code directly
- **No long explanations** — if it takes more than one line, it's a skill
- **Use `@import` references** for deep documentation instead of inlining
- **Every line must earn its place** — apply the mistake test

---

## Phase 4: Review and Write

Present the draft CLAUDE.md to the user. Ask them to review it.
Only write the file after they approve.

If a CLAUDE.md already exists, show a diff of what changed and why
(citing the research principle behind each removal or addition).
