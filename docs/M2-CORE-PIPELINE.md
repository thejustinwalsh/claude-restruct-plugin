# M2: Core Pipeline Hardening

## Goal

Make the refinement pipeline production-ready with proper error handling, timeouts, structured logging, and resilient Ollama communication.

## Depends On

M1 (Hook Protocol) — need the correct I/O contract before hardening the pipeline that uses it.

---

## Tasks

### 2.1 — Ollama Client Robustness

**What:** Harden `ollama/client.go` for real-world conditions.

**Requirements:**
- **Connection timeout:** Configurable (default 2s). If Ollama doesn't respond within timeout, fail fast.
- **Request timeout:** Configurable (default 10s). Total time for refinement response. Kill request if exceeded.
- **Retry with backoff:** Single retry on transient errors (connection reset, 503). No retry on 4xx.
- **Model availability check:** Before sending chat request, verify the model is loaded. If not, attempt a load (with configurable keep_alive).
- **Version check:** Warn (don't block) if Ollama version is below minimum (0.18.0).
- **Streaming vs non-streaming:** Currently uses `stream: false`. Evaluate whether streaming with early termination would help latency. Decide and document.

**Current gaps in `client.go`:**
- Uses `http.DefaultClient` (no timeout configured)
- No retry logic
- No model pre-check before chat
- PullModel uses raw curl-style HTTP; should use proper Go HTTP client

### 2.2 — Pipeline Error Handling

**What:** Make `pipeline/pipeline.go` handle every failure mode gracefully.

**Failure modes to handle:**
| Failure | Current Behavior | Required Behavior |
|---------|-----------------|-------------------|
| Ollama not running | Unclear error | Log warning, return raw prompt |
| Model not pulled | API error | Log instruction to run `restruct model pull`, return raw prompt |
| Ollama timeout | Hangs | Return raw prompt after timeout, log latency |
| Malformed LLM response | Pass through garbage | Validate output has expected structure, fall back to raw |
| Empty LLM response | Pass through empty | Return raw prompt, log warning |
| Rules files missing | Silent empty rules | Log which files were checked, continue with empty rules |
| Git context fails | Silent empty context | Log warning, continue without git context |

**Key principle:** The pipeline NEVER blocks the user. Every failure returns the raw prompt.

### 2.3 — Output Validation

**What:** Validate the LLM's refined output before returning it.

**Validation rules:**
- Output must not be empty
- Output must not be shorter than the original prompt (likely a failure)
- Output must contain expected structural markers (tags or headers, depending on M3 format decision)
- Output must not contain the system prompt or meta-instructions (LLM leak detection)
- If validation fails, return raw prompt with a warning

### 2.4 — Structured Logging

**What:** Add consistent, leveled logging throughout the pipeline.

**Requirements:**
- Use `log/slog` (Go stdlib, no external dependency)
- Levels: `DEBUG` (verbose pipeline steps), `INFO` (refinement success/skip), `WARN` (degraded operation), `ERROR` (failures)
- Default level: `WARN` (only show problems)
- `--verbose` flag sets `DEBUG`
- All log output goes to stderr (stdout is reserved for hook JSON output)
- Include timing: log how long each pipeline stage takes

**Log points:**
```
DEBUG: Loading rules from [agents.md, CLAUDE.md] (found: agents.md)
DEBUG: Rules hash: abc123
DEBUG: Cache miss for prompt hash: def456
DEBUG: Gathering git context...
DEBUG: Git context: branch=main, 5 recent commits
INFO:  Refining prompt (47 words) via qwen2.5-coder:14b
DEBUG: Ollama response received in 1.2s (834 tokens)
INFO:  Refinement complete. Output: 312 words, validated OK.
WARN:  Ollama not available, passing through raw prompt
ERROR: Ollama returned malformed response: <details>
```

### 2.5 — Context Timeout Propagation

**What:** Ensure the Go `context.Context` timeout flows through the entire pipeline.

**Requirements:**
- `refine` command creates a context with the configured timeout
- Context is passed to: rules loader, git context, cache lookup, Ollama chat
- If any step exceeds its budget, cancel and degrade gracefully
- Total timeout budget: configurable, default 10s
- Individual stage budgets: Ollama gets 80% of remaining budget, everything else shares 20%

### 2.6 — Streaming Ollama Client

**What:** Add a streaming mode to the Ollama client that yields tokens as they arrive.

**Why:** The dashboard (M4) needs to show refinement progress in real-time. Streaming also enables earlier timeout detection — if no tokens arrive within the first 3s, abort early rather than waiting for the full timeout.

**Implementation:**
```go
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
    // POST /api/chat with stream: true
    // Read NDJSON response line-by-line
    // Send each chunk to returned channel
    // Close channel on done: true or error
}

type StreamChunk struct {
    Content string // token text
    Done    bool   // final chunk
    Error   error  // non-nil on failure
}
```

**Requirements:**
- The existing non-streaming `Chat()` method remains for simple use cases
- Streaming client respects the same timeouts and context cancellation
- Token chunks are forwarded to an optional `TokenSink` interface (used by M4's SSE hub)
- Final response is assembled from chunks and validated the same way as non-streaming
- If streaming fails mid-response, return what we have if it's structurally valid, otherwise fall back to raw prompt

**Backward compatibility:** Pipeline uses streaming by default. If the caller doesn't provide a `TokenSink`, tokens are silently consumed and only the final result is returned (behaves like non-streaming).

### 2.7 — Pipeline Tests

**What:** Comprehensive unit tests for the pipeline.

**Test cases:**
- Happy path: rules exist, Ollama responds, cache misses then hits
- Ollama unavailable: returns raw prompt
- Ollama timeout: returns raw prompt within timeout budget
- Malformed response: returns raw prompt
- Empty rules: refinement still works with generic constraints
- Short prompt bypass: prompts under min_words pass through unchanged
- Cache hit: returns cached result without calling Ollama
- Output validation: catches empty, too-short, and leaked system prompts

**Approach:** Use interfaces/mocks for Ollama client to test pipeline logic without a running server.

---

## Acceptance Criteria

- [ ] Ollama client has configurable timeouts, retry, and model pre-check
- [ ] Every pipeline failure mode returns raw prompt (never blocks)
- [ ] LLM output is validated before returning
- [ ] Structured logging with slog, all to stderr
- [ ] Context timeout propagates through all stages
- [ ] Streaming Ollama client with TokenSink interface
- [ ] Pipeline test coverage >80%

## Files Modified

- `cli/internal/ollama/client.go` — timeout, retry, model check
- `cli/internal/pipeline/pipeline.go` — error handling, validation, logging
- `cli/internal/pipeline/pipeline_test.go` — comprehensive tests
- `cli/cmd/refine.go` — context timeout setup
- `cli/internal/config/config.go` — timeout config fields if missing

## Risk

**Medium.** Existing code is structurally sound but lacks production error handling. This is straightforward hardening work.
