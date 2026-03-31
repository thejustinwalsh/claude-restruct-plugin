# M13: Team Configuration

## Goal

Establish a layered configuration system that supports team-shared defaults (checked into repo) with per-user overrides. Replace the single-user Viper config with a multi-source hierarchy: `.restruct/config.yaml` (team) → `~/.config/restruct/config.yaml` (user) → env vars → plugin options (secrets only).

## Depends On

M7 (CLI UX — config command exists), M8 (Plugin Distribution — plugin.json manifest)

---

## Problem Statement

The current config system (`~/.config/restruct/config.yaml`) is single-user. When a team adopts restruct, each member must manually configure the same model, temperature, and rules settings. There's no way to check in team defaults. Plugin options (`userConfig` in plugin.json) would solve the UX problem but are per-user only — Anthropic's own source has a TODO acknowledging project-scoped plugin options don't exist.

## Solution

A layered config hierarchy using `.restruct/config.yaml` as the team-shared defaults file, fitting naturally alongside `.restruct/verify.yaml` and `.restruct/system_prompt.tmpl`.

### Config Precedence (Highest Wins)

```
CLAUDE_PLUGIN_OPTION_*    (plugin userConfig, secrets only)
  ↓
RESTRUCT_OLLAMA_MODEL     (env vars, CI/personal)
  ↓
~/.config/restruct/config.yaml  (user personal prefs)
  ↓
.restruct/config.yaml     (team defaults, checked in)
  ↓
compiled defaults          (Defaults() in config.go)
```

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 13.1 — Add `.restruct/` to Viper config search | 0.5h | In `root.go initConfig()`, add `viper.AddConfigPath(".restruct")` before `viper.AddConfigPath(".")`. Search order: `--config` flag → `.restruct/config.yaml` → `./config.yaml` → `~/.config/restruct/config.yaml` |
| 13.2 — `restruct init` command | 2h | New `cmd/init.go`: scaffolds `.restruct/config.yaml` with commented defaults. Detects existing config and merges. Creates `.restruct/` directory structure if needed |
| 13.3 — Selective `.gitignore` for `.restruct/` | 1h | Update `hook/install.go`: instead of ignoring all of `.restruct/`, ignore `.restruct/sessions/` and `.restruct/*.db` but allow `.restruct/config.yaml`, `.restruct/verify.yaml`, `.restruct/system_prompt.tmpl`, `.restruct/links/` |
| 13.4 — Add `userConfig` to plugin.json for secrets | 1h | Declare optional `ollama_url` and `ollama_api_key` (sensitive: true) in manifest. These override YAML values via `CLAUDE_PLUGIN_OPTION_*` env vars in hooks. Only for authenticated/remote Ollama setups |
| 13.5 — Config precedence documentation | 1h | Document the layering in a comment block in `config.go` and in `docs/reference/`. Include examples of team vs user config |
| 13.6 — `restruct config show --resolved` | 1.5h | Show the fully resolved config with source annotations (which file/env var each value came from). Add source tracking to config loading |
| 13.7 — Tests | 1.5h | Test config layering: team file overrides defaults, user file overrides team, env vars override all. Test `restruct init` scaffolding. Test `.gitignore` selective patterns |

---

## Example `.restruct/config.yaml` (Team Shared)

```yaml
# Team configuration — checked into repo
# Personal overrides: ~/.config/restruct/config.yaml or RESTRUCT_* env vars

ollama:
  model: qwen2.5-coder:14b    # team-agreed model
  url: http://localhost:11434  # default Ollama

refinement:
  temperature: 0.3
  max_tokens: 2048
  min_words: 5

rules:
  files:
    - CLAUDE.md
    - agents.md

server:
  port: "8377"
```

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Team config location | `.restruct/config.yaml` | Consistent with existing `.restruct/` convention (verify.yaml, system_prompt.tmpl) |
| Plugin options scope | Secrets only | Plugin options are per-user with no project-scope write path (confirmed in source). YAML is better for shareable config |
| `.gitignore` strategy | Selective ignoring | Allow config/verify/links, ignore sessions/db. Teams can share project setup |
| `restruct init` | Scaffolding command | Reduces onboarding friction. Creates config with commented defaults, explains override mechanism |
| Env var prefix | `RESTRUCT_` | Already works via `viper.SetEnvPrefix("RESTRUCT")` + `viper.AutomaticEnv()` |

## Acceptance Criteria

- `.restruct/config.yaml` is read by Viper and values override defaults
- `~/.config/restruct/config.yaml` overrides `.restruct/config.yaml`
- `RESTRUCT_*` env vars override everything except plugin options
- `restruct init` scaffolds a working team config
- `.restruct/config.yaml` and `.restruct/verify.yaml` are NOT gitignored
- `restruct config show --resolved` shows source of each value
- Existing single-user configs continue to work (backward compatible)

## Risk

**Low.** Viper's multi-path config is well-tested. Main risk is the `.gitignore` change — existing users with `.restruct/` fully ignored won't accidentally commit session data because `restruct init` handles the migration.

## Files Modified

- `cli/cmd/root.go` — add `.restruct` config path
- `cli/cmd/init.go` — NEW: scaffolding command
- `cli/cmd/config_cmd.go` — `--resolved` flag
- `cli/internal/config/config.go` — source tracking
- `cli/internal/hook/install.go` — selective gitignore
- `plugin/.claude-plugin/plugin.json` — add `userConfig` for secrets
