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
| **CLAUDE.md generation** | On-demand | `/restruct:init-restruct` generates a minimal, research-backed CLAUDE.md for your project. |
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

Add the marketplace in Claude Code, then install the plugin:

```
/plugin marketplace add thejustinwalsh/claude-plugins
/plugin install restruct@thejustinwalsh
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
/restruct:setup-restruct
```

This walks through four phases:

1. **Project rules** — generates or audits `CLAUDE.md`
2. **Verification** — discovers your lint/test/build commands and writes `.restruct/verify.yaml`
3. **Tool permissions** — writes `.restruct/permissions.yaml` with safe defaults
4. **Prompt refinement** — skipped unless `features.refinement: true` is set

Each phase is also invokable standalone so you can re-run one later:

- `/restruct:setup-rules-restruct` — regenerate or audit CLAUDE.md
- `/restruct:setup-verify-restruct` — rediscover verify commands
- `/restruct:setup-permissions-restruct` — refresh the permit config
- `/restruct:setup-refine-restruct` — install Ollama and enable refinement (feature-flagged)

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
   /restruct:setup-refine-restruct
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
