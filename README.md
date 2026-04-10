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
from a marketplace URL or local path. Platform binaries are built and pushed by CI —
they are **not** committed manually.

1. Update the `version` in `plugin/.claude-plugin/plugin.json`.
2. Tag a release: `git tag v<version> && git push --tags`.
3. The `Build` workflow (`.github/workflows/build.yml`) runs on the tag:
   - `pnpm install --frozen-lockfile`
   - `pnpm build` — produces the cross-compiled platform binaries:
     - `plugin/bin/restruct-darwin-arm64`
     - `plugin/bin/restruct-darwin-x86_64`
     - `plugin/bin/restruct-linux-x86_64`
   - `git add -f plugin/bin/restruct-*` (the `-f` bypasses the gitignore that
     excludes platform binaries in normal dev)
   - Commits as `build: release binaries for v<version>` and pushes to `main`
4. End users install via `/plugin marketplace add thejustinwalsh/claude-plugins`
   then `/plugin install restruct@thejustinwalsh` and get the CI-built binaries.

For local development, `pnpm build` produces the same binaries in `plugin/bin/` but
they stay ignored by git — only the `plugin/bin/restruct` shell shim is tracked.
You can also manually trigger the `Build` workflow via `workflow_dispatch` without
a tag if you need to refresh the published binaries outside a release.

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
