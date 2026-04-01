# Restruct

## Build & Test — ALL commands run from project root, never `cd` into subdirectories
- `pnpm dev` — starts xmake watch (auto-rebuild Go on change) + Vite HMR + docs server
- `pnpm build` — release build: xmake → Go cross-compile → `plugin/bin/`
- `pnpm test` — runs all Go tests via pnpm workspaces (`go test ./...` in cli/)
- `pnpm --filter @restruct/cli test` — run only Go tests
- `pnpm --filter @restruct/cli exec go test ./internal/pipeline/ -run TestRefine -v` — run a single Go test
- `pnpm --filter web lint` — lint web TypeScript
- `pnpm --filter web exec tsc --noEmit` — type-check web
- `pnpm clean` — xmake clean

## Build System
- xmake (not Make) with pnpm workspaces. Build config in `xmake.lua`, custom DSL in `xmake/`
- xmake's native `set_languages("go")` is broken for Go 1.20+. Use phony targets with direct `go build`
- Debug builds use Go build tags (`//go:build debug`). Release embeds web dist via `go:embed`

## Code Style
- TypeScript: no `as` type assertions — they mask type errors. Fix the types instead
- Web components: use shadcn (`@shadcn/ui`) before building custom components
- Go: pure Go only, no CGO. SQLite via `modernc.org/sqlite` for cross-compilation

## Architecture
- CLI writes to SQLite (sessions, refinements, pipeline events). Server only reads + writes ratings
- All inference is local via Ollama — no data leaves the machine
- Plugin hooks: `additionalContext` is appended alongside the user's prompt, never replaces it
- @docs/PRD.md for research and design rationale
- @docs/milestones/ for implementation roadmap

## Workflow
- Conventional commits
- Prompt versions tracked in `cli/internal/prompt/versions/`
- If you added new behavior, add or update tests to cover it
- Prefer focused unit tests over broad integration tests
- When fixing a bug, add a test that reproduces it before fixing

## Constraints
- Hooks always map to CLI commands — the restruct binary is the interface for all hook operations
- Performance is critical due to the interactive nature of the system; track timing with metrics and use performant algorithms in critical code paths

## Do NOT
- Do not use `as` in TypeScript to fix type errors
- Do not use xmake `set_languages("go")` — use phony targets
- Do not add CGO dependencies — breaks cross-compilation
- Do not build custom web components when shadcn has one
- Do not send any user data to external services — local-only
- Do not `cd` into `cli/` or `web/` — it breaks session CWD and causes hooks to miss CLAUDE.md. All commands run from project root

## Design
- Design context and principles in @web/CLAUDE.md
