Set up the restruct meta-prompt system. Run diagnostics, install dependencies, and warm the model.

## Important: Binary Path

The restruct binary is bundled inside this plugin. It is NOT on the user's PATH.

Always invoke it using the absolute path via the plugin root:

```
${CLAUDE_PLUGIN_ROOT}/bin/restruct <command>
```

For example: `${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor`

All commands below use `restruct` as shorthand — always expand to the full path above when running them.

## Steps

### 1. Run diagnostics

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor` and read the JSON output. This tells you everything about the current state:
- `ollama_installed` — whether the `ollama` binary is on PATH
- `ollama_running` — whether the Ollama server is responding
- `ollama_version` / `version_ok` — whether the installed version meets the minimum
- `model_pulled` — whether the configured model is available locally
- `all_good` — true if everything is ready

If `all_good` is true, skip to step 5 (load model).

### 2. Install Ollama (if `ollama_installed` is false)

Tell the user to install Ollama:
- **macOS**: `brew install ollama` or download from https://ollama.com/download
- **Linux**: `curl -fsSL https://ollama.com/install.sh | sh`

After install, the user needs to start the server: `ollama serve` (or it may auto-start).

Then re-run `${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor` to confirm.

### 3. Pull the model (if `model_pulled` is false)

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct model pull`. This downloads the configured model.

Progress is printed to stderr. The JSON result on stdout confirms success:
```json
{"ok": true, "model": "qwen2.5-coder:14b", "duration": "2m30s"}
```

If the user has limited RAM (< 16GB), suggest they configure a smaller model:
```
${CLAUDE_PLUGIN_ROOT}/bin/restruct config set ollama.model qwen3:7b
${CLAUDE_PLUGIN_ROOT}/bin/restruct model pull
```

### 4. Check model status

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct model status` to see all available models and confirm the configured one is present.

### 5. Load and warm the model

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct model load` to preload the model into GPU/RAM with a 60-minute keep_alive.

This ensures the first real refinement is fast (no cold-start delay).

The JSON result confirms:
```json
{"ok": true, "model": "qwen2.5-coder:14b", "keep_alive": "1h0m0s", "duration": "3.2s"}
```

### 6. Confirm

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor` one final time. If `all_good` is true, tell the user they're ready. The hook will automatically refine prompts on every message.

If they want to test it: `echo '{"prompt":"fix the auth bug"}' | ${CLAUDE_PLUGIN_ROOT}/bin/restruct refine --dry-run`
