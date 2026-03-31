# Prompting Research & Findings

Research findings applied during restruct development, with citations and how each informed the system design.

---

## XML Tag Effectiveness in Claude

**Finding:** No XML tag names carry special semantic weight in Claude. Tag names are parsing hints, not compliance triggers. Anthropic's official docs confirm: "There are no canonical 'best' XML tags that Claude has been trained with in particular."

**Source:** [Anthropic Prompt Engineering Docs — Use XML Tags](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/use-xml-tags)

**Applied:** Renamed tags for clarity rather than magic: `<context_supplement>` to `<context>`, `<considerations>` to `<analysis>`, `<ambiguities>` to `<clarification_needed>`. Compliance comes from content quality and explicitness, not tag naming.

---

## Claude Code Plan Mode

**Finding:** Plan mode is a **permission mode** enforced by the Claude Code harness, not a prompt-triggered behavior. There are no keywords or phrases that make Claude enter plan mode from within a prompt. It must be set via CLI flag (`--permission-mode plan`), settings.json, or Shift+Tab cycling.

**Source:** [Claude Code Permission Modes](https://code.claude.com/docs/en/permission-modes.md), [Common Workflows](https://code.claude.com/docs/en/common-workflows.md)

**Applied:** Instead of trying to trigger plan mode (impossible from `additionalContext`), we inject a behavioral instruction: "Before implementing, present a plan and wait for approval." This achieves plan-like behavior through prompt engineering rather than permission mode switching.

---

## Post-Process Constraint Injection

**Finding:** System-level directives for Claude (plan mode, sub-agent delegation) are better injected as a post-processing step in Go code than baked into the local LLM's system prompt. This avoids:
1. Wasting local LLM tokens on reproducing boilerplate
2. Risk of the local LLM paraphrasing or dropping directives
3. Prompt leakage (system instructions appearing in output)

**Applied:** `appendConstraints()` in `pipeline.go` injects two hardcoded directives after LLM output is validated. The local LLM never sees them.

---

## Prompting Technique Research (from PRD)

These findings from the PRD informed the system prompt design:

### Chain-of-Thought (CoT)
- Wei et al. (2022): CoT improves arithmetic, commonsense, symbolic reasoning. 540B model with 8 CoT exemplars achieved SOTA on GSM8K.
- Wang et al. (2022): Self-Consistency extends CoT. GSM8K +17.9%, SVAMP +11.0%, AQuA +12.2%.
- **Wharton 2025 caveat:** CoT value is *decreasing* for reasoning-native models (Claude, o1/o3). Non-reasoning models benefit more.
- **Applied:** CoT scaffolding used in the local refinement LLM (smaller, benefits more). Not forced onto Claude.

### Expert Role / Persona Prompting
- ExpertPrompting (Xu et al., 2023): Improved GPT-3.5 quality. ExpertLLaMA achieved 96% of ChatGPT.
- **PRISM study (March 2026):** Expert personas improve alignment tasks but *damage* factual accuracy (MMLU: 71.6% → 66.3% with long persona).
- **Applied:** Concise, task-scoped role in system prompt. No elaborate backstory. "You generate supplementary execution context" — structural, not fictional.

### ReAct (Reasoning + Acting)
- Yao et al. (2022): ReAct outperformed imitation/RL by 34% on ALFWorld, 10% on WebShop.
- **Applied:** Workflow section uses ReAct pattern: Investigate → Plan → Implement → Verify. Built into Claude Code's architecture; we reinforce it.

### RE2 Re-Reading
- Xu et al. (EMNLP 2024): Re-reading the question improves reasoning +2-5 pts across benchmarks.
- **Applied:** Local LLM generates `<intent>` section restating the request. Claude reads casual prompt (pass 1), then precise restatement (pass 2).

### Structured Prompt Frameworks
- CO-STAR (Context, Objective, Style, Tone, Audience, Response)
- TIDD-EC (Task, Instructions, Do, Don't, Examples, Constraints)
- **Applied:** Synthesized into our XML template: `<intent>`, `<applicable_rules>`, `<constraints>`, `<analysis>`, `<anti_patterns>`, `<clarification_needed>`.

---

## Local LLM Selection

| Model | Parameters | RAM | HumanEval | Why |
|-------|-----------|-----|-----------|-----|
| **Qwen 2.5 Coder 14B** (primary) | 14B | 16 GB | 85% | Best code-aware local model. Strong structured output. Apache 2.0 |
| **Qwen 3 7B** (fallback) | 7B | 8 GB | 76% | Best performance-per-GB under 8B |
| **Phi-4 Mini** (low-RAM) | 3.8B | 8 GB | 65% | Only viable option for 8GB machines |

**Source:** Benchmarks from Ollama + HuggingFace leaderboard as of 2025-Q4.

---

## Key Insight: Injection vs Replacement

The most important design finding: Claude's `additionalContext` exploits **recency bias** — LLMs weight later context more heavily. By appending structured context *after* the user's casual prompt, we steer Claude without replacing or hiding the user's intent. The user types casually; Claude executes thoroughly.

This is more effective than prompt replacement because:
1. Claude sees the original intent (no information loss)
2. The structured context is at the end (highest attention weight)
3. `suppressOutput: true` keeps the machinery invisible to the user
