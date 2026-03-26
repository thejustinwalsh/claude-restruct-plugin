# Meta-Prompt Hook System: Implementation Plan

## Transforming Conversational Prompts into Structured Agent Instructions via Local LLM Preprocessing

---

## 1. Research Summary: Prompting Techniques That Keep Agents on Task

### 1.1 Chain-of-Thought (CoT) Prompting

**What it is:** Instructing the model to articulate intermediate reasoning steps before arriving at a final answer, rather than jumping straight to output.

**Evidence of effectiveness:**
- Wei et al. (2022) demonstrated that CoT prompting improves performance across arithmetic, commonsense, and symbolic reasoning tasks. A 540B-parameter model with just eight CoT exemplars achieved state-of-the-art on the GSM8K math benchmark, surpassing fine-tuned models.
- Self-Consistency (Wang et al., 2022) extends CoT by sampling multiple reasoning paths and taking a majority vote. This boosted GSM8K by +17.9%, SVAMP by +11.0%, AQuA by +12.2%, StrategyQA by +6.4%, and ARC-challenge by +3.9%.

**Nuance (Wharton 2025 report):** The value of explicit CoT is *decreasing* for reasoning-native models (e.g., Claude with extended thinking, o1/o3). Non-reasoning models still benefit modestly, but reasoning models gain only marginal benefits at 20–80% increased latency. **Takeaway for our system:** Use CoT scaffolding in the *local* refinement LLM (which is smaller and benefits more), but don't force redundant step-by-step instructions onto Claude, which has built-in reasoning.

**Prompt pattern:**
```
Before answering, break the problem into steps:
1. Identify what the user is actually asking for
2. Determine which files/systems are affected
3. Plan the changes before writing code
4. Validate the plan against project rules
```

### 1.2 Expert Role / Persona Prompting

**What it is:** Assigning the model an expert identity (e.g., "You are a senior backend engineer") to steer tone, structure, and domain focus.

**Evidence — mixed results:**
- ExpertPrompting (Xu et al., 2023): Auto-generating detailed expert descriptions per instruction improved GPT-3.5 answer quality significantly. ExpertLLaMA achieved 96% of ChatGPT's capability on GPT-4-based evaluation.
- Multi-Expert Prompting (Do Xuan Long et al., 2024): Simulating multiple experts and aggregating responses outperformed ExpertPrompting by 8.69% on truthfulness benchmarks with ChatGPT.
- **PRISM study (March 2026):** Expert personas improve alignment-dependent tasks (writing +0.40, extraction +0.65, STEM +0.60) but *damage* factual accuracy on MMLU (71.6% baseline → 68.0% with minimum persona, 66.3% with long persona). The persona activates "instruction-following mode" at the expense of factual recall.

**Takeaway for our system:** Use role prompting for *structural and stylistic* steering (how to format output, what tone to use, what workflow to follow), but avoid overloading the persona with domain claims that could suppress factual accuracy. Use concise, task-scoped roles — not elaborate backstories.

**Prompt pattern:**
```
You are a code review specialist working within this repository's conventions.
Your role: ensure all changes follow the project's architecture patterns
and coding standards defined in AGENTS.md.
```

### 1.3 ReAct (Reasoning + Acting)

**What it is:** A Thought → Action → Observation loop where the model reasons about what to do, takes an action (tool call, search), observes the result, and iterates.

**Evidence of effectiveness:**
- Yao et al. (2022): ReAct outperformed imitation and reinforcement learning methods by 34% on ALFWorld and 10% on WebShop. On HotpotQA and Fever, it overcame hallucination and error propagation issues compared to pure CoT.
- ReAct combined with CoT (using internal knowledge + external tool results) was found to be the best overall approach.
- ReWOO (2023) improved on ReAct with 5× token efficiency and 4% accuracy improvement on HotpotQA, plus 64% token reduction with 4.4% absolute accuracy gain.

**Takeaway for our system:** ReAct is ideal for the *agent's operating mode*, not the prompt refinement step. Our meta-prompt should instruct the agent to use ReAct-style reasoning: think before acting, observe results, iterate. This is built into Claude Code's architecture already — our job is to reinforce it.

