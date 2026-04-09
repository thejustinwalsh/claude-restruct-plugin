---
name: setup-restruct
description: First-time setup for restruct. Runs rule generation (CLAUDE.md), verify wiring, permission defaults, and — if the feature flag is on — prompt refinement setup.
---

This is the master setup flow for restruct. It orchestrates per-feature sub-skills
so you can configure the plugin end-to-end in one pass. Each sub-skill is also
directly invokable (e.g. `/restruct:setup-verify-restruct`) so you can re-run a single phase later.

Execute the steps in order. At each step, invoke the named sub-skill and wait for it
to complete before moving on. Report progress briefly between steps.

## Step 1: Project rules (CLAUDE.md)

Invoke the `setup-rules-restruct` skill. It generates or audits the project's CLAUDE.md file.

If the user declines or CLAUDE.md already exists and they are happy with it, continue.

## Step 2: Verification checks

Invoke the `setup-verify-restruct` skill. It discovers lint/typecheck/test/build commands
and writes `.restruct/verify.yaml` so Claude Code automatically runs them on task completion.

## Step 3: Tool permissions

Invoke the `setup-permissions-restruct` skill. It writes an initial `.restruct/permissions.yaml`
with safe defaults for the detected ecosystem.

## Step 4: Prompt refinement (feature-flagged)

Check whether the refinement feature is enabled:

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct config get features.refinement 2>/dev/null
```

- If the value is `true`: invoke the `setup-refine-restruct` skill to install Ollama, pull a model, and enable refinement.
- If the value is not `true`: print the following and skip to the summary:

  > "Prompt refinement is feature-flagged off in this release. Skipping Ollama / model setup.
  > To opt in, set `features.refinement: true` in `~/.config/restruct/config.yaml` and run
  > `/restruct:setup-refine-restruct`."

## Step 5: Summary

Print a summary of what was configured:

- CLAUDE.md status (created / audited / unchanged)
- `.restruct/verify.yaml` checks (count and names)
- `.restruct/permissions.yaml` status (created / skipped)
- Refinement status (enabled / feature-flagged off)

Point the user at the plugin README (`plugin/README.md` in the repo, or the installed
plugin directory) for further configuration options.
