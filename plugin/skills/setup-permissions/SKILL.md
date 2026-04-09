---
name: setup-permissions
description: Write an initial .restruct/permissions.yaml with safe defaults so the restruct permit hook can auto-approve common tool use without repeated dialogs.
---

Generate an initial permissions config that auto-approves safe, common tool operations
inside the project root. Pattern analysis of historical decisions is handled separately
by the `/restruct:review-permissions` skill — this skill is for first-time seeding.

## Step 1: Detect the project root

Use the current working directory as the project root. Confirm with the user if uncertain.

## Step 2: Check whether a permissions file already exists

```bash
cat .restruct/permissions.yaml 2>/dev/null
```

If it exists, show the current contents and ask whether to replace, merge, or skip.
Default to "skip" if the user doesn't respond — do not clobber existing configs.

## Step 3: Detect project ecosystem

Look at the root for signals that shape the defaults:

- `package.json` + lock file → allow `pnpm`, `npm`, `yarn`, `node` command families
- `go.mod` → allow `go test`, `go vet`, `go build`, `go run`
- `Cargo.toml` → allow `cargo test`, `cargo check`, `cargo build`
- `Makefile` / `justfile` → allow `make`, `just` invocations

## Step 4: Write the initial config

Create `.restruct/permissions.yaml` with:

```yaml
# Auto-approve common safe operations inside the project root.
# Use /restruct:review-permissions later to expand this from historical decision data.

allowed_paths:
  - "."   # reads/writes inside the project root

allowed_commands:
  # Ecosystem-specific — include lines that match detected tooling, drop the rest.
  - "pnpm test"
  - "pnpm lint"
  - "pnpm build"
  - "pnpm exec tsc --noEmit"
  - "go test ./..."
  - "go vet ./..."
  - "go build ./..."
  - "cargo test"
  - "cargo check"
  - "make test"
  - "make build"

trusted_urls:
  # Add package registries used by the project
  - "https://registry.npmjs.org/"
  - "https://proxy.golang.org/"
```

Only include the lines relevant to the ecosystems you actually detected.

## Step 5: Show the user the result

Print the written file and suggest they run `/restruct:review-permissions` after a few
sessions to expand it based on real usage patterns.