**Prompt pattern:**
```
Before making changes:
THINK: What is the root cause? What files are involved?
PLAN: What is the minimal change set?
ACT: Implement the change.
VERIFY: Run tests. Check for regressions.
If verification fails, return to THINK — do not guess at a fix.
```

### 1.4 Tree of Thoughts (ToT)

**What it is:** Extends CoT by exploring multiple reasoning branches (like BFS/DFS over solution paths), evaluating each, and pruning bad paths before committing.

**Evidence of effectiveness:**
- Yao et al. (2023): On Game of 24, standard prompting achieved 7.3% success, CoT achieved 4.0%, but ToT achieved 74%. Massive gains on problems requiring exploration and backtracking.

**Takeaway for our system:** ToT is computationally expensive (multiple LLM calls per branch). For our meta-prompt hook, we use a *lightweight ToT-inspired pattern*: generate 2–3 candidate interpretations of the user's intent, evaluate which aligns best with the repo context, then commit to one. This happens in the local LLM, not in Claude.

### 1.5 Self-Consistency

**What it is:** Generate multiple diverse reasoning paths via few-shot CoT, then select the most consistent answer via majority vote.

**Evidence:** +17.9% on GSM8K, +11.0% on SVAMP, +12.2% on AQuA over standard CoT. Benefits scale with model size — up to +23% for LaMDA-137B and GPT-3.

**Takeaway for our system:** The local LLM can use a lightweight self-consistency check: generate the refined prompt, then verify it against the project rules (agents.md) in a second pass. If contradictions are found, regenerate.

### 1.6 Meta-Prompting & Self-Refinement

**What it is:** Using an LLM to craft, evaluate, and improve prompts for another LLM (or itself).

**Evidence:**
- Self-Refine (Madaan et al., NeurIPS 2023): Outputs generated with iterative self-refinement were preferred by humans ~20% more on average across 7 task types.
- Arize's Prompt Learning applied to Claude Code: By using LLM-as-evaluator feedback to iteratively optimize CLAUDE.md instructions, they improved SWE-Bench Lite performance purely through better prompts — no fine-tuning, no architecture changes.

**Takeaway for our system:** This is the core philosophy of our entire system. A local LLM refines the user's conversational prompt into a structured, rules-aware prompt before Claude ever sees it.

### 1.7 Structured Prompt Frameworks

Several frameworks codify best practices into repeatable templates:

| Framework | Components | Best For |
|-----------|-----------|----------|
| **CO-STAR** | Context, Objective, Style, Tone, Audience, Response | General-purpose prompt structuring |
| **RISEN** | Role, Instructions, Steps, End goal, Narrowing | Task decomposition |
| **TIDD-EC** | Task, Instructions, Do, Don't, Examples, Constraints | Guardrail-heavy tasks |
| **RTF** | Role, Task, Format | Quick role-scoped tasks |

**Takeaway:** Our meta-prompt template will synthesize elements from CO-STAR and TIDD-EC, adapted for developer workflow: Context (repo state), Objective (user intent), Constraints (agents.md rules), Format (expected output structure), Anti-patterns (what NOT to do).

---

## 2. Research Summary: Best Local LLMs for Prompt Refinement

The local LLM serves a specific purpose: take a conversational user prompt + repo rules, and output a structured prompt. It does NOT need to write code, run tools, or reason about complex architectures. It needs strong instruction-following, good structured output, and fast inference.

### 2.1 Recommended Models (Ranked by Fit)

| Model | Parameters | RAM Required | Why It Fits | HumanEval | MMLU | License |
|-------|-----------|-------------|-------------|-----------|------|---------|
| **Qwen 2.5 Coder 14B** | 14B | 16 GB | Top-rated local coding model in 2026. Understands code context, strong structured output. 85% HumanEval. | 85% | 74% | Apache 2.0 |
| **Qwen 3 7B** | 7B | 8 GB | Highest HumanEval (76%) under 8B. Excellent multilingual + code understanding. | 76% | 71% | Apache 2.0 |
| **DeepSeek R1 14B** | 14B | 16 GB | Chain-of-thought reasoning native. Excels at structured analysis and debugging context. | 72% | 69% | Permissive |
| **Llama 3.3 8B** | 8B | 8 GB | Best all-around balance. 73% MMLU, 72.6% HumanEval at Q4. Largest community ecosystem. | 72.6% | 73% | Llama License |
| **Phi-4 Mini** | 3.8B | 8 GB | Only viable option for 8GB machines. 80.4% on MATH benchmark — exceptional reasoning per GB. | 65% | 70% | MIT |
| **Mistral Small 3 7B** | 7B | 8 GB | Fastest tokens/sec on mid-range hardware. Good for latency-critical preprocessing. | 68% | 70% | Apache 2.0 |

