---
description: Review tool permission decisions across sessions and propose auto-approval rules. Analyzes which tools the user consistently approves, then suggests additions to .restruct/permissions.yaml to eliminate repetitive permission dialogs.
---

You are reviewing tool permission decision data to propose auto-approval rules. This is a data-driven workflow — base all proposals on actual usage patterns, not assumptions.

## Step 1: Load Decision Data

Query the restruct database for unreviewed tool decisions where the hook passed through but the user approved the tool (it executed successfully):

```bash
sqlite3 "$(find ~/.claude/plugins/data -name 'restruct.db' 2>/dev/null | head -1)" \
  "SELECT tool_name, tool_input_summary, COUNT(*) as count,
          COUNT(DISTINCT session_id) as sessions
   FROM tool_decisions
   WHERE reviewed = FALSE
     AND hook_decision = 'passthrough'
     AND outcome = 'executed'
   GROUP BY tool_name, tool_input_summary
   HAVING count >= 2
   ORDER BY count DESC
   LIMIT 30;"
```

If no results, inform the user there are no unreviewed decisions to process and stop.

## Step 2: Identify Patterns

Group the results into categories:

1. **Read commands outside project root** — commands reading from paths not in the project. These might need `allowed_paths` entries.
2. **Write commands** — commands that modify files. These are already auto-approved inside the project root, so these are likely outside it.
3. **Network requests** — URLs that aren't in the trusted list. Frequently-accessed APIs should be added to `trusted_urls`.
4. **Bash commands** — commands the tokenizer couldn't classify. Look for patterns that could be added to the known command lists.

For each pattern, note:
- How many times it was approved
- Across how many sessions
- Whether it was ever denied (query for same tool_input_summary with outcome != 'executed')

## Step 3: Read Current Config

Read the current permissions config:

```bash
cat .restruct/permissions.yaml 2>/dev/null || echo "No permissions.yaml found"
```

## Step 4: Propose Changes

For each identified pattern, propose a specific change to `.restruct/permissions.yaml`:

Format each proposal as:

### Proposal N: [Category]
- **Pattern**: `[tool_name] [summarized input]`
- **Approved**: N times across M sessions
- **Denied**: 0 times (or N times — flag if any denials exist)
- **Proposed rule**: Add `[specific YAML entry]` to `[section]`
- **Risk**: Low/Medium/High with explanation

**Do NOT propose rules for:**
- Commands that have been denied even once (needs human review)
- Network requests to unknown external services (only propose for well-known APIs)
- Write operations outside the project root (needs explicit user intent)
- Commands with environment variable expansion ($VAR patterns)

## Step 5: Apply Accepted Changes

After presenting all proposals, ask the user which ones to accept. For accepted proposals:

1. Update `.restruct/permissions.yaml` with the new rules (create if it doesn't exist)
2. Mark the reviewed decisions in the database:

```bash
sqlite3 "$(find ~/.claude/plugins/data -name 'restruct.db' 2>/dev/null | head -1)" \
  "UPDATE tool_decisions SET reviewed = TRUE, reviewed_at = datetime('now')
   WHERE reviewed = FALSE
     AND hook_decision = 'passthrough'
     AND outcome = 'executed';"
```

## Step 6: Summary

Report:
- How many proposals were made
- How many were accepted
- How many decisions were marked as reviewed
- Estimated permission dialogs that will be eliminated per session based on historical frequency

Remind the user they can run `/restruct:review-permissions` again after more sessions to continue improving auto-approvals.
