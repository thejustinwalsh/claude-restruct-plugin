---
name: setup-rules
description: Generate or refresh the project's CLAUDE.md rules file. Delegates to the restruct init skill.
---

This skill generates a minimal, research-backed CLAUDE.md for the current project.
It delegates to the `init` skill, which handles discovery, user interview, and generation.

## What this skill does

1. Invokes `/restruct:init` to run the full CLAUDE.md generation flow.
2. If a CLAUDE.md already exists, `init` will audit it and propose changes rather than overwriting.

## When to use

- First-time setup of a new project with restruct.
- When you want to refresh an existing CLAUDE.md after major project changes.
- As part of the master `/restruct:setup` flow.

## Usage

Run `/restruct:init` to execute the full generation flow, or invoke this skill from
the master setup flow.