### 2.2 Selection Criteria

For the meta-prompt hook system, the ideal model needs:

1. **Fast inference** — This runs on every prompt before Claude sees it. Latency budget: <2 seconds.
2. **Strong instruction following** — Must reliably output structured prompts in the expected format.
3. **Code context awareness** — Must understand repo conventions, file paths, architectural patterns.
4. **Small footprint** — Runs alongside the developer's main workload.
5. **Structured output** — Reliably produces XML/markdown-formatted prompts without drift.

### 2.3 Primary Recommendation

**Qwen 2.5 Coder 14B** (via Ollama) for developers with 16GB+ RAM. It leads on code-aware tasks, follows structured output instructions reliably, and is Apache 2.0 licensed.

**Fallback: Qwen 3 7B** for 8GB machines — best performance-per-GB ratio with strong code understanding.

### 2.4 Runtime: Ollama

```bash
# Install the recommended model
ollama pull qwen2.5-coder:14b

# Or the lightweight alternative
ollama pull qwen3:7b

# Verify it's running
curl http://localhost:11434/api/chat -d '{
  "model": "qwen2.5-coder:14b",
  "messages": [{"role": "user", "content": "Hello"}]
}'
```

Ollama provides a local OpenAI-compatible API on `localhost:11434`, making integration straightforward with any hook system.

---

## 3. System Design: Meta-Prompt Hook Architecture

### 3.1 Core Principle

> The user's raw conversational prompt **never enters Claude's context window**. A local LLM intercepts it, reads the project's `agents.md` (rules, patterns, constraints), and produces a structured prompt that Claude receives as if the user wrote it that way.

### 3.2 Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│                    DEVELOPER                            │
│                                                         │
│   "hey can you fix the auth bug where tokens expire     │
│    too fast"                                            │
└──────────────┬──────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│              META-PROMPT HOOK (PreToolUse)               │
│                                                          │
│  1. Intercept raw user prompt                            │
│  2. Load agents.md + repo context                        │
│  3. Send to LOCAL LLM (Ollama)                           │
│  4. Receive structured prompt                            │
│  5. Replace user message with structured version         │
│                                                          │
│  ┌────────────────────────────────────────────────────┐  │
│  │          LOCAL LLM (Qwen 2.5 Coder 14B)           │  │
│  │                                                    │  │
│  │  System Prompt:                                    │  │
│  │  "You are a prompt architect. Given a developer's  │  │
│  │   casual request and the project's rules file,     │  │
│  │   produce a structured prompt that will guide      │  │
│  │   Claude to follow the project's patterns."        │  │
│  │                                                    │  │
│  │  Input:                                            │  │
│  │  - Raw user prompt                                 │  │
│  │  - agents.md contents                              │  │
│  │  - Recent git context (optional)                   │  │
│  │                                                    │  │
│  │  Output:                                           │  │
│  │  - Structured prompt with:                         │  │
│  │    • Objective                                     │  │
│  │    • Constraints from agents.md                    │  │
│  │    • Expected workflow                             │  │
│  │    • Anti-patterns to avoid                        │  │
│  │    • Uncertainty directive                         │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│                    CLAUDE (Agent)                         │
│                                                          │
│  Receives ONLY the structured prompt.                    │
│  Never sees the raw conversational input.                │
│  Follows project rules because they're embedded          │
│  directly in the prompt it receives.                     │
│                                                          │
│  Key behaviors enforced:                                 │
│  - No probabilistic guessing                             │
│  - Ask user when uncertain                               │
│  - Follow repo patterns                                  │
│  - Verify before committing                              │
└──────────────────────────────────────────────────────────┘
```

### 3.3 Hook Integration Points

For **Claude Code**, this integrates via the hooks system:

```jsonc
// .claude/settings.json
{
  "hooks": {
    "UserPrompt": [
      {
        "matcher": ".*",  // Intercept all user prompts
        "hooks": [
          {
            "type": "command",
            "command": "python3 .claude/hooks/meta-prompt-refine.py"
          }
        ]
      }
    ]
  }
}
```

For **Claude.ai / API**, this integrates as a middleware layer:

```
User Input → meta-prompt-refine.py → Anthropic API (messages endpoint)
```

### 3.4 The Meta-Prompt Refinement Pipeline

```python
# .claude/hooks/meta-prompt-refine.py (conceptual)

