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
