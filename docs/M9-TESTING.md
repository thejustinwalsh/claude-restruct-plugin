# M8: Testing & Calibration

## Goal

Build a comprehensive testing and evaluation framework that measures refinement quality, establishes baselines, and enables data-driven prompt optimization.

## Depends On

M3 (Prompt Engine) — need finalized output format before calibrating quality.

---

## Tasks

### 8.1 — Unit Test Coverage

**What:** Achieve >80% test coverage across all packages.

**Current state:** 2 test files (cache/store_test.go, pipeline/pipeline_test.go) with minimal coverage.

**Package-by-package requirements:**

| Package | Key Test Cases |
|---------|---------------|
| `cache/store` | Round-trip, TTL expiration, LRU eviction, normalization, concurrent access |
| `config` | Defaults, file loading, env override, flag override, validation |
| `git/context` | In-repo, not-in-repo, no git, changed files detection |
| `hook/protocol` | Parse valid input, parse malformed input, write all output types |
| `hook/install` | Fresh install, existing hooks (merge), global install, idempotency |
| `ollama/client` | Available/unavailable, timeout, retry, model check (use httptest) |
| `pipeline` | Full happy path, every degradation path, passthrough detection |
| `prompt/builder` | Template rendering, rules injection, token budget |
| `rules/loader` | Single file, multiple files, hierarchical, filtering, missing files |

**Approach:**
- Use interfaces and mocks for external dependencies (Ollama, filesystem, git)
- Use `httptest.Server` for Ollama client tests
- Use `t.TempDir()` for cache and rules tests

### 8.2 — Integration Test Suite

**What:** End-to-end tests that run the actual binary with a mock Ollama server.

**Test harness:**
1. Start a mock HTTP server that mimics Ollama's `/api/chat` endpoint
2. Set `RESTRUCT_OLLAMA_URL` to point at the mock
3. Feed test hook input via stdin to `restruct refine`
4. Assert on stdout JSON output

**Integration test cases:**
- Happy path: prompt in → refined prompt out
- Ollama down: prompt in → passthrough out
- Ollama slow: prompt in → timeout → passthrough out
- Short prompt: bypass refinement
- Malformed Ollama response: passthrough
- Cache hit: second identical prompt returns cached result without Ollama call

### 8.3 — Prompt Quality Evaluation Framework

**What:** A framework for evaluating refinement quality using LLM-as-judge.

**Design:**
```
restruct eval [--corpus <path>] [--judge <model>]
```

**Evaluation corpus:** `cli/testdata/eval_corpus.json`
```json
[
  {
    "id": "auth-bug-vague",
    "raw_prompt": "fix the auth bug",
    "rules": "...",
    "quality_criteria": {
      "preserves_intent": true,
      "adds_investigation_step": true,
      "includes_uncertainty_directive": true,
      "embeds_relevant_rules": true,
      "not_over_verbose": true
    }
  }
]
```

**Evaluation modes:**
1. **Structural validation:** Check that output contains required sections (objective, workflow, uncertainty, etc.). No LLM needed. Fast.
2. **LLM-as-judge:** Send the refined prompt to a judge LLM (Claude or local model) with the quality criteria. Judge scores each criterion as pass/fail with reasoning.
3. **A/B comparison:** Given two refined versions of the same prompt, judge which is better. Useful for comparing system prompt iterations.

### 8.4 — Evaluation Corpus Creation

**What:** Build a diverse corpus of 30+ test prompts with expected quality criteria.

**Categories:**
| Category | Count | Examples |
|----------|-------|---------|
| Bug fixes (vague) | 5 | "fix the auth bug", "login is broken" |
| Bug fixes (specific) | 5 | "the JWT refresh token expires after 5min instead of 24h" |
| Feature requests | 5 | "add dark mode", "add CSV export to the reports page" |
| Refactoring | 3 | "refactor the database layer", "clean up the auth module" |
| Investigation | 3 | "why is the app slow", "figure out why tests are flaky" |
| Documentation | 2 | "update the README", "add API docs" |
| Follow-ups | 3 | "yes", "try option 2", "actually make it async" |
| Edge cases | 4 | empty, very long, contains code, already structured |

Each entry includes: raw prompt, sample rules file, expected quality criteria.

### 8.5 — Baseline Measurement

**What:** Run the evaluation corpus against the current system and record baseline scores.

**Metrics to capture:**
- **Structural compliance:** % of outputs that contain all required sections
- **Intent preservation:** % judged as preserving the user's original intent
- **Rule embedding:** % that include relevant rules from the provided rules file
- **Uncertainty handling:** % that correctly flag ambiguity vs. resolve clear requests
- **Verbosity ratio:** refined_words / raw_words (target: 3-8x, not 20x)
- **Latency:** p50, p90, p99 refinement times

**Output:** `docs/BASELINE.md` with scores and analysis. This becomes the benchmark for M3 prompt optimization and M9 self-improvement.

### 8.6 — Regression Test Gate

**What:** Prevent system prompt changes from degrading quality.

**Implementation:**
- `make eval` runs structural validation (fast, no LLM needed)
- CI runs `make eval` on every PR that touches `system_prompt.tmpl` or `builder.go`
- Structural compliance must be >= baseline. If it drops, CI fails.
- Full LLM-as-judge evaluation runs manually (`make eval-full`) before releases.

---

## Acceptance Criteria

- [ ] Unit test coverage >80% across all packages
- [ ] Integration tests pass with mock Ollama server
- [ ] Evaluation corpus of 30+ prompts with quality criteria
- [ ] `restruct eval` command runs structural and LLM-based evaluation
- [ ] Baseline scores documented
- [ ] CI gate prevents regression on structural compliance

## Files Modified

- All `*_test.go` files — expanded test coverage
- New: `cli/cmd/eval.go` — evaluation command
- New: `cli/internal/eval/` — evaluation framework (structural + LLM judge)
- New: `cli/testdata/eval_corpus.json` — test corpus
- New: `docs/BASELINE.md` — baseline scores
- `.github/workflows/build.yml` — add eval gate

## Risk

**Medium.** The LLM-as-judge evaluation is inherently subjective. Mitigate by:
1. Keeping structural checks objective (section presence, word count ratios)
2. Using the LLM judge only for nuanced criteria (intent preservation)
3. Running judge evaluation multiple times and averaging scores