import json
import sys
import subprocess
from pathlib import Path

def load_project_rules():
    """Load agents.md and any CLAUDE.md rules."""
    rules = ""
    for rules_file in ["agents.md", "CLAUDE.md", ".claude/rules.md"]:
        path = Path(rules_file)
        if path.exists():
            rules += f"\n\n## Rules from {rules_file}\n{path.read_text()}"
    return rules

def get_git_context():
    """Get recent git state for context."""
    try:
        branch = subprocess.check_output(
            ["git", "branch", "--show-current"], text=True
        ).strip()
        recent = subprocess.check_output(
            ["git", "log", "--oneline", "-5"], text=True
        ).strip()
        diff_stat = subprocess.check_output(
            ["git", "diff", "--stat", "HEAD~1"], text=True
        ).strip()
        return f"Branch: {branch}\nRecent commits:\n{recent}\nRecent changes:\n{diff_stat}"
    except Exception:
        return ""

def refine_prompt(raw_prompt: str, rules: str, git_context: str) -> str:
    """Send to local Ollama model for refinement."""
    system_prompt = """You are a Prompt Architect for a software development team.

Your job: Transform a developer's casual request into a structured prompt
that will guide an AI coding agent (Claude) to produce excellent results.

## Your Process (Chain of Thought)
1. PARSE the developer's intent — what do they actually want done?
2. IDENTIFY which project rules are relevant to this request
3. DETECT ambiguity — flag anything the agent should ask about rather than guess
4. STRUCTURE the output using the template below

## Critical Directives to Embed
- The agent must NEVER guess when uncertain. It must ask the user.
- The agent must NEVER attempt a fix without understanding the root cause.
- The agent must follow the project's architectural patterns exactly.
- The agent must present a plan before implementing.

## Output Template (produce ONLY this, no preamble)
<structured_prompt>
<objective>[Clear, specific statement of what needs to be done]</objective>

<context>[Relevant repo state, branch, recent changes]</context>

<constraints>
[Extracted rules from agents.md that apply to this task]
</constraints>

<workflow>
1. [First step — usually: investigate/understand]
2. [Second step — usually: plan and present to user]
3. [Third step — implement]
4. [Fourth step — verify]
</workflow>

<uncertainty_protocol>
If you encounter any of the following, STOP and ask the user:
- Multiple valid approaches with different tradeoffs
- Missing context about business logic or requirements
- Ambiguous scope (could mean several different things)
- Risk of breaking changes to existing functionality
Do NOT make probabilistic guesses. Ask.
</uncertainty_protocol>

<anti_patterns>
- Do NOT [specific anti-patterns from agents.md]
- Do NOT guess at implementations when the requirements are unclear
- Do NOT make changes outside the scope of this request
</anti_patterns>
</structured_prompt>"""

    user_content = f"""## Developer's Request
{raw_prompt}

## Project Rules (agents.md)
{rules}

## Current Repository State
{git_context}

Transform this into a structured prompt. Output ONLY the <structured_prompt> block."""

    result = subprocess.run(
        ["curl", "-s", "http://localhost:11434/api/chat", "-d", json.dumps({
            "model": "qwen2.5-coder:14b",
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_content}
            ],
            "stream": False,
            "options": {"temperature": 0.3, "num_predict": 2048}
        })],
        capture_output=True, text=True
    )

    response = json.loads(result.stdout)
    return response["message"]["content"]

