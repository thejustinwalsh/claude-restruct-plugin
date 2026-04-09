# Restruct: Feature-Flagged Refinement, Standalone Plugin, Master Setup

**Date:** 2026-04-09
**Status:** Approved (pending implementation)

## Goal

Prepare the restruct plugin to be installed standalone from a Claude Code marketplace with prompt refinement feature-flagged off by default, while keeping every other feature (verification, tool permissions, rule bootstrapping, CLAUDE.md generation) fully operational. Add a master setup skill that orchestrates per-feature setup skills, and ship two READMEs — one for monorepo contributors and one for plugin users.

## Motivation

Prompt refinement is not ready for external rollout, but the rest of the plugin (verify, permit, bootstrap, init) is useful today. We need a distribution-level flag that hides refinement from setup and the CLI enable/disable commands without ripping the code out. When refinement is ready, flipping a single config key should turn the whole feature back on.

## Non-goals

- No changes to the web dashboard.
- No changes to the Go pipeline internals (`internal/pipeline`, `internal/bootstrap`, `internal/ollama`).
- No automated end-to-end tests of the plugin installation flow.
- No decision-history pattern analysis in `setup-permissions` (that is the existing `review-permissions` skill's job).
- No rebuild of the existing runtime sentinel toggle in `internal/toggle` — it stays as-is.

## Architecture

### Two-layer on/off model

| Layer | Source | Purpose | Default |
|---|---|---|---|
| **Release flag** (new) | `features.refinement` in `config.yaml` | "Is refinement available in this release at all?" | `false` |
| **Runtime toggle** (existing) | sentinel file in restruct data dir | "Is refinement active right now?" | enabled (no sentinel) |

The release flag gates the runtime toggle. When `features.refinement == false`:
- `restruct refine` passes through immediately.
- `restruct enable` / `disable` / `status` refuse with a "not yet enabled" message.
- The master `setup` skill skips the refinement step.

When `features.refinement == true`:
- All existing behavior applies (sentinel-controlled runtime toggle).

### Components touched

| File | Change |
|---|---|
| `cli/internal/config/config.go` | Add `FeaturesConfig { Refinement bool }` with `mapstructure:"refinement"`. Register defaults in `LoadFromViper`. Add helper `(*Config).RefinementEnabled()`. |
| `cli/cmd/refine.go` | After `config.LoadFromViper()`, short-circuit with `hook.PassthroughOutput()` when `!cfg.RefinementEnabled()`. Placed before the sentinel check, before DB open, before pipeline init. |
| `cli/cmd/toggle.go` | `enable`, `disable`, `status` check the flag first. If off, print the "not yet enabled" message and exit non-zero. |
| `cli/cmd/doctor.go` | Add `RefinementFeatureEnabled bool` to `DoctorReport`. When false, Ollama/model checks are still reported but don't gate `all_good`. |
| `cli/cmd/model.go` | `model load` exits 0 with a debug log when flag is off. |
| `plugin/skills/setup/SKILL.md` | Rewritten as a master orchestrator — short, calls sub-skills in order, respects the flag. |
| `plugin/skills/setup-rules/SKILL.md` | NEW. Thin wrapper that invokes the existing `init` skill. |
| `plugin/skills/setup-verify/SKILL.md` | NEW. Contains verify.yaml discovery logic from current `setup` Step 8. |
| `plugin/skills/setup-permissions/SKILL.md` | NEW. Scans project, writes `.restruct/permissions.yaml` with sensible defaults, asks user to confirm. |
| `plugin/skills/setup-refine/SKILL.md` | NEW. Contains Ollama install/start/model pull/warmup/`config set`/`doctor`/`enable` flow from current `setup` Steps 1–7. Respects the flag. |
| `plugin/skills/init/SKILL.md` | Unchanged. `setup-rules` delegates to it. |
| `plugin/skills/refine/SKILL.md` | Unchanged. |
| `plugin/skills/enable/SKILL.md`, `plugin/skills/disable/SKILL.md` | Unchanged in body — their CLI targets become flag-aware. |
| `plugin/skills/review-permissions/SKILL.md` | Unchanged. |
| `plugin/.claude-plugin/README.md` | NEW. ~30 lines. Marketplace description. |
| `plugin/README.md` | NEW. ~200 lines. User-facing installation and configuration docs. |
| `README.md` (repo root) | NEW. ~100 lines. Contributor docs for the monorepo. |
| `cli/internal/config/config_test.go` | NEW (or extend). Defaults + feature flag unmarshal tests. |
| `cli/cmd/toggle_test.go` | NEW. Flag-aware enable/disable/status tests. |
| `cli/cmd/refine_test.go` | NEW or extend. Flag-off passthrough test. |
| `cli/internal/toggle/toggle_test.go` | Unchanged. |

Bootstrap (`SessionStart` / `FileChanged` rule indexing) stays enabled when the flag is off. It's cheap and keeps the deep-context map warm for when refinement is later enabled.

### Data flow — refine hook, flag off

```
SessionStart
  → restruct bootstrap          (runs normally, indexes rules)
  → restruct model load         (no-op, gated by flag)

UserPromptSubmit
  → restruct refine
    → parse stdin
    → config.LoadFromViper()
    → if !cfg.RefinementEnabled():
        WriteOutput(passthrough); return   ← NEW short-circuit
    → (sentinel check, pipeline, LLM call, streaming — all skipped)
```

### Data flow — master setup skill

```
/restruct:setup
  → read features.refinement via `restruct config get features.refinement`
  → invoke /restruct:setup-rules        (delegates to init)
  → invoke /restruct:setup-verify        (writes .restruct/verify.yaml)
  → invoke /restruct:setup-permissions   (writes .restruct/permissions.yaml)
  → if features.refinement == true:
       invoke /restruct:setup-refine     (Ollama + model + enable)
     else:
       print "refinement is not yet enabled in this release; skipping"
  → print summary
```

## Skill hierarchy

```
plugin/skills/
├── setup/              master — orchestrates, feature-flag-aware
├── setup-rules/        NEW — wraps init
├── setup-verify/       NEW — verify.yaml discovery
├── setup-permissions/  NEW — permissions.yaml defaults
├── setup-refine/       NEW — Ollama + model + enable
├── init/               kept — setup-rules calls this
├── refine/             kept — manual refine
├── enable/             kept — underlying CLI now flag-aware
├── disable/            kept — underlying CLI now flag-aware
└── review-permissions/ kept
```

Sub-skills are idempotent and directly invokable (`/restruct:setup-refine`, etc.). Users can re-run a single phase without going through the master.

## README structure

### Repo root `README.md` (contributors)

1. Title + one-sentence description
2. Repository layout (cli/, web/, plugin/, docs/)
3. Prerequisites (Go, Node, pnpm, xmake)
4. Build & test commands (lifted from CLAUDE.md)
5. Architecture overview (2–3 sentences, link to `docs/reference/ARCHITECTURE.md`)
6. Release process (how `pnpm build` produces `plugin/bin/*`, what gets committed)
7. Link to `plugin/README.md`

Target length: ~100 lines.

### `plugin/README.md` (end users)

1. What is restruct (one paragraph)
2. Features — table with row per feature, status, description
3. Requirements — Claude Code, supported platforms (`darwin-arm64`, `darwin-x86_64`, `linux-x86_64`), optional Ollama
4. Installation — marketplace add command
5. First-time setup — `/restruct:setup`
6. Per-feature setup — reference to each sub-skill
7. Configuration — `~/.config/restruct/config.yaml`, notable keys, `restruct config set`
8. Enabling prompt refinement (advanced) — flip `features.refinement: true`, run `/restruct:setup-refine`
9. Troubleshooting — `restruct doctor`
10. License / repository link

Target length: ~200 lines.

### `plugin/.claude-plugin/README.md` (marketplace)

Short version. Name, one-paragraph description, feature bullets, install command, link to `plugin/README.md`. ~30 lines.

## Error handling

| Failure | Behavior |
|---|---|
| `config.LoadFromViper()` errors in refine hook | Fall back to `config.Defaults()` (which has `Refinement: false`) → passthrough. |
| `restruct enable` called with flag off | Exit non-zero, message on stderr: `restruct: refinement is not yet enabled in this release — set features.refinement: true in config.yaml to opt in`. |
| `setup-refine` invoked with flag off | Skill detects the flag via `restruct config get`, prints the same message, exits gracefully. |
| Bootstrap fails in `SessionStart` | Existing graceful-degrade path — no change. |
| Sub-skill invoked standalone without project context | Each sub-skill handles its own precondition checks; no coupling to the master. |

## Testing

### Automated

1. **`cli/internal/config/config_test.go`**
   - `TestDefaultsRefinementDisabled` — `Defaults().Features.Refinement == false`.
   - `TestLoadFromViperRespectsFeaturesRefinement` — setting `features.refinement: true` in viper is unmarshaled correctly.

2. **`cli/cmd/refine_test.go`**
   - `TestRefineFeatureFlagOffPassthrough` — feed stdin payload, assert passthrough JSON output, no Ollama HTTP call attempted.

3. **`cli/cmd/toggle_test.go`**
   - `TestEnableRefusesWhenFlagOff` — flag false, `enable` exits non-zero with the "not yet" message.
   - `TestEnableWorksWhenFlagOn` — flag true, `enable` creates sentinel file.
   - `TestStatusShowsFlagStateWhenOff` — `status` mentions feature not yet enabled.

4. **`cli/internal/toggle/toggle_test.go`** — unchanged. The toggle package is lower-level and flag-agnostic.

### Manual verification (milestone gate)

- Install the plugin from a local path into a scratch Claude Code project:
  `/plugin marketplace add /path/to/claude-restruct-plugin/plugin`
- Confirm each feature loads without errors.
- Confirm `restruct refine` passthrough is fast (no Ollama calls).
- Confirm `restruct enable` prints the "not yet enabled" message.
- Confirm `/restruct:setup` runs all four phases, skipping refine.
- Flip `features.refinement: true`, rerun `/restruct:setup-refine`, confirm Ollama flow works end to end.

### Not tested

- End-to-end Claude Code integration tests (too expensive, too flaky).
- Master setup skill orchestration end-to-end (each sub-skill is testable in isolation).
- Web dashboard (untouched).

## Scope boundaries

- **CLI-only code changes:** `cmd/refine.go`, `cmd/toggle.go`, `cmd/doctor.go`, `cmd/model.go`, `internal/config/config.go`.
- **Skills and docs:** new setup sub-skills, rewritten master setup, three new READMEs.
- **Untouched:** web dashboard, `internal/pipeline`, `internal/bootstrap`, `internal/ollama`, `internal/toggle`.

## Open questions

None — resolved during brainstorming.

## Decision log

- **Config key vs Go constant for the flag** — chose config key (`features.refinement`) so testers can flip it without a rebuild.
- **Sentinel toggle remains** — runtime on/off is still valuable once the flag is on. Sentinel is simply bypassed when the flag is off.
- **Bootstrap keeps running when flag is off** — cheap, and keeps the deep-context map warm.
- **`init` skill kept, `setup-rules` wraps it** — preserves existing entry point while unifying naming under `setup-*`.
- **Two READMEs** — monorepo vs plugin, clean separation.
- **Hook stays in plugin.json** — flipping the flag later shouldn't require a plugin.json edit.
