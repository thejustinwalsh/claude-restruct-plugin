---
description: Set up the restruct meta-prompt system. Runs diagnostics, installs Ollama, selects the right model for your hardware, and warms everything up.
---

Execute ALL of the following steps immediately using the Bash tool. Do not describe what you're going to do — just do it. Run each command yourself, report results briefly, and move to the next step. Only pause if a command fails and you cannot fix it.

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

Run `ollama pull <selected-model>` directly. This downloads the model weights. It may take several minutes for larger models.

## Step 5: Warm the model

Run `ollama run <selected-model> "hello" --keepalive 60m` to load the model into GPU/RAM and keep it resident. This ensures the first real refinement is fast.

## Step 6: Configure restruct to use the selected model

Only needed if the model differs from the default (`qwen2.5-coder:14b`):

```
${CLAUDE_PLUGIN_ROOT}/bin/restruct config set ollama.model <selected-model>
```

## Step 7: Final verification

Run `${CLAUDE_PLUGIN_ROOT}/bin/restruct doctor` to confirm everything is green.

If `all_good` is true, tell the user: **"Restruct is ready. Your prompts will be automatically refined via `<selected-model>`."**

If not, report what's still failing and attempt to fix it.

## Step 8: Configure verification checks

Scan the project to discover what verification commands should run when Claude completes a task. This generates `.restruct/verify.yaml` which enforces lint, typecheck, build, and test rules automatically.

**Discovery process:**

1. Read CLAUDE.md (or AGENTS.md) for an "After Every Change" section — extract any commands listed there
2. Look for `package.json` — check for scripts: `test`, `lint`, `build`, `typecheck`, `check`, `tsc`
   - Determine the package manager (`pnpm`, `npm`, `yarn`) from lock files
   - For monorepos with workspace filters, use the appropriate `--filter` syntax
3. Look for `go.mod` — add `go vet ./...` and `go test ./...`
4. Look for `Cargo.toml` — add `cargo check`, `cargo clippy` (if available), `cargo test`
5. Look for `Makefile` or `Justfile` — check for `lint`, `test`, `check` targets
6. For each discovered command, verify it actually works by running it (or `--help`)
7. Assign glob filters based on language/ecosystem:
   - TypeScript/JavaScript: match against the source directory (e.g., `web/**/*.ts`, `src/**/*.tsx`)
   - Go: `**/*.go` (or scoped to the Go module directory like `cli/**/*.go`)
   - Rust: `**/*.rs`
   - Commands without a clear language scope (like `pnpm test` or `pnpm build`) get no globs — they run on any file change

**Write the config:**

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

**Show the user** the discovered checks and ask if they want to adjust anything before finalizing.