def main():
    # Read hook input from stdin
    hook_input = json.load(sys.stdin)
    raw_prompt = hook_input.get("prompt", "")

    # Skip refinement for very short/simple prompts
    if len(raw_prompt.split()) < 5:
        json.dump({"ok": True}, sys.stdout)
        return

    rules = load_project_rules()
    git_context = get_git_context()

    refined = refine_prompt(raw_prompt, rules, git_context)

    # Output the refined prompt to replace the original
    json.dump({
        "ok": True,
        "refined_prompt": refined
    }, sys.stdout)

if __name__ == "__main__":
    main()
```

---

## 4. The Local LLM's System Prompt (Complete)

This is the core prompt that powers the refinement. It synthesizes CoT, role prompting, TIDD-EC structure, and self-consistency into a single instruction set.

```markdown
# System Prompt for Meta-Prompt Refinement LLM

You are a **Prompt Architect** — a specialist in transforming casual developer
requests into structured, high-quality prompts for AI coding agents.

## Your Operating Principles

### 1. Chain of Thought (mandatory internal process)
Before producing output, reason through these steps internally:
- What is the developer ACTUALLY asking for? (Parse intent)
- What could go wrong if the agent misinterprets this? (Risk analysis)
- Which project rules constrain how this should be done? (Rule extraction)
- What information is MISSING that the agent shouldn't guess about? (Gap detection)

### 2. Uncertainty Preservation
If the developer's request is ambiguous, DO NOT resolve the ambiguity yourself.
Instead, embed an explicit instruction for the agent to ask the developer.
The goal is to prevent the agent from making probabilistic guesses.

### 3. Rule Reinforcement
Always extract and embed relevant rules from the project's agents.md file.
The agent's prompt MUST contain the specific constraints that apply.
Do not summarize rules — include them verbatim when they're relevant.

### 4. Anti-Pattern Injection
Based on the project rules, identify what the agent should NOT do
and make these explicit. Negative instructions ("do NOT") are as
important as positive ones.

### 5. Workflow Enforcement
Every refined prompt must include a workflow that follows this pattern:
  Investigate → Plan → Present Plan → Implement → Verify
The agent must NEVER skip straight to implementation.

## Output Format
Produce ONLY the structured prompt. No explanations, no preamble,
no commentary. The output goes directly to the agent.

## Temperature Guidance
Use low temperature (0.2-0.3). Consistency matters more than creativity
for prompt refinement.
```

---

## 5. Implementation Phases

### Phase 1: Foundation (Week 1)

| Task | Details |
|------|---------|
| Install Ollama | `curl -fsSL https://ollama.com/install.sh \| sh` |
| Pull model | `ollama pull qwen2.5-coder:14b` |
| Create hook script | `.claude/hooks/meta-prompt-refine.py` |
| Create agents.md template | Define project rules, patterns, anti-patterns |
| Wire up hook | Add to `.claude/settings.json` |
| Test with 10 sample prompts | Verify refined output quality |

### Phase 2: Calibration (Week 2)

| Task | Details |
|------|---------|
| Collect prompt pairs | Log raw → refined → Claude output for 20+ real tasks |
| Evaluate quality | Score refined prompts on: specificity, rule coverage, clarity |
| Tune system prompt | Iterate on the refinement LLM's instructions based on failures |
| Add bypass mechanism | `--raw` flag to skip refinement for advanced users |
| Add caching | Cache refined prompts for identical inputs to reduce latency |

### Phase 3: Self-Improvement Loop (Week 3+)

| Task | Details |
|------|---------|
| LLM-as-evaluator | Use Claude to evaluate whether refined prompts led to good outcomes |
| Feedback integration | Feed evaluation results back into the refinement prompt (Prompt Learning) |
| agents.md auto-update | When patterns emerge from evaluations, suggest rules updates |
| Metrics dashboard | Track: refinement latency, Claude accuracy, user override rate |

---

## 6. Key Design Decisions

### 6.1 Why the Original Prompt Must Not Enter Claude's Context

When Claude sees a casual prompt like "fix the auth bug," it activates a probabilistic completion mode — predicting the most likely "fix" without systematic analysis. By replacing this with a structured prompt that says "investigate the authentication token expiration issue, identify root cause, present plan before implementing," we shift Claude from *guessing* to *reasoning*.

The research supports this: structured prompts with explicit constraints consistently outperform casual instructions, and the gap widens with task complexity.

### 6.2 Why Local LLM, Not Claude Itself

