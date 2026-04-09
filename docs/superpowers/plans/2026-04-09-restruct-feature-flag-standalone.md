# Restruct Feature Flag + Standalone Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gate prompt refinement behind a `features.refinement` config key that defaults to false, leaving all other features (verify, permit, bootstrap, init) fully operational; split the setup skill into a master + per-feature sub-skills; ship two READMEs.

**Architecture:** New `FeaturesConfig` struct on `*config.Config` with a `RefinementEnabled()` helper and a `GuardRefinement` function returning `ErrRefinementDisabled`. The refine hook, toggle commands, and `model load` call the guard/helper and short-circuit when the flag is off. Setup flow is rebuilt as a master skill that checks the flag and delegates to four feature-level sub-skills.

**Tech Stack:** Go 1.26 (CLI), spf13/cobra + spf13/viper, Claude Code plugin skills (markdown), xmake + pnpm build system.

**Spec reference:** `docs/superpowers/specs/2026-04-09-restruct-feature-flag-standalone-design.md`

---

## File Structure

### Go (CLI) — edits and additions

| File | Responsibility |
|---|---|
| `cli/internal/config/config.go` | Add `FeaturesConfig` struct, add `Features` field to `Config`, add `Defaults` value, add `RefinementEnabled()` helper method, wire viper default. |
| `cli/internal/config/config_test.go` | NEW. Defaults test, helper test, viper round-trip test. |
| `cli/internal/config/feature.go` | NEW. `ErrRefinementDisabled` sentinel error, `GuardRefinement(cfg *Config) error` helper. |
| `cli/internal/config/feature_test.go` | NEW. Guard tests (flag on, flag off). |
| `cli/cmd/refine.go` | Short-circuit to passthrough when `!cfg.RefinementEnabled()`, placed immediately after `config.LoadFromViper()`. |
| `cli/cmd/toggle.go` | `enable`, `disable`, `status` RunE functions call `config.GuardRefinement(cfg)` at the top. On `ErrRefinementDisabled`: print the error to stderr and return the error (cobra prints non-zero exit). |
| `cli/cmd/doctor.go` | Add `RefinementFeatureEnabled bool` to `DoctorReport`. When false, `AllGood` is `true` (the non-refinement features are always available) unless future checks are added. |
| `cli/cmd/model.go` | `modelLoadCmd` short-circuits: if flag off, print a debug note to stderr and return nil. |

### Plugin skills — edits and additions

| File | Responsibility |
|---|---|
| `plugin/skills/setup/SKILL.md` | REWRITE. Master orchestrator — reads flag, runs sub-skills in order, skips refine when off. |
| `plugin/skills/setup-rules/SKILL.md` | NEW. Thin wrapper that invokes the existing `init` skill. |
| `plugin/skills/setup-verify/SKILL.md` | NEW. verify.yaml discovery (extracted from current setup Step 8). |
| `plugin/skills/setup-permissions/SKILL.md` | NEW. Writes `.restruct/permissions.yaml` with safe defaults. |
| `plugin/skills/setup-refine/SKILL.md` | NEW. Ollama install + model pull + warmup + `restruct enable` (extracted from current setup Steps 1–7). |
| `plugin/skills/init/SKILL.md` | Unchanged. |
| `plugin/skills/enable/SKILL.md`, `disable/SKILL.md` | Unchanged (their underlying CLI is now flag-aware). |
| `plugin/skills/refine/SKILL.md`, `review-permissions/SKILL.md` | Unchanged. |

### Documentation

| File | Responsibility |
|---|---|
| `README.md` (repo root) | NEW. Contributor docs for the monorepo. ~100 lines. |
| `plugin/README.md` | NEW. User-facing install + configure docs. ~200 lines. |

---

## Task 1: Add `FeaturesConfig` struct and `RefinementEnabled()` helper

**Files:**
- Modify: `cli/internal/config/config.go`
- Create: `cli/internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cli/internal/config/config_test.go`:

```go
package config

import "testing"

func TestDefaultsRefinementDisabled(t *testing.T) {
	cfg := Defaults()
	if cfg.Features.Refinement {
		t.Fatal("expected Features.Refinement to default to false")
	}
	if cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() to return false on defaults")
	}
}

func TestRefinementEnabledHelper(t *testing.T) {
	cfg := Defaults()
	cfg.Features.Refinement = true
	if !cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() to return true when flag set")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
pnpm --filter @restruct/cli exec go test ./internal/config/ -run 'TestDefaults|TestRefinement' -v
```

Expected: compile errors — `Features` field and `RefinementEnabled` method do not exist.

- [ ] **Step 3: Add `FeaturesConfig` struct, `Features` field, and helper method**

Edit `cli/internal/config/config.go`:

At the top with the other type declarations, add:

```go
type FeaturesConfig struct {
	Refinement bool `mapstructure:"refinement"`
}
```

Modify the `Config` struct to add `Features` as the first field:

```go
type Config struct {
	Features   FeaturesConfig   `mapstructure:"features"`
	Ollama     OllamaConfig     `mapstructure:"ollama"`
	Refinement RefinementConfig `mapstructure:"refinement"`
	Cache      CacheConfig      `mapstructure:"cache"`
	Rules      RulesConfig      `mapstructure:"rules"`
	Server     ServerConfig     `mapstructure:"server"`
}
```

Modify `Defaults()` to return the new field:

```go
func Defaults() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Features: FeaturesConfig{
			Refinement: false,
		},
		Ollama: OllamaConfig{
			// ...existing content unchanged
```

At the end of the file, add the helper method:

```go
// RefinementEnabled reports whether prompt refinement is enabled in this release.
// When false, the refine hook short-circuits to passthrough and toggle commands refuse.
func (c *Config) RefinementEnabled() bool {
	return c.Features.Refinement
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm --filter @restruct/cli exec go test ./internal/config/ -run 'TestDefaults|TestRefinement' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/config/config.go cli/internal/config/config_test.go
git commit -m "feat(config): add features.refinement flag (default off)"
```

