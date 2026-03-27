---
label: Home
icon: home
order: 1000
---

# Restruct

Restruct intercepts conversational prompts and transforms them into structured, rules-aware instructions via a local LLM before Claude sees them.

## Quick Start

```bash
# Install dependencies
pnpm install

# Dev mode (Go server + Vite HMR with file watching)
pnpm dev

# Release build (all platforms)
pnpm build

# Run tests
pnpm test
```

## Developer Testing

To test the plugin live inside Claude Code, point it at the plugin directory:

```bash
claude --plugin-dir /path/to/claude-restruct-plugin/plugin
```

This activates the hooks — `SessionStart` loads the Ollama model, and every prompt runs through the refinement pipeline. The structured `additionalContext` is injected hidden from the UI.

### Full dev workflow

```bash
# Terminal 1: dev server (Go + Vite HMR + file watcher)
pnpm dev

# Terminal 2: Claude Code with the plugin active
claude --plugin-dir ./plugin

# Dashboard at http://localhost:5173 (Vite) — shows live refinements
```

### Dry-run a refinement without Claude Code

```bash
echo '{"session_id":"test","transcript_path":"","cwd":".","hook_event_name":"UserPromptSubmit","prompt":"fix the auth bug"}' \
  | ./plugin/bin/restruct refine --dry-run --verbose
```

### Prerequisites

- **Ollama running** with the configured model (`ollama pull qwen2.5-coder:14b`)
- **pnpm install** for dependencies
- **pnpm dev** or **pnpm build** to compile the binary

## Architecture

```
User prompt → Claude Code hook (UserPromptSubmit)
    → Restruct CLI (Go binary)
        → Local LLM (Ollama) refines prompt
        → Returns structured additionalContext
    → Claude sees original prompt + hidden structured context
```

## Documentation

| Section | Description |
|---------|-------------|
| [PRD](PRD.md) | Product requirements and research |
| [Milestones](milestones/) | Implementation roadmap and progress |
| [Reference](reference/) | Technical specifications |

## Project Structure

```
├── cli/                    # Go CLI (Cobra + Viper)
│   ├── cmd/                # CLI commands
│   └── internal/           # Core packages
│       ├── db/             # SQLite + migrations
│       ├── ollama/         # LLM client
│       ├── pipeline/       # Refinement pipeline
│       ├── prompt/         # System prompt template
│       ├── server/         # HTTP server + embedded SPA
│       └── session/        # Session management
├── web/                    # React/Vite dashboard
├── plugin/                 # Claude Code plugin packaging
│   ├── .claude-plugin/     # Plugin manifest
│   ├── bin/                # Built binaries
│   └── skills/             # Plugin skills
├── docs/                   # This documentation
├── xmake.lua               # Build graph
└── xmake/                  # Build DSL modules
```
