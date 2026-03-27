# M2: Core Pipeline Hardening

## Goal

Make the refinement pipeline production-ready with proper error handling, resilient Ollama communication, structured logging, and streaming support. The pipeline should be reliable and thorough — not fast at the cost of quality.

## Philosophy: Thoroughness Over Speed

The refinement runs on a **local LLM** — there are no API costs, no rate limits, and latency is bounded by local hardware. A refinement that takes 5-10 seconds but prevents 3 rounds of clarification with Claude is a massive net win. The pipeline should:

- **Never sacrifice refinement quality for speed.** Let the LLM think.
- **Use generous timeouts.** The default should accommodate slower hardware and larger prompts without cutting off good output.
- **Fail gracefully but not eagerly.** Don't abort a refinement that's progressing — only bail on genuine failures (Ollama down, model not loaded, connection lost).
- **Support multi-pass refinement.** If a future milestone adds a second LLM call (e.g., rules relevance filtering), the pipeline should support it without treating it as a performance problem.

## Depends On

M1 (Hook Protocol) — need the correct I/O contract before hardening the pipeline that uses it.

---

## Tasks

### 2.1 — Ollama Client Robustness

**What:** Harden `ollama/client.go` for real-world conditions.

**Requirements:**
- **Connection timeout:** Configurable (default 5s). Detects whether Ollama is reachable. This is the only timeout that should be aggressive — if Ollama isn't running, fail fast.
- **Request timeout:** Configurable (default 120s). Total time for a refinement response. This must be generous — a 14B model on a 16GB machine may take 15-30s for a complex prompt, and that's fine.
- **Retry with backoff:** Single retry on transient errors (connection reset, 503). No retry on 4xx.
- **Model availability check:** Before sending chat request, verify the model is loaded. If not, attempt a load (with configurable keep_alive). Wait for the load to complete rather than failing.
- **Version check:** Warn (don't block) if Ollama version is below minimum (0.18.0).
- **Streaming:** Use streaming mode (`stream: true`) by default. This enables real-time progress visibility in the dashboard (M4) and allows the pipeline to detect stalled responses (no tokens for 30s = Ollama is stuck, not just thinking).

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
| Ollama stalled | Hangs forever | If no tokens received for 30s, abort and return raw prompt |
| Malformed LLM response | Pass through garbage | Validate output has expected structure, fall back to raw |
| Empty LLM response | Pass through empty | Return raw prompt, log warning |
| Rules files missing | Silent empty rules | Log which files were checked, continue with empty rules |
| Git context fails | Silent empty context | Log warning, continue without git context |

**Key principle:** The pipeline NEVER blocks the user indefinitely. Every failure returns the raw prompt. But "taking 15 seconds to produce a good refinement" is NOT a failure — it's working as intended.

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
- Include timing: log how long each pipeline stage takes (for observability, not enforcement)

**Log points:**
```
DEBUG: Loading rules from [agents.md, CLAUDE.md] (found: agents.md)
DEBUG: Rules hash: abc123
DEBUG: Cache miss for prompt hash: def456
DEBUG: Gathering git context...
DEBUG: Git context: branch=main, 5 recent commits
INFO:  Refining prompt (47 words) via qwen2.5-coder:14b
DEBUG: Ollama streaming response started...
DEBUG: Ollama response complete in 8.3s (1247 tokens)
INFO:  Refinement complete. Output: 312 words, validated OK.
WARN:  Ollama not available, passing through raw prompt
ERROR: Ollama returned malformed response: <details>
```

### 2.5 — Context Timeout (Safety Net Only)

**What:** A generous safety-net timeout to prevent infinite hangs — not a latency budget.

**Requirements:**
- `refine` command creates a context with a configurable timeout (default: **120s**)
- This is a safety net, not a performance target. It exists to catch cases where Ollama is genuinely stuck (deadlock, OOM), not to enforce speed.
- Context is passed to: rules loader, git context, cache lookup, Ollama chat
- If timeout fires, log a warning with the duration and return raw prompt
- No per-stage budgets. The LLM gets as much time as it needs within the overall safety net.

### 2.6 — Streaming Ollama Client

**What:** Add a streaming mode to the Ollama client that yields tokens as they arrive.

**Why:** The dashboard (M4) needs to show refinement progress in real-time. Streaming also enables detecting stalled responses — if no tokens arrive for 30s, something is genuinely wrong (vs. a non-streaming request where you can't distinguish "thinking" from "stuck").

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
- Streaming client respects context cancellation
- Token chunks are forwarded to an optional `TokenSink` interface (used by M4's SSE hub)
- Final response is assembled from chunks and validated the same way as non-streaming
- **Stall detection:** If no tokens arrive for 30s, cancel the request and return raw prompt. This replaces aggressive timeouts — it distinguishes "Ollama is generating tokens (healthy)" from "Ollama is stuck (abort)."
- If streaming fails mid-response, return what we have if it's structurally valid, otherwise fall back to raw prompt

**Backward compatibility:** Pipeline uses streaming by default. If the caller doesn't provide a `TokenSink`, tokens are silently consumed and only the final result is returned (behaves like non-streaming).

### 2.7 — Pipeline Tests

**What:** Comprehensive unit tests for the pipeline.

**Test cases:**
- Happy path: rules exist, Ollama responds, cache misses then hits
- Ollama unavailable: returns raw prompt
- Ollama stalled (no tokens for 30s): returns raw prompt
- Malformed response: returns raw prompt
- Empty rules: refinement still works with generic constraints
- Short prompt bypass: prompts under min_words pass through unchanged
- Cache hit: returns cached result without calling Ollama
- Output validation: catches empty, too-short, and leaked system prompts
- Long-running but healthy refinement: 15s response completes successfully (not aborted)

**Approach:** Use interfaces/mocks for Ollama client to test pipeline logic without a running server.

---

## Acceptance Criteria

- [ ] Ollama client has configurable timeouts (generous defaults), retry, and model pre-check
- [ ] Every pipeline failure mode returns raw prompt (never blocks indefinitely)
- [ ] Stall detection (30s no tokens) replaces aggressive latency budgets
- [ ] LLM output is validated before returning
- [ ] Structured logging with slog, all to stderr
- [ ] Context timeout as safety net (120s default), not performance enforcer
- [ ] Streaming Ollama client with TokenSink interface
- [ ] Pipeline test coverage >80%

## Files Modified

- `cli/internal/ollama/client.go` — timeout, retry, model check, streaming
- `cli/internal/pipeline/pipeline.go` — error handling, validation, logging, stall detection
- `cli/internal/pipeline/pipeline_test.go` — comprehensive tests
- `cli/cmd/refine.go` — context timeout setup
- `cli/internal/config/config.go` — timeout config fields if missing

## Risk

**Medium.** Existing code is structurally sound but lacks production error handling. This is straightforward hardening work.