---

## Task 2: Wire viper default for `features.refinement`

**Files:**
- Modify: `cli/internal/config/config.go`
- Modify: `cli/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cli/internal/config/config_test.go`:

```go
import (
	"testing"

	"github.com/spf13/viper"
)

func TestLoadFromViperRespectsFeaturesRefinement(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("features.refinement", true)

	cfg, err := LoadFromViper()
	if err != nil {
		t.Fatalf("LoadFromViper: %v", err)
	}
	if !cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() true when viper has features.refinement=true")
	}
}

func TestLoadFromViperDefaultsToDisabled(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := LoadFromViper()
	if err != nil {
		t.Fatalf("LoadFromViper: %v", err)
	}
	if cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() false by default")
	}
}
```

If the import block at the top of `config_test.go` already exists from Task 1, merge the `github.com/spf13/viper` import into it instead of adding a second import block.

- [ ] **Step 2: Run tests to verify failures**

```bash
pnpm --filter @restruct/cli exec go test ./internal/config/ -run 'TestLoadFromViper' -v
```

Expected: `TestLoadFromViperRespectsFeaturesRefinement` FAILs because `LoadFromViper` does not set a default and does not unmarshal the `features.refinement` key yet (depending on viper defaults behavior — either cfg.Features.Refinement is zero-valued or viper hasn't registered the key).

- [ ] **Step 3: Add the viper default**

In `cli/internal/config/config.go`, inside `LoadFromViper`, add this line alongside the other `viper.SetDefault(...)` calls:

```go
viper.SetDefault("features.refinement", cfg.Features.Refinement)
```

Place it as the first `SetDefault` call in that function, mirroring the `Features` field position.

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm --filter @restruct/cli exec go test ./internal/config/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/config/config.go cli/internal/config/config_test.go
git commit -m "feat(config): wire viper default for features.refinement"
```

---

## Task 3: Add `GuardRefinement` helper + `ErrRefinementDisabled`

**Files:**
- Create: `cli/internal/config/feature.go`
- Create: `cli/internal/config/feature_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cli/internal/config/feature_test.go`:

```go
package config

import (
	"errors"
	"strings"
	"testing"
)

func TestGuardRefinementDisabled(t *testing.T) {
	cfg := Defaults()
	err := GuardRefinement(cfg)
	if err == nil {
		t.Fatal("expected error when refinement disabled")
	}
	if !errors.Is(err, ErrRefinementDisabled) {
		t.Fatalf("expected ErrRefinementDisabled, got %v", err)
	}
	if !strings.Contains(err.Error(), "features.refinement: true") {
		t.Fatalf("expected error message to hint at config key, got %q", err.Error())
	}
}

func TestGuardRefinementEnabled(t *testing.T) {
	cfg := Defaults()
	cfg.Features.Refinement = true
	if err := GuardRefinement(cfg); err != nil {
		t.Fatalf("expected nil when refinement enabled, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
pnpm --filter @restruct/cli exec go test ./internal/config/ -run 'TestGuardRefinement' -v
```

Expected: compile error — `GuardRefinement` and `ErrRefinementDisabled` do not exist.

- [ ] **Step 3: Implement the guard**

Create `cli/internal/config/feature.go`:

```go
package config

import "errors"

// ErrRefinementDisabled is returned when an operation requires prompt refinement
// but the features.refinement flag is off in the active configuration.
var ErrRefinementDisabled = errors.New(
	"restruct: refinement is not yet enabled in this release — set features.refinement: true in config.yaml to opt in",
)

// GuardRefinement returns ErrRefinementDisabled if the refinement feature flag is off.
// Callers should print the error and exit non-zero.
func GuardRefinement(cfg *Config) error {
	if cfg == nil || !cfg.RefinementEnabled() {
		return ErrRefinementDisabled
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm --filter @restruct/cli exec go test ./internal/config/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/config/feature.go cli/internal/config/feature_test.go
git commit -m "feat(config): add GuardRefinement helper and ErrRefinementDisabled"
```

---

## Task 4: Short-circuit `refine` hook when flag is off

**Files:**
- Modify: `cli/cmd/refine.go`

- [ ] **Step 1: Find the exact insertion point**

In `cli/cmd/refine.go`, locate the block that looks like:

```go
		// Check if restruct is globally disabled
		if !toggle.IsEnabled(db.DataDir()) {
			slog.Debug("restruct disabled, passing through")
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		cfg, err := config.LoadFromViper()
		if err != nil {
			slog.Warn("config error, using defaults", "error", err)
			cfg = config.Defaults()
		}
```

- [ ] **Step 2: Move the config load above the sentinel check and add the feature-flag short-circuit**

Replace that block with:

```go
		cfg, err := config.LoadFromViper()
		if err != nil {
			slog.Warn("config error, using defaults", "error", err)
			cfg = config.Defaults()
		}

		// Feature flag gate: when refinement is not yet enabled in this release,
		// passthrough immediately. The sentinel toggle is only consulted when the
		// flag is on.
		if !cfg.RefinementEnabled() {
			slog.Debug("refinement feature disabled, passing through")
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}

		// Check if restruct is globally disabled (runtime sentinel toggle)
		if !toggle.IsEnabled(db.DataDir()) {
			slog.Debug("restruct disabled, passing through")
			return hook.WriteOutput(os.Stdout, hook.PassthroughOutput())
		}
```

Rationale: loading config before the sentinel check is safe (it's a small file read, no network), and it lets the feature flag win over the sentinel. Config load already happens later in the function, so we're moving it up, not adding a second load.

- [ ] **Step 3: Verify no stale duplicate config load remains**

The replacement in Step 2 moved the single `config.LoadFromViper()` call from below the sentinel check to above it. Scan the file for any remaining `config.LoadFromViper()` calls inside `refineCmd`'s RunE — there should be exactly one. If you see two, the replacement was incomplete; delete the lower one.

- [ ] **Step 4: Build and run the full Go test suite**

```bash
pnpm --filter @restruct/cli test
```

Expected: all tests PASS (nothing depends on the config-load position).

- [ ] **Step 5: Commit**

```bash
git add cli/cmd/refine.go
git commit -m "feat(refine): short-circuit when features.refinement is disabled"
```

---

## Task 5: Guard `enable`, `disable`, `status` commands

**Files:**
- Modify: `cli/cmd/toggle.go`

- [ ] **Step 1: Add feature-flag guards to each command**

Replace the contents of `cli/cmd/toggle.go` with:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/toggle"
)

func loadConfigOrDefaults() *config.Config {
	cfg, err := config.LoadFromViper()
	if err != nil || cfg == nil {
		return config.Defaults()
	}
	return cfg
}

var enableCmd = &cobra.Command{
	Use:           "enable",
	Short:         "Enable restruct prompt refinement",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.GuardRefinement(loadConfigOrDefaults()); err != nil {
			// root.go's Execute() prints the error and exits non-zero.
			return err
		}
		if err := toggle.Enable(db.DataDir()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: enabled")
		return nil
	},
}

var disableCmd = &cobra.Command{
	Use:           "disable",
	Short:         "Disable restruct prompt refinement (passthrough all prompts)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.GuardRefinement(loadConfigOrDefaults()); err != nil {
			return err
		}
		if err := toggle.Disable(db.DataDir()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: disabled — all prompts will pass through unrefined")
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: run 'restruct enable' to re-enable")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether restruct is enabled or disabled",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfigOrDefaults()
		if !cfg.RefinementEnabled() {
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: refinement feature not yet enabled in this release")
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: set features.refinement: true in config.yaml to opt in")
			return nil
		}
		if toggle.IsEnabled(db.DataDir()) {
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: enabled")
		} else {
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: disabled")
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: run 'restruct enable' to re-enable")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(statusCmd)
}
```

Notes:
- `status` does not return an error when the flag is off — it prints the explanation and exits zero. Only `enable`/`disable` return non-zero, because those are imperative operations the user asked to perform.
- `SilenceUsage` + `SilenceErrors` on `enable`/`disable` prevent cobra from printing the error itself. The single print comes from `root.go`'s `Execute()`, which is the existing convention for error output.

- [ ] **Step 2: Build and run the full Go test suite**

```bash
pnpm --filter @restruct/cli test
```

Expected: all tests PASS.

- [ ] **Step 3: Smoke-test the binary**

```bash
pnpm build
./plugin/bin/restruct status
./plugin/bin/restruct enable || echo "exit=$?"
```

Expected (with flag defaulting to off):

- `restruct status` prints two lines: the "feature not yet enabled" message and the hint to edit config.yaml. Exit 0.
- `restruct enable` prints the `ErrRefinementDisabled` message via `root.go`'s Execute wrapper. Exit 1 (observed via the `|| echo "exit=$?"` trailer).

The exact wording of the line printed for the failure is `restruct: refinement is not yet enabled in this release — set features.refinement: true in config.yaml to opt in`.

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/toggle.go
git commit -m "feat(toggle): gate enable/disable/status on features.refinement"
```

---

## Task 6: Gate `model load` on feature flag

**Files:**
- Modify: `cli/cmd/model.go`

- [ ] **Step 1: Add the flag check to `modelLoadCmd`**

In `cli/cmd/model.go`, find the `modelLoadCmd` RunE function. Immediately after the `cfg` is loaded and defaulted, add the short-circuit:

```go
var modelLoadCmd = &cobra.Command{
	Use:   "load [model]",
	Short: "Preload model into memory with configured keep_alive",
	Long:  `Sends a warm-up request to load the model into GPU/RAM and sets keep_alive so it stays resident.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.LoadFromViper()
		if cfg == nil {
			cfg = config.Defaults()
		}

		if !cfg.RefinementEnabled() {
			fmt.Fprintln(cmd.ErrOrStderr(), "restruct: model load skipped — refinement feature not yet enabled")
			return nil
		}

		// ...rest of existing function unchanged
```

Do not modify `modelPullCmd` or `modelStatusCmd` — those are operator-invoked diagnostic commands. Only `modelLoadCmd` runs as an automatic hook via `SessionStart`, which is where the no-op matters.

- [ ] **Step 2: Build and run tests**

```bash
pnpm --filter @restruct/cli test
```

Expected: PASS.

- [ ] **Step 3: Smoke-test**

```bash
pnpm build
./plugin/bin/restruct model load
```

Expected (flag off):
```
restruct: model load skipped — refinement feature not yet enabled
```
Exit code 0.

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/model.go
git commit -m "feat(model): skip load when features.refinement is disabled"
```

---

## Task 7: Update `doctor` report and `all_good` logic

**Files:**
- Modify: `cli/cmd/doctor.go`

- [ ] **Step 1: Add `RefinementFeatureEnabled` field to `DoctorReport`**

In `cli/cmd/doctor.go`, add a field to the struct:

```go
type DoctorReport struct {
	RefinementFeatureEnabled bool   `json:"refinement_feature_enabled"`
	OllamaInstalled          bool   `json:"ollama_installed"`
	OllamaBinaryPath         string `json:"ollama_binary_path,omitempty"`
	OllamaRunning            bool   `json:"ollama_running"`
	OllamaVersion            string `json:"ollama_version,omitempty"`
	MinVersion               string `json:"min_version"`
	VersionOK                bool   `json:"version_ok"`
	ModelRequired            string `json:"model_required"`
	ModelPulled              bool   `json:"model_pulled"`
	ModelSizeGB              string `json:"model_size_gb,omitempty"`
	KeepAlive                string `json:"keep_alive"`
	ConfigPath               string `json:"config_path,omitempty"`
	AllGood                  bool   `json:"all_good"`
}
```

- [ ] **Step 2: Populate the field and adjust `AllGood` logic**

Inside the RunE function, immediately after `report := DoctorReport{...}` initialization, set the field and change `AllGood`:

```go
		report := DoctorReport{
			RefinementFeatureEnabled: cfg.RefinementEnabled(),
			MinVersion:               cfg.Ollama.MinVersion,
			ModelRequired:            cfg.Ollama.Model,
			KeepAlive:                cfg.Ollama.KeepAlive.String(),
			ConfigPath:               configFileUsed(),
		}
```

And replace:

```go
		report.AllGood = report.OllamaInstalled && report.OllamaRunning &&
			report.VersionOK && report.ModelPulled
```

with:

```go
		if report.RefinementFeatureEnabled {
			report.AllGood = report.OllamaInstalled && report.OllamaRunning &&
				report.VersionOK && report.ModelPulled
		} else {
			// Non-refinement features (verify, permit, bootstrap, init) don't require
			// Ollama — when refinement is disabled, the plugin is always "all good".
			report.AllGood = true
		}
```

- [ ] **Step 3: Build and run tests**

```bash
pnpm --filter @restruct/cli test
```

Expected: PASS.

- [ ] **Step 4: Smoke-test**

```bash
pnpm build
./plugin/bin/restruct doctor
```

Expected JSON with `"refinement_feature_enabled": false` and `"all_good": true`.

- [ ] **Step 5: Commit**

```bash
git add cli/cmd/doctor.go
git commit -m "feat(doctor): report refinement feature flag and adjust all_good"
```

---

## Task 8: Full-stack smoke test (manual)

**Files:** none modified.

- [ ] **Step 1: Full test suite**

```bash
pnpm test
```

Expected: all tests PASS.

- [ ] **Step 2: Release build**

```bash
pnpm build
```

Expected: `plugin/bin/restruct-darwin-arm64`, `restruct-darwin-x86_64`, `restruct-linux-x86_64` all rebuilt.

- [ ] **Step 3: Refine passthrough test**

```bash
echo '{"prompt":"hey can you fix the auth bug where tokens expire too fast","session_id":"test-1","transcript_path":"/tmp/transcript","cwd":"'"$PWD"'"}' \
  | ./plugin/bin/restruct refine
```

Expected: JSON output containing `"hookSpecificOutput"` with passthrough (no refined additional context). No Ollama calls made. Verify by checking that the command completes in <100ms.

- [ ] **Step 4: Enable refusal test**

```bash
./plugin/bin/restruct enable
echo "exit=$?"
```

Expected:
```
restruct: refinement is not yet enabled in this release — set features.refinement: true in config.yaml to opt in
exit=1
```

- [ ] **Step 5: Flag-on round trip**

```bash
./plugin/bin/restruct config set features.refinement true
./plugin/bin/restruct status
./plugin/bin/restruct enable
./plugin/bin/restruct status
./plugin/bin/restruct disable
./plugin/bin/restruct status
./plugin/bin/restruct config set features.refinement false
```

Expected:
- After `config set features.refinement true` → status shows `enabled` (sentinel absent by default).
- After `enable` → still `enabled` (idempotent).
- After `disable` → status shows `disabled`.
- After second `config set features.refinement false` → config.yaml has the key set back to false; subsequent `status` would show the "not yet enabled" message.

Note: viper's `config set` writes string values. Verify that `features.refinement: "true"` in the resulting YAML still unmarshals as `true`. If not, amend Task 2 to add a `weaklyTypedInput` decoder option to `viper.Unmarshal` — or document that users must edit YAML directly. Resolution goes in a follow-up commit if needed.

- [ ] **Step 6: Commit any follow-up fixes from the smoke test**

If the smoke test revealed issues (e.g., `config set` producing a string that doesn't coerce), fix them and commit:

```bash
git add <files>
git commit -m "fix(config): <specific fix>"
```

If no issues were found, skip this step.

---

## Task 9: Create `setup-rules` skill

**Files:**
- Create: `plugin/skills/setup-rules/SKILL.md`

- [ ] **Step 1: Write the skill**

Create `plugin/skills/setup-rules/SKILL.md`:

```markdown
---
name: setup-rules
description: Generate or refresh the project's CLAUDE.md rules file. Delegates to the restruct init skill.
---

This skill generates a minimal, research-backed CLAUDE.md for the current project.
It delegates to the `init` skill, which handles discovery, user interview, and generation.

## What this skill does

1. Invokes `/restruct:init` to run the full CLAUDE.md generation flow.
2. If a CLAUDE.md already exists, `init` will audit it and propose changes rather than overwriting.

## When to use

- First-time setup of a new project with restruct.
- When you want to refresh an existing CLAUDE.md after major project changes.
- As part of the master `/restruct:setup` flow.

## Usage

Run `/restruct:init` to execute the full generation flow, or invoke this skill from
the master setup flow.
```

- [ ] **Step 2: Commit**

```bash
git add plugin/skills/setup-rules/SKILL.md
git commit -m "feat(skills): add setup-rules skill that delegates to init"
```

---

## Task 10: Create `setup-verify` skill

**Files:**
- Create: `plugin/skills/setup-verify/SKILL.md`

- [ ] **Step 1: Write the skill**

Create `plugin/skills/setup-verify/SKILL.md`:

```markdown
---
name: setup-verify
description: Discover lint/typecheck/test/build commands in the current project and write them to .restruct/verify.yaml so Claude Code automatically runs them on task completion.
---

Scan the project to discover what verification commands should run when Claude
completes a task. This generates `.restruct/verify.yaml` which enforces lint,
typecheck, build, and test rules automatically.

## Discovery process

1. Read CLAUDE.md (or AGENTS.md) for an "After Every Change" section — extract any commands listed there.
2. Look for `package.json` — check for scripts: `test`, `lint`, `build`, `typecheck`, `check`, `tsc`.
   - Determine the package manager (`pnpm`, `npm`, `yarn`) from lock files.
   - For monorepos with workspace filters, use the appropriate `--filter` syntax.
3. Look for `go.mod` — add `go vet ./...` and `go test ./...`.
4. Look for `Cargo.toml` — add `cargo check`, `cargo clippy` (if available), `cargo test`.
5. Look for `Makefile` or `Justfile` — check for `lint`, `test`, `check` targets.
6. For each discovered command, verify it actually works by running it (or `--help`).
7. Assign glob filters based on language/ecosystem:
   - TypeScript/JavaScript: match against the source directory (e.g., `web/**/*.ts`, `src/**/*.tsx`).
   - Go: `**/*.go` (or scoped to the Go module directory like `cli/**/*.go`).
   - Rust: `**/*.rs`.
   - Commands without a clear language scope (like `pnpm test` or `pnpm build`) get no globs — they run on any file change.

## Write the config

Create `.restruct/verify.yaml` with the discovered checks. Example format:

```yaml
checks:
  - name: test
    command: "pnpm test"
  - name: build
    command: "pnpm build"
  - name: typecheck
    command: "pnpm --filter web exec tsc --noEmit"
    globs:
      - "web/**/*.ts"
      - "web/**/*.tsx"
  - name: go-vet
    command: "pnpm --filter @restruct/cli exec go vet ./..."
    globs:
      - "cli/**/*.go"
```

## Confirm with the user

Show the user the discovered checks and ask if they want to adjust anything before finalizing.
If `.restruct/verify.yaml` already exists, show a diff and ask before overwriting.
```

- [ ] **Step 2: Commit**

```bash
git add plugin/skills/setup-verify/SKILL.md
git commit -m "feat(skills): add setup-verify skill for verify.yaml discovery"
```

---

## Task 11: Create `setup-permissions` skill

**Files:**
- Create: `plugin/skills/setup-permissions/SKILL.md`

- [ ] **Step 1: Write the skill**

Create `plugin/skills/setup-permissions/SKILL.md`:

```markdown
---
name: setup-permissions
description: Write an initial .restruct/permissions.yaml with safe defaults so the restruct permit hook can auto-approve common tool use without repeated dialogs.
---

Generate an initial permissions config that auto-approves safe, common tool operations
inside the project root. Pattern analysis of historical decisions is handled separately
by the `/restruct:review-permissions` skill — this skill is for first-time seeding.

## Step 1: Detect the project root

Use the current working directory as the project root. Confirm with the user if uncertain.

## Step 2: Check whether a permissions file already exists

```bash
cat .restruct/permissions.yaml 2>/dev/null
```

If it exists, show the current contents and ask whether to replace, merge, or skip.
Default to "skip" if the user doesn't respond — do not clobber existing configs.

## Step 3: Detect project ecosystem

Look at the root for signals that shape the defaults:

- `package.json` + lock file → allow `pnpm`, `npm`, `yarn`, `node` command families
- `go.mod` → allow `go test`, `go vet`, `go build`, `go run`
- `Cargo.toml` → allow `cargo test`, `cargo check`, `cargo build`
- `Makefile` / `justfile` → allow `make`, `just` invocations

## Step 4: Write the initial config

Create `.restruct/permissions.yaml` with:

```yaml
# Auto-approve common safe operations inside the project root.
# Use /restruct:review-permissions later to expand this from historical decision data.

allowed_paths:
  - "."   # reads/writes inside the project root

allowed_commands:
  # Ecosystem-specific — include lines that match detected tooling, drop the rest.
  - "pnpm test"
  - "pnpm lint"
  - "pnpm build"
  - "pnpm exec tsc --noEmit"
  - "go test ./..."
  - "go vet ./..."
  - "go build ./..."
  - "cargo test"
  - "cargo check"
  - "make test"
  - "make build"

trusted_urls:
  # Add package registries used by the project
  - "https://registry.npmjs.org/"
  - "https://proxy.golang.org/"
```

Only include the lines relevant to the ecosystems you actually detected.

## Step 5: Show the user the result

Print the written file and suggest they run `/restruct:review-permissions` after a few
sessions to expand it based on real usage patterns.
```

- [ ] **Step 2: Commit**

```bash
git add plugin/skills/setup-permissions/SKILL.md
git commit -m "feat(skills): add setup-permissions skill for initial permit defaults"
```

---

## Task 12: Create `setup-refine` skill

**Files:**
- Create: `plugin/skills/setup-refine/SKILL.md`

- [ ] **Step 1: Write the skill**

Create `plugin/skills/setup-refine/SKILL.md`:

```markdown
---
name: setup-refine
description: Install Ollama, pull the right model for your hardware, warm it up, and enable the restruct prompt refinement pipeline. Only runs when features.refinement is enabled in config.yaml.
---

This skill sets up the local LLM that powers restruct's prompt refinement.
It is only relevant when `features.refinement: true` is set in `~/.config/restruct/config.yaml`.

Execute ALL of the following steps immediately using the Bash tool. Do not describe
what you're going to do — just do it. Run each command yourself, report results briefly,
and move to the next step. Only pause if a command fails and you cannot fix it.

## Step 0: Check the feature flag

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct config get features.refinement 2>/dev/null
```

If the value is not `true`, stop and tell the user:

> "Refinement is feature-flagged off in this release. To enable it, edit
> `~/.config/restruct/config.yaml` and set `features.refinement: true`, then re-run this skill."

Do not proceed with Ollama installation if the flag is off.

## Step 1: Check if Ollama is installed

Run `which ollama` or `command -v ollama`.

If not found, install it:
- **macOS**: `brew install ollama`
- **Linux**: `curl -fsSL https://ollama.com/install.sh | sh`

## Step 2: Start Ollama

Check if Ollama is running: `curl -sf http://localhost:11434/api/version`

If not running:
- **macOS**: `brew services start ollama`
- **Linux**: start `ollama serve` in the background

Wait 2 seconds, then confirm it responds.

## Step 3: Check system memory and select model

Run `sysctl -n hw.memsize` (macOS) or `grep MemTotal /proc/meminfo` (Linux) to get total system RAM.

Select the model based on available memory:
- **32GB+**: `qwen2.5-coder:14b` (best quality, ~9GB model, needs ~16GB for inference)
- **16-31GB**: `qwen2.5-coder:7b` (good quality, ~4.5GB model, needs ~8GB for inference)
- **8-15GB**: `qwen2.5-coder:3b` (acceptable quality, ~2GB model)
- **<8GB**: Tell the user their system may not have enough memory for local LLM inference and suggest they check the restruct docs for alternatives.

Report the detected RAM and your model choice to the user.

## Step 4: Pull the model

Run `ollama pull <selected-model>` directly. This downloads the model weights.
It may take several minutes for larger models.

## Step 5: Warm the model

Run `ollama run <selected-model> "hello" --keepalive 60m` to load the model into GPU/RAM
and keep it resident. This ensures the first real refinement is fast.

## Step 6: Configure restruct to use the selected model

Only needed if the model differs from the default (`qwen2.5-coder:14b`):

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct config set ollama.model <selected-model>
```

## Step 7: Enable refinement

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct enable
```

This removes the runtime disable sentinel if present. (With the feature flag on and
sentinel absent, refinement is live.)

## Step 8: Final verification

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor` to confirm everything is green.

If `all_good` is `true` and `refinement_feature_enabled` is `true`, tell the user:
**"Restruct refinement is ready. Your prompts will be automatically refined via `<selected-model>`."**

If not, report what's still failing and attempt to fix it.
```

- [ ] **Step 2: Commit**

```bash
git add plugin/skills/setup-refine/SKILL.md
git commit -m "feat(skills): add setup-refine skill with feature-flag gate"
```

---

## Task 13: Rewrite the master `setup` skill

**Files:**
- Modify: `plugin/skills/setup/SKILL.md`

- [ ] **Step 1: Replace the contents**

Replace the entire contents of `plugin/skills/setup/SKILL.md` with:

```markdown
---
name: setup
description: First-time setup for restruct. Runs rule generation (CLAUDE.md), verify wiring, permission defaults, and — if the feature flag is on — prompt refinement setup.
---

This is the master setup flow for restruct. It orchestrates per-feature sub-skills
so you can configure the plugin end-to-end in one pass. Each sub-skill is also
directly invokable (e.g. `/restruct:setup-verify`) so you can re-run a single phase later.

Execute the steps in order. At each step, invoke the named sub-skill and wait for it
to complete before moving on. Report progress briefly between steps.

## Step 1: Project rules (CLAUDE.md)

Invoke the `setup-rules` skill. It generates or audits the project's CLAUDE.md file.

If the user declines or CLAUDE.md already exists and they are happy with it, continue.

## Step 2: Verification checks

Invoke the `setup-verify` skill. It discovers lint/typecheck/test/build commands
and writes `.restruct/verify.yaml` so Claude Code automatically runs them on task completion.

## Step 3: Tool permissions

Invoke the `setup-permissions` skill. It writes an initial `.restruct/permissions.yaml`
with safe defaults for the detected ecosystem.

## Step 4: Prompt refinement (feature-flagged)

Check whether the refinement feature is enabled:

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct config get features.refinement 2>/dev/null
```

- If the value is `true`: invoke the `setup-refine` skill to install Ollama, pull a model, and enable refinement.
- If the value is not `true`: print the following and skip to the summary:

  > "Prompt refinement is feature-flagged off in this release. Skipping Ollama / model setup.
  > To opt in, set `features.refinement: true` in `~/.config/restruct/config.yaml` and run
  > `/restruct:setup-refine`."

## Step 5: Summary

Print a summary of what was configured:

- CLAUDE.md status (created / audited / unchanged)
- `.restruct/verify.yaml` checks (count and names)
- `.restruct/permissions.yaml` status (created / skipped)
- Refinement status (enabled / feature-flagged off)

Point the user at the plugin README (`plugin/README.md` in the repo, or the installed
plugin directory) for further configuration options.
```

- [ ] **Step 2: Commit**

```bash
git add plugin/skills/setup/SKILL.md
git commit -m "refactor(skills): rewrite setup as master orchestrator"
```

---

## Task 14: Write `plugin/README.md` (user-facing)

**Files:**
- Create: `plugin/README.md`

- [ ] **Step 1: Write the README**

Create `plugin/README.md`:

```markdown
# Restruct

A Claude Code plugin that adds verification, tool permissions, rule bootstrapping,
CLAUDE.md generation, and — optionally — local-LLM prompt refinement.

> Prompt refinement is currently feature-flagged off. All other features are on
> by default and work without Ollama or any network calls.

## Features

| Feature | Default | What it does |
|---|---|---|
| **Verification** | On | Runs lint/test/build/typecheck after Claude completes a task. Config: `.restruct/verify.yaml`. Hooks: `TaskCompleted`, `Stop`. |
| **Tool permissions** | On | Intercepts `PreToolUse` to auto-approve safe commands and log decisions. Config: `.restruct/permissions.yaml`. |
| **Rule bootstrapping** | On | Parses CLAUDE.md / agents.md on `SessionStart` and `FileChanged` into a deep-context map used by refinement (and available for inspection). |
| **CLAUDE.md generation** | On-demand | `/restruct:init` generates a minimal, research-backed CLAUDE.md for your project. |
| **Snapshots** | On | Takes a file snapshot on task creation so verification has a baseline to diff against. |
| **Prompt refinement** | **Off (feature-flagged)** | Transforms casual prompts into structured, rules-aware instructions via a local Ollama model before Claude sees them. Not ready for external rollout yet — opt in via `features.refinement: true`. |

## Requirements

- Claude Code (CLI, desktop, web, or IDE extension)
- One of the supported platforms:
  - macOS arm64 (`darwin-arm64`)
  - macOS Intel (`darwin-x86_64`)
  - Linux x86_64 (`linux-x86_64`)
- (Optional, for refinement) [Ollama](https://ollama.com) and ~16 GB RAM for the default model

No other runtime dependencies — the `restruct` binary is pure Go with no CGO.

## Installation

Add the plugin from the marketplace in Claude Code:

```
/plugin marketplace add thejustinwalsh/claude-restruct-plugin
/plugin install restruct
```

Or install from a local path while developing:

```
/plugin marketplace add /path/to/claude-restruct-plugin/plugin
/plugin install restruct
```

Verify the binary is reachable:

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor
```

## First-time setup

Run the master setup flow in any project where you've installed the plugin:

```
/restruct:setup
```

This walks through four phases:

1. **Project rules** — generates or audits `CLAUDE.md`
2. **Verification** — discovers your lint/test/build commands and writes `.restruct/verify.yaml`
3. **Tool permissions** — writes `.restruct/permissions.yaml` with safe defaults
4. **Prompt refinement** — skipped unless `features.refinement: true` is set

Each phase is also invokable standalone so you can re-run one later:

- `/restruct:setup-rules` — regenerate or audit CLAUDE.md
- `/restruct:setup-verify` — rediscover verify commands
- `/restruct:setup-permissions` — refresh the permit config
- `/restruct:setup-refine` — install Ollama and enable refinement (feature-flagged)

## Configuration

Restruct stores config in `~/.config/restruct/config.yaml`. Use the CLI to get/set values:

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct config list
${CLAUDE_PLUGIN_ROOT}/bin/restruct config get features.refinement
${CLAUDE_PLUGIN_ROOT}/bin/restruct config set features.refinement true
```

Notable keys:

| Key | Default | Purpose |
|---|---|---|
| `features.refinement` | `false` | Master feature flag for prompt refinement. |
| `ollama.url` | `http://localhost:11434` | Ollama API URL (used when refinement is enabled). |
| `ollama.model` | `qwen2.5-coder:14b` | Model used for prompt refinement. |
| `ollama.keep_alive` | `60m` | How long Ollama keeps the model resident after the last request. |
| `cache.enabled` | `true` | Cache refined prompts for identical inputs. |
| `server.port` | `8377` | Port the restruct dashboard binds to (if you run it). |

## Enabling prompt refinement (advanced)

Prompt refinement is off by default because it requires Ollama and a ~9 GB model download,
and because the pipeline is still being tuned for external rollout. To opt in:

1. Flip the feature flag:

   ```bash
   ${CLAUDE_PLUGIN_ROOT}/bin/restruct config set features.refinement true
   ```

2. Run the refinement setup skill:

   ```
   /restruct:setup-refine
   ```

   This installs Ollama (via Homebrew on macOS or the official installer on Linux),
   pulls a Qwen model scaled to your RAM, warms it up, and verifies the pipeline with
   `restruct doctor`.

3. Optionally toggle refinement on/off at runtime without flipping the feature flag:

   ```bash
   ${CLAUDE_PLUGIN_ROOT}/bin/restruct disable  # pass prompts through unchanged
   ${CLAUDE_PLUGIN_ROOT}/bin/restruct enable   # re-enable
   ```

   This is useful when you want to keep the feature flag on but temporarily bypass
   refinement for a session.

## Troubleshooting

Run the doctor to see system readiness:

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor
```

It reports whether refinement is feature-flag-enabled, whether Ollama is installed and
running, whether the required model is pulled, and whether the config is loaded correctly.

Common issues:

- **`restruct: unexpected exit code` in hook output** — typically means a panic.
  Run `restruct refine --verbose` manually with a JSON payload on stdin and read the
  stderr output.
- **`restruct enable` refuses with "refinement is not yet enabled"** — expected when
  `features.refinement` is false. Flip the flag first.
- **Hooks timing out** — check that the restruct binary is reachable at
  `${CLAUDE_PLUGIN_ROOT}/bin/restruct` and that `chmod +x` is set on the platform binary.

## Local-only guarantee

Restruct does not send any data to external services. All refinement runs through your
local Ollama instance. Telemetry, session tracking, and metrics stay on your machine
in `~/.local/share/restruct/` (or equivalent XDG data directory).

## Links

- Source and contributor docs: https://github.com/thejustinwalsh/claude-restruct-plugin
- Research and design rationale: `docs/PRD.md` in the source repo
- Architecture reference: `docs/reference/ARCHITECTURE.md` in the source repo

## License

See the LICENSE file in the source repository.
```

- [ ] **Step 2: Commit**

```bash
git add plugin/README.md
git commit -m "docs(plugin): add user-facing plugin README"
```

---

## Task 15: Write repo-root `README.md` (contributor)

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write the README**

Create `README.md` at the repo root:

```markdown
# claude-restruct-plugin

Monorepo for the **restruct** Claude Code plugin — a local-LLM meta-prompt refinement
system plus a tool permissions, verification, and rule-bootstrapping stack.

> **Looking for install / usage docs?** See [`plugin/README.md`](plugin/README.md).
> This README is for contributors to the monorepo.

## Repository layout

```
.
├── cli/          Go CLI (the `restruct` binary)
│   ├── cmd/       Cobra command definitions
│   ├── internal/  Packages: pipeline, bootstrap, verify, permit, config, db, ...
│   └── main.go
├── web/          React 19 + Vite dashboard (embedded into the binary on release)
├── plugin/       The shippable plugin artifact — binaries, skills, plugin.json
│   ├── bin/       Cross-compiled restruct binaries (committed)
│   ├── skills/    Markdown skill definitions
│   └── .claude-plugin/  plugin.json + marketplace.json
├── docs/         PRD, milestones, architecture and API reference
└── xmake.lua     Build configuration (xmake + phony Go targets)
```

## Prerequisites

- **Go** 1.26+
- **Node** 22+ with **pnpm** 10+
- **xmake** (https://xmake.io)
- An **Ollama** install if you're working on the refinement pipeline (not required for
  CLI-only changes)

## Build and test

All commands run from the repo root. **Never `cd` into `cli/` or `web/`** — it breaks
session CWD and causes hooks to miss CLAUDE.md.

```bash
pnpm dev             # xmake watch + Vite HMR + docs server
pnpm build           # release build: xmake → Go cross-compile → plugin/bin/
pnpm test            # all Go tests via pnpm workspaces
pnpm --filter @restruct/cli test                    # only Go tests
pnpm --filter web lint                              # lint web TS
pnpm --filter web exec tsc --noEmit                 # type-check web
pnpm clean           # xmake clean
```

Run a single Go test:

```bash
pnpm --filter @restruct/cli exec go test ./internal/pipeline/ -run TestRefine -v
```

## Architecture at a glance

- The CLI writes all state to SQLite (sessions, refinements, pipeline events, tool
  decisions). The dashboard server only reads that DB plus writes ratings.
- All inference is local via Ollama — no data leaves the machine.
- Plugin hooks always map to CLI subcommands; the `restruct` binary is the interface
  for every hook operation.
- Debug builds use Go build tags (`//go:build debug`); release builds embed the web
  dist via `go:embed`.

Full architecture reference: [`docs/reference/ARCHITECTURE.md`](docs/reference/ARCHITECTURE.md).

Research and design rationale: [`docs/PRD.md`](docs/PRD.md).

## Release process

The plugin ships as a self-contained directory (`plugin/`) that Claude Code installs
from a marketplace URL or local path.

1. Run `pnpm build`. This produces:
   - `plugin/bin/restruct-darwin-arm64`
   - `plugin/bin/restruct-darwin-x86_64`
   - `plugin/bin/restruct-linux-x86_64`
   - `plugin/bin/restruct` (shell shim that dispatches on `uname`)
2. Commit the updated binaries. They are part of the shippable plugin and must be in git.
3. Tag a release and update the `version` in `plugin/.claude-plugin/plugin.json`.
4. End users install via `/plugin marketplace add thejustinwalsh/claude-restruct-plugin`.

## Constraints

- Pure Go, no CGO — uses `modernc.org/sqlite` for cross-compilation.
- xmake's native `set_languages("go")` is broken for Go 1.20+. Use phony targets with
  direct `go build` (see `xmake/phony.lua` and `xmake/go_build.lua`).
- TypeScript: no `as` type assertions. Fix the types instead.
- Web: use shadcn / Base-UI primitives before building custom components.

## Contributing

Conventional commits. Prompt versions tracked in `cli/internal/prompt/versions/`.
Add tests for new behavior — prefer focused unit tests in `internal/` packages over
broad integration tests. When fixing a bug, add a regression test before fixing.

See [`CLAUDE.md`](CLAUDE.md) for the full set of development conventions, and
[`web/CLAUDE.md`](web/CLAUDE.md) for frontend-specific rules.

## License

See [LICENSE](LICENSE) (or the LICENSE file in `plugin/` for the shipped artifact).
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add contributor README at repo root"
```

---

## Task 16: Standalone plugin smoke test (manual)

**Files:** none modified.

- [ ] **Step 1: Install the plugin into a scratch Claude Code project**

Create or pick a scratch project and install the plugin from the local path:

```
/plugin marketplace add /Users/tjw/Developer/claude-restruct-plugin/plugin
/plugin install restruct
```

- [ ] **Step 2: Confirm each hook fires without errors**

Inside the scratch project:

1. Open a new session — verify `SessionStart` hooks complete without red output (bootstrap + model load no-op).
2. Run `/restruct:init` — verify CLAUDE.md generation walks through discovery.
3. Run `/restruct:setup` — verify all four phases run, with refinement **skipped** with the "feature-flagged off" message.
4. Submit a casual prompt like "hey fix the auth bug" — verify Claude receives it **unchanged** (no refined context injected).
5. Run `/restruct:enable` — verify it surfaces the "refinement is not yet enabled in this release" message.

- [ ] **Step 3: Flag-on round trip**

1. `restruct config set features.refinement true` in the scratch project (via the plugin binary).
2. Run `/restruct:setup-refine` — verify the Ollama flow completes and `doctor` reports `all_good: true`.
3. Submit a casual prompt — verify the refined context is appended.
4. `restruct config set features.refinement false` to restore the default.

- [ ] **Step 4: Document any issues found and commit fixes**

If the smoke test surfaces issues, fix them and commit as separate commits — each
fix stays small and focused. If no issues, skip this step.

```bash
git add <files>
git commit -m "fix(<scope>): <specific fix from standalone smoke test>"
```

- [ ] **Step 5: Final wrap-up**

Run the full test suite one last time:

```bash
pnpm test
pnpm --filter web lint
pnpm --filter web exec tsc --noEmit
```

Expected: all green.

Confirm the working tree is clean:

```bash
git status
```

Expected: clean.

---

## Done

When all tasks are checked off:

- `features.refinement` defaults to false; refinement short-circuits to passthrough.
- `/restruct:setup` runs the four-phase master flow and skips refinement cleanly.
- Flipping `features.refinement: true` and running `/restruct:setup-refine` restores the
  full refinement pipeline with no rebuild needed.
- The plugin installs standalone from a marketplace URL.
- Two READMEs ship: contributor (root) and user (plugin/).
