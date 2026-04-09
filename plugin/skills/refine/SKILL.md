---
name: refine
description: Manually refine a prompt through the local LLM meta-prompt pipeline. Transforms a casual developer request into a structured, rules-aware prompt using a local Ollama model. Only works when features.refinement is enabled in config.yaml.
---

This skill transforms a casual developer request into a structured, rules-aware prompt using a local Ollama model. It reads project rules from agents.md/CLAUDE.md, gathers git context, and produces XML-structured output with objective, constraints, workflow, uncertainty protocol, and anti-patterns.

The binary is bundled in this plugin — always use the full path:

```
${CLAUDE_PLUGIN_ROOT}/bin/restruct refine
```

Usage:
- `echo '{"prompt":"your prompt here"}' | ${CLAUDE_PLUGIN_ROOT}/bin/restruct refine` to refine a prompt
- Add `--dry-run` to preview the refined prompt to stderr without replacing

The refinement pipeline runs entirely locally — no data leaves your machine.