- **Cost:** Refinement via Claude API would double token costs on every interaction.
- **Latency:** Local inference at 40–60 tok/s on a 7B model adds <1s. API round-trip adds 2–5s.
- **Privacy:** The raw prompt and repo rules never leave the developer's machine.
- **Independence:** The refinement layer works even when the API is down or rate-limited.

### 6.3 Why Qwen 2.5 Coder Specifically

The refinement task is a structured text transformation that benefits from code awareness (understanding file paths, function names, architectural patterns in agents.md). Qwen 2.5 Coder 14B scores 85% on HumanEval and has strong structured output adherence — both critical for producing reliable prompt templates. Its Apache 2.0 license allows unrestricted commercial use.

### 6.4 Handling Edge Cases

| Scenario | Behavior |
|----------|----------|
| Prompt is already well-structured | Local LLM detects structure, passes through with minimal modification |
| Prompt is too vague to refine | Output includes explicit "ask user for clarification" directives |
| Ollama is not running | Hook fails gracefully, passes raw prompt through with a warning |
| agents.md doesn't exist | Refinement uses generic best-practice constraints |
| Prompt is a follow-up in conversation | Include conversation summary context in refinement input |

---

## 7. Example Transformation

### Raw User Prompt
```
hey can you fix the auth bug where tokens expire too fast
```

### After Meta-Prompt Refinement
```xml
<structured_prompt>
<objective>
Investigate and fix a bug in the authentication system where tokens are
expiring faster than expected. Determine whether this is a configuration
issue (token TTL), a clock skew problem, or a logic error in token
validation/refresh.
</objective>

<context>
Branch: feature/auth-improvements
Recent commits: refactored middleware, updated JWT library
</context>

<constraints>
- All authentication changes must include unit tests (agents.md §3.2)
- JWT configuration lives in config/auth.ts — do not hardcode values (agents.md §4.1)
- Token refresh logic must use the existing RefreshTokenService (agents.md §5.3)
- All auth changes require security review before merge (agents.md §7.0)
</constraints>

<workflow>
1. INVESTIGATE: Read the current token configuration and expiration logic.
   Identify where TTL is set and how token validation checks expiration.
2. REPRODUCE: Determine how to reproduce the "too fast" expiration.
   Ask the user: What is the expected TTL? What TTL are they observing?
3. PLAN: Present your findings and proposed fix before implementing.
4. IMPLEMENT: Make the fix following project patterns.
5. VERIFY: Run existing auth tests. Add a test for the specific expiration case.
</workflow>

<uncertainty_protocol>
STOP and ask the user before proceeding if:
- You cannot determine the intended token TTL from config
- Multiple token types exist and it's unclear which one is affected
- The fix might affect other services that depend on token expiration
- You're unsure whether this is a backend or frontend issue
</uncertainty_protocol>

<anti_patterns>
- Do NOT hardcode token expiration values
- Do NOT modify the token refresh flow without understanding the full chain
- Do NOT skip writing tests for auth-related changes
- Do NOT guess at the intended TTL — ask the user
</anti_patterns>
</structured_prompt>
```

---

## 8. Measuring Success

| Metric | Baseline (no refinement) | Target (with refinement) |
|--------|--------------------------|--------------------------|
| Agent asks before guessing | ~20% of ambiguous cases | >80% |
| First-attempt success rate | ~50% | >75% |
| Project rule violations | ~30% of changes | <5% |
| User prompt iterations needed | 3–5 per task | 1–2 per task |
| Refinement latency overhead | 0s | <2s |

---

## 9. Future Extensions

1. **Conversation-aware refinement:** Feed the last N turns of conversation into the local LLM for better follow-up prompt refinement.
2. **Per-file rule injection:** When the user mentions specific files, extract file-level conventions (e.g., from inline comments or module-level docs) into the refined prompt.
3. **Prompt library:** Build a cache of high-performing refined prompts that can be pattern-matched and reused for common request types.
4. **Multi-agent routing:** The refinement LLM could classify the request type and route to different agent configurations (e.g., "this is a refactoring task" → use the refactoring agent template).
5. **Self-improving rules:** Use Claude's evaluation of its own outputs to suggest new rules for agents.md, creating a feedback loop that continuously improves prompt quality.
