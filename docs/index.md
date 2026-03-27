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
