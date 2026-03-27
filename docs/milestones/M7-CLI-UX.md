# M6: CLI UX & Configuration

## Goal

Polish the CLI commands for a great developer experience: clear error messages, helpful diagnostics, smooth onboarding, and intuitive configuration.

## Depends On

M2 (Core Pipeline) — needs stable internals before polishing the surface.

---

## Tasks

### 6.1 — Doctor Command Enhancement

**What:** Make `restruct doctor` a comprehensive diagnostic tool that identifies and suggests fixes for every possible setup issue.

**Current behavior:** Checks Ollama install, version, server, models. Returns JSON.

**Improvements:**
- **Human-readable output** by default (colored terminal output with pass/fail indicators). `--json` flag for machine-readable.
- **Check list:**
  | Check | Pass | Fail Action |
  |-------|------|-------------|
  | Ollama installed | `ollama` in PATH | Print install instructions |
  | Ollama version | >= min_version | Print upgrade instructions |
  | Ollama server running | API responds | Print `ollama serve` instructions |
  | Configured model available | Model in list | Print `restruct model pull` instructions |
  | Model loaded in memory | Model has active session | Print `restruct model load` suggestion |
  | Config file exists | Found and parseable | Print `restruct config` instructions |
  | Hook installed | `.claude/settings.json` has hook | Print `restruct install` instructions |
  | Rules files exist | At least one found | Suggest creating `agents.md` |
  | Cache directory writable | Can write test file | Print permissions fix |
  | Binary in PATH | `restruct` resolves | Print PATH instructions |
- **Summary:** End with "X/Y checks passed. Run `restruct doctor --fix` to auto-fix what we can."
- **Auto-fix mode:** `--fix` flag attempts to fix what it can (install hook, create config, create cache dir). Never destructive.

### 6.2 — Error Message Quality

**What:** Every error message should tell the user what went wrong AND what to do about it.

**Pattern:**
```
Error: Could not connect to Ollama at http://localhost:11434

  Ollama doesn't appear to be running. To fix this:
  1. Start Ollama: ollama serve
  2. Or check if it's running on a different port
  3. Run 'restruct doctor' for full diagnostics
```

**Audit all error paths in:**
- `cmd/refine.go` — hook I/O errors, pipeline errors
- `cmd/model.go` — pull failures, load failures
- `cmd/install.go` — permission errors, existing hooks
- `cmd/config_cmd.go` — invalid keys, write failures
- `pipeline/pipeline.go` — all degradation paths

### 6.3 — Config Command Polish

**What:** Make configuration intuitive.

**Improvements:**
- `restruct config list` — show all config with source annotation (default/file/env/flag)
- `restruct config set` — validate values before writing (e.g., temperature must be 0-1, URL must be valid)
- `restruct config reset` — reset to defaults
- `restruct config path` — print the config file path being used
- `restruct config edit` — open config in $EDITOR
- **Tab completion:** Add shell completion for config keys

### 6.4 — Model Command Polish

**What:** Improve model management UX.

**Improvements:**
- `restruct model pull` — show download progress bar (the Ollama API streams progress)
- `restruct model status` — show: model name, size, quantization, loaded/unloaded, VRAM usage
- `restruct model list` — show all Ollama models with a marker on the configured one
- `restruct model recommend` — detect system RAM/VRAM and recommend the best model
  - 32GB+ → qwen2.5-coder:14b
  - 16GB → qwen2.5-coder:14b (with warning about memory pressure)
  - 8GB → qwen3:7b
  - <8GB → phi-4-mini

### 6.5 — Shell Completions

**What:** Generate shell completions for bash, zsh, fish, and PowerShell.

**Implementation:** Cobra has built-in completion generation. Add:
- `restruct completion bash`
- `restruct completion zsh`
- `restruct completion fish`
- `restruct completion powershell`
- Document installation in setup skill

### 6.6 — Version & Update Check

**What:** Help users stay current.

- `restruct version` — print version, build date, Go version, configured model
- `restruct version --check` — compare against latest GitHub release (non-blocking, cache result for 24h)
- On `restruct refine`, if version is >30 days old, print a one-line update notice to stderr (max once per day)

---

## Acceptance Criteria

- [ ] `doctor` checks 10+ conditions with human-readable output and auto-fix
- [ ] Every error message includes remediation steps
- [ ] Config commands validate input and show value sources
- [ ] Model commands show progress and hardware-based recommendations
- [ ] Shell completions for bash/zsh/fish
- [ ] Version command with optional update check

## Files Modified

- `cli/cmd/doctor.go` — enhanced checks, human output, auto-fix
- `cli/cmd/config_cmd.go` — validation, reset, edit, path
- `cli/cmd/model.go` — progress bar, recommend, enhanced status
- `cli/cmd/root.go` — completion subcommand
- New: `cli/cmd/version.go` — version and update check
- All `cmd/*.go` — error message audit

## Risk

**Low.** This is UX polish — no architectural risk. The main risk is scope creep (adding too many convenience features). Stay focused on the checks and error messages that save the most user time.
