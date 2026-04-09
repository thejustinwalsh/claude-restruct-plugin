---
name: setup-verify-restruct
description: Discover lint/typecheck/test/build commands in the current project and write them to .restruct/verify.yaml so Claude Code automatically runs them on task completion.
---

Scan the project to discover what verification commands should run when Claude
completes a task. This generates `.restruct/verify.yaml` which enforces lint,
typecheck, build, and test rules automatically.

## Discovery process

1. Read CLAUDE.md (or AGENTS.md) for an "After Every Change" section — extract any commands listed there.
2. Look for `package.json` — check for scripts: `test`, `lint`, `build`, `typecheck`, `check`, `tsc`.
   - Determine the package manager (`pnpm`, `npm`, `yarn`) from lock files.
   - For monorepos with workspace filters, use the appropriate `--filter` syntax.
3. Look for `go.mod` — add `go vet ./...` and `go test ./...`.
4. Look for `Cargo.toml` — add `cargo check`, `cargo clippy` (if available), `cargo test`.
5. Look for `Makefile` or `Justfile` — check for `lint`, `test`, `check` targets.
6. For each discovered command, verify it actually works by running it (or `--help`).
7. Assign glob filters based on language/ecosystem:
   - TypeScript/JavaScript: match against the source directory (e.g., `web/**/*.ts`, `src/**/*.tsx`).
   - Go: `**/*.go` (or scoped to the Go module directory like `cli/**/*.go`).
   - Rust: `**/*.rs`.
   - Commands without a clear language scope (like `pnpm test` or `pnpm build`) get no globs — they run on any file change.

## Write the config

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

## Confirm with the user

Show the user the discovered checks and ask if they want to adjust anything before finalizing.
If `.restruct/verify.yaml` already exists, show a diff and ask before overwriting.
