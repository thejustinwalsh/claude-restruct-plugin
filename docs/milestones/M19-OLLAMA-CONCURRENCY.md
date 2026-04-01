# M19: Ollama Concurrency Management

## Goal

Prevent prompt refinement (UserPromptSubmit) and document classification (SessionStart async) from contending on a single Ollama instance. Refinement is latency-critical and must always win; classification is background enrichment and must yield. Introduce a client-side priority queue with context cancellation so high-priority requests preempt low-priority ones without relying on Ollama's own scheduling.

## Depends On

M2 (Core Pipeline — Ollama client), M11 (Project Bootstrap — classification system)

---

## Problem Statement

The restruct plugin sends two categories of requests to the same Ollama server:

1. **Prompt refinement** (`pipeline.Refine` via `ChatWithRetry`) — synchronous, user-blocking, runs on every `UserPromptSubmit` hook. Latency budget: <5s total, with most of that being inference time. Every extra second here is a second the user waits before Claude sees their prompt.

2. **Document classification** (`bootstrap.ClassifyAsync` via `client.Chat`) — async background task, runs after `SessionStart` returns. Classifies each discovered CLAUDE.md file sequentially. Can take 30-60s total for a project with several rule files. Nobody is waiting on this.

Both hit the same Ollama server at `localhost:11434`. Ollama processes inference requests sequentially on a single GPU — it does not interleave token generation across concurrent requests. The server accepts concurrent connections but queues them internally.

### What goes wrong today

**Scenario A: Classification blocks refinement.** User starts a session. Bootstrap completes and kicks off async classification. User types their first prompt 3 seconds later. The refine request arrives at Ollama while classification is mid-inference on the second document. Ollama queues the refine request. The user waits 10-15 seconds for the current classification document to finish before refinement even begins.

**Scenario B: Refinement blocks classification.** Classification is running. User sends 3 prompts in quick succession. Each prompt triggers a refine request. Classification's next document request sits in Ollama's queue behind all three refine requests. The 60-second classification timeout expires, and classification reports as failed despite Ollama being perfectly healthy.

**Scenario C: Stall detection false positives.** The refine request's stall timer (30s) starts counting while Ollama is still processing a classification request. If the queue delay exceeds the stall timeout, the refine request is aborted with a misleading "ollama stalled" error.

### Why Ollama can't solve this for us

Ollama's `OLLAMA_NUM_PARALLEL` setting (default 1, since Ollama 0.1.33) controls how many requests the server processes concurrently for the *same loaded model*. Setting it to 2 would allow both requests to proceed simultaneously, but:

- **Shared VRAM.** Parallel requests split the available KV cache. On a 16GB machine running a 14B model, there's barely enough VRAM for one request's context window. Two parallel requests would either halve the effective context length or cause Ollama to swap to system RAM, degrading both requests.
- **Throughput penalty.** GPU compute is not free to share. Two parallel inference streams on the same GPU run at roughly 50-60% the speed of sequential processing. Both the refine and classify requests get slower.
- **No priority mechanism.** `OLLAMA_NUM_PARALLEL=2` treats both requests equally. There's no way to tell Ollama "finish the refine request first."
- **Queue behavior.** When `num_parallel` is at capacity, additional requests queue. Ollama does not expose queue position, estimated wait time, or cancellation APIs. The client has no visibility into queue state.

Ollama does queue requests rather than rejecting them — a request that arrives while the server is busy will block until a slot opens. This is good (no lost requests) but means the client must manage its own priority to avoid the blocking scenarios above.

### Model loading and keep_alive

Ollama keeps a model loaded in VRAM for `keep_alive` duration after the last request (default 5 minutes, configurable per-request). Both refine and classify use the same model (`qwen2.5-coder:14b`), so there's no model loading/unloading overhead between them — the model stays hot. Running a separate model instance for classification (e.g., a smaller model) would require loading two models simultaneously, which doubles VRAM usage and isn't viable on most developer machines.

---

## Solution: Client-Side Priority Queue

Since Ollama has no priority mechanism, we build one in the Go client. The queue sits between the pipeline/bootstrap callers and the Ollama HTTP client, ensuring that:

1. Only one request is in-flight to Ollama at any time (matches `num_parallel=1` default).
2. High-priority requests (refine) preempt low-priority requests (classify) by cancelling the low-priority request's context.
3. Low-priority requests automatically re-queue after preemption and resume when the high-priority request completes.

### Priority levels

| Priority | Caller | Behavior |
|----------|--------|----------|
| **Critical** | `pipeline.Refine` | Preempts all lower priorities. Never cancelled by the queue. |
| **Background** | `bootstrap.ClassifyAsync` | Yields to Critical. Context cancelled when a Critical request arrives. Re-queued automatically after preemption. |

Two levels are sufficient. If future milestones add more request types (e.g., M10 self-improvement analysis), they slot into Background unless they're user-blocking.

---

## Architecture

```
pipeline.Refine()                    bootstrap.ClassifyAsync()
      │                                       │
      │ priority: Critical                    │ priority: Background
      ▼                                       ▼
┌─────────────────────────────────────────────────┐
│              OllamaQueue (new)                  │
│                                                 │
│  ┌───────────┐  ┌──────────────────────────┐    │
│  │ Semaphore │  │ Active Request Tracker    │    │
│  │ (cap: 1)  │  │ - current priority        │    │
│  │           │  │ - cancel func             │    │
│  └───────────┘  └──────────────────────────┘    │
│                                                 │
│  On submit(priority, request):                  │
│    1. If active request has lower priority:     │
│       → cancel it (context cancellation)        │
│       → wait for semaphore release              │
│    2. Acquire semaphore                         │
│    3. Track as active request                   │
│    4. Forward to Ollama client                  │
│    5. Release semaphore on completion            │
│                                                 │
│  Preempted requests:                            │
│    → caller receives context.Canceled           │
│    → ClassifyAsync's loop checks ctx.Err()      │
│    → re-queues the same document after a brief  │
│      backoff (100ms)                            │
│                                                 │
└───────────────────┬─────────────────────────────┘
                    │
                    ▼
           ollama.Client (existing)
                    │
                    ▼
           Ollama server (:11434)
```

### Queue integration points

The queue wraps the existing `ollama.Client` and exposes the same `ChatStream`/`ChatWithRetry`/`Chat` interface. Callers pass a priority alongside their request. The pipeline and bootstrap don't need to know about each other — the queue mediates.

```
cmd/refine.go
  → pipeline.New() creates Pipeline with OllamaQueue (Critical priority)

cmd/bootstrap.go
  → startClassifyAsync() creates Classifier with OllamaQueue (Background priority)

Both share the same OllamaQueue instance? No — they run in separate OS processes
(restruct refine vs restruct bootstrap). See "Cross-Process Coordination" below.
```

### Cross-process coordination

This is the key architectural constraint: `restruct refine` and `restruct bootstrap` are separate process invocations. They don't share memory. The queue can't be a simple in-process semaphore.

**Options considered:**

| Approach | Pros | Cons |
|----------|------|------|
| **A. File-based lock** | Simple, no dependencies | No preemption — lock holder runs to completion |
| **B. Unix socket coordinator** | Full priority + preemption | Complex, new long-running daemon |
| **C. Advisory lock + cancellation signal** | Preemption via signal file, no daemon | Polling overhead, race conditions |
| **D. Ollama request cancellation via HTTP** | Native, no coordinator | Ollama doesn't support request cancellation |
| **E. Classify yields proactively** | Simple, no coordination infra | Doesn't handle all scenarios, but handles the common ones |

**Chosen: Option E — Classify yields proactively, with an advisory lock for coordination.**

The insight is that the common case (Scenario A) is the critical one: classification is running, refine arrives, classification should stop. Since classification processes documents one at a time in a loop, we can make classification check for "refine wants the GPU" before each document and between inference calls.

The mechanism:

1. **Lock file:** `.restruct/ollama.lock` — a flock-based advisory lock.
2. **Refine** acquires the lock before calling Ollama. If the lock is held (classify is mid-inference), refine writes a **preempt signal** (`.restruct/ollama.preempt`) and waits for the lock with a short timeout.
3. **Classify** checks for the preempt signal between documents. If present, it releases the lock, waits for the signal to clear (refine finished), then resumes.
4. **Classify** also wraps each Ollama call in a context that's cancelled if the preempt signal appears mid-inference.

This avoids a daemon, avoids polling overhead (flock is kernel-level), and handles the dominant contention scenario. The worst case is that classify's current document finishes before it notices the preempt signal — adding at most one document's inference time (~5-10s) to refine's wait. Acceptable for a first iteration.

```
Classify process                      Refine process
      │                                     │
      ├── flock(.restruct/ollama.lock)       │
      ├── classify doc 1                     │
      │   (Ollama inference ~5s)             │
      ├── check preempt signal → none        │
      ├── classify doc 2                     │
      │   (Ollama inference, 2s in...)       ├── write .restruct/ollama.preempt
      │   ...context cancelled               ├── flock(.restruct/ollama.lock) — blocks
      ├── detect preempt, release lock       │
      │   wait for preempt clear...          ├── (lock acquired)
      │                                      ├── refine prompt
      │                                      │   (Ollama inference ~3s)
      │                                      ├── delete .restruct/ollama.preempt
      │                                      ├── release lock
      ├── preempt cleared, re-acquire lock   │
      ├── re-classify doc 2 (retry)          │
      ├── classify doc 3                     │
      └── done                               │
```

### Preemption mid-inference

For classify to actually stop mid-inference (not just between documents), the `ChatStream`/`Chat` call needs a cancellable context. The existing `ChatStream` already respects `ctx.Done()` — when the context is cancelled, it returns `ctx.Err()`. We add a watcher goroutine in the classify flow that cancels the context when the preempt signal file appears:

```go
// In classify's Ollama call wrapper
ctx, cancel := context.WithCancel(parentCtx)
go func() {
    // Poll for preempt signal every 500ms
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if preemptSignalExists() {
                cancel()
                return
            }
        }
    }
}()
result, err := client.Chat(ctx, ...)
```

This means classify's in-flight Ollama request is aborted within 500ms of refine requesting preemption. Ollama stops generating tokens for the cancelled HTTP connection and is immediately free for the refine request.

The 500ms polling interval is a pragmatic choice — it keeps CPU overhead negligible (one stat call every 500ms) while capping preemption latency at 500ms in the worst case. A `fsnotify` watcher would give instant notification but adds a dependency for marginal gain.

---

## Detailed Design

### New package: `internal/ollama/priority.go`

```go
// Priority levels for Ollama request scheduling.
type Priority int

const (
    PriorityBackground Priority = iota
    PriorityCritical
)

// Lock manages cross-process Ollama access coordination.
type Lock struct {
    lockPath    string
    preemptPath string
}

// Acquire attempts to acquire the Ollama lock.
// If preempt is true, writes the preempt signal before waiting.
func (l *Lock) Acquire(ctx context.Context, preempt bool) (release func(), err error)

// CheckPreempt returns true if a higher-priority process wants the lock.
func (l *Lock) CheckPreempt() bool

// WatchPreempt returns a context that's cancelled when the preempt signal appears.
func (l *Lock) WatchPreempt(parent context.Context) (context.Context, context.CancelFunc)
```

### Changes to `internal/ollama/client.go`

No changes to the Client struct itself. The priority coordination happens at the caller level (pipeline and bootstrap), not inside the HTTP client. The Client remains a dumb HTTP wrapper — coordination is a separate concern.

### Changes to `internal/pipeline/pipeline.go`

```go
func (p *Pipeline) Refine(ctx context.Context, rawPrompt string, sink ollama.TokenSink) (*RefineResult, error) {
    // ... existing stages 1-6 ...

    // 7. Acquire Ollama lock (preempting any background classify)
    lock := ollama.NewLock(p.dataDir)
    release, err := lock.Acquire(ctx, true) // preempt=true
    if err != nil {
        // Lock acquisition failed (timeout or context cancelled)
        // Fall through without lock — Ollama request may queue behind classify
        slog.Warn("failed to acquire ollama lock, proceeding without", "error", err)
    } else {
        defer release()
    }

    // 8. Check Ollama availability (existing)
    // 9. Ensure model (existing)
    // 10. Call LLM (existing)
    // ...
}
```

### Changes to `cli/cmd/bootstrap.go` — `startClassifyAsync`

```go
func startClassifyAsync(docs []*bootstrap.Document, linksDir string, cfg *config.Config, ...) {
    // ...existing client creation...

    lock := ollama.NewLock(dataDir)

    // Wrap the chat function with lock awareness
    chatFn := func(ctx context.Context, system, user string, temp float32, maxTokens int) (string, error) {
        // Check preempt before starting
        if lock.CheckPreempt() {
            return "", context.Canceled
        }

        // Watch for preempt during inference
        inferCtx, inferCancel := lock.WatchPreempt(ctx)
        defer inferCancel()

        return client.Chat(inferCtx, system, user, temp, maxTokens)
    }

    classifier := bootstrap.NewClassifier(chatFn, linksDir, ...)

    // Enhanced classify loop with preempt handling
    for _, doc := range docs {
        // Acquire lock for this document
        release, err := lock.Acquire(ctx, false) // preempt=false (we're background)
        if err != nil {
            // Preempted or timed out — wait and retry
            waitForPreemptClear(lock, ctx)
            release, err = lock.Acquire(ctx, false)
            if err != nil {
                slog.Warn("classify: could not acquire lock after wait", "error", err)
                continue
            }
        }

        result, err := classifier.ClassifyOne(ctx, doc)
        release()

        if errors.Is(err, context.Canceled) {
            // Preempted mid-inference — wait for refine to finish, then retry this doc
            slog.Debug("classify: preempted, waiting to retry", "doc", doc.Source)
            waitForPreemptClear(lock, ctx)
            // Retry this document
            release2, _ := lock.Acquire(ctx, false)
            result, err = classifier.ClassifyOne(ctx, doc)
            if release2 != nil {
                release2()
            }
        }

        if err != nil {
            slog.Warn("classify: failed", "doc", doc.Source, "error", err)
            continue
        }

        // ...existing enrichment logic...
    }
}
```

### Changes to `internal/bootstrap/classify.go`

The `ClassifyAsync` method's goroutine loop already checks `ctx.Done()` between documents. The preemption-aware `chatFn` wrapper handles mid-inference cancellation. The main change is in `cmd/bootstrap.go` (the caller), not in the classifier itself.

One adjustment: `ClassifyOne` should not log a warning when the error is `context.Canceled` — that's expected behavior during preemption, not a failure.

---

## Tasks

| Task | Estimate | Description |
|------|----------|-------------|
| 19.1 — Lock implementation | 3h | `internal/ollama/priority.go`: flock-based advisory lock with preempt signal file. `Acquire()`, `Release()`, `CheckPreempt()`, `WatchPreempt()`. Cross-platform: flock on macOS/Linux, LockFileEx on Windows (or skip Windows — pure Go `os.OpenFile` with `O_EXCL` as fallback). |
| 19.2 — Pipeline lock integration | 2h | Modify `pipeline.go` `Refine()` to acquire the lock with preempt before Ollama calls. Pass `dataDir` through Pipeline config. Graceful fallback if lock acquisition fails. |
| 19.3 — Bootstrap classify preemption | 3h | Modify `cmd/bootstrap.go` `startClassifyAsync()`: acquire/release lock per document, watch for preempt signal during inference, retry preempted documents after backoff. |
| 19.4 — Classify context cancellation | 2h | Implement preempt-aware context wrapper in the classify chat function. 500ms poll interval for preempt signal. Cancel in-flight Ollama request on preempt detection. |
| 19.5 — Classify retry logic | 2h | When `ClassifyOne` returns `context.Canceled`, wait for preempt to clear, re-acquire lock, retry the document. Cap retries at 3 per document. Track retry count in classify metrics. |
| 19.6 — Stale lock cleanup | 1h | On process start, check if lock file exists with a stale PID (process no longer running). Clean up stale locks. Write PID to lock file for staleness detection. |
| 19.7 — Metrics and logging | 1h | Log lock acquisition time, preemption events, retry counts. Add timing stage `lock_acquire` to pipeline `RefineResult.Timings`. Record preemption events to SQLite for dashboard (reuse `pipeline_events` table with `event_type: "preempt"`). |
| 19.8 — Config: lock timeout | 0.5h | Add `ollama.lock_timeout` to `OllamaConfig` (default 10s). Refine waits up to this duration for the lock. If exceeded, proceeds without lock (degraded but functional). |
| 19.9 — Unit tests: lock | 3h | Test concurrent lock acquisition from two goroutines (simulating two processes). Test preempt signal write/detect/clear cycle. Test stale lock cleanup. Test WatchPreempt context cancellation timing. |
| 19.10 — Unit tests: pipeline preemption | 2h | Mock Ollama client. Simulate classify holding the lock, refine preempting. Verify refine completes without excessive delay. Verify classify retries the interrupted document. |
| 19.11 — Integration test: end-to-end | 3h | Spawn two goroutines: one running classify loop, one sending a refine request mid-classify. Verify refine latency is not degraded by classify. Verify classify completes all documents despite preemption. Use a mock Ollama server (HTTP handler with configurable response delay). |
| 19.12 — Dashboard: preemption visibility | 2h | Show preemption events in the session detail view. Count of preempted classify documents, total classify delay from preemption, lock wait times for refine. |

**Total estimate: ~24.5h**

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Coordination level | Cross-process (file lock), not in-process | `restruct refine` and `restruct bootstrap` are separate process invocations from Claude Code hooks. They don't share memory. |
| Lock mechanism | `flock` advisory lock | Kernel-level, no polling for basic lock/unlock. Automatically released if process crashes. Available on macOS and Linux (the target platforms). |
| Preemption mechanism | Signal file + context cancellation | Preempt signal (`.restruct/ollama.preempt`) is written by refine, polled by classify. Context cancellation propagates to the HTTP client, which closes the connection. Ollama stops generating tokens for closed connections. |
| Preempt poll interval | 500ms | One `os.Stat` call every 500ms is negligible CPU. Worst-case preemption latency is 500ms — acceptable given the 5s refine budget. |
| `OLLAMA_NUM_PARALLEL` | Leave at default (1) | Parallel inference on a single GPU splits VRAM and reduces throughput. Sequential with client-side priority is better for developer hardware. |
| Separate models for classify vs refine | No | Same model avoids double VRAM usage. The queue ensures they don't contend. If a future milestone wants a smaller classify model, the lock mechanism works regardless. |
| Retry cap | 3 per document | Prevents infinite retry loops if refine requests arrive continuously. After 3 preemptions of the same document, skip it — the structural metadata is sufficient. |
| Lock timeout for refine | 10s (configurable) | If classify's current Ollama request doesn't respond to cancellation within 10s, refine proceeds without the lock. This is a safety net — it should never hit in practice since context cancellation closes the HTTP connection within 500ms. |
| Fallback on lock failure | Proceed without lock | The lock is an optimization, not a correctness requirement. Without it, refine may queue behind classify in Ollama — slower but functional. No user-visible errors. |
| No daemon | Correct | A long-running coordinator process would need lifecycle management, crash recovery, and port allocation. File locks achieve the same goal with zero operational complexity. The lock file is cleaned up by flock semantics (released on process exit). |

---

## Acceptance Criteria

- [ ] Refine requests are not delayed by in-progress classification (preemption latency <1s)
- [ ] Classification completes all documents despite preemption (retry logic works)
- [ ] No stale lock files left after process crashes
- [ ] Pipeline `RefineResult.Timings` includes `lock_acquire` stage
- [ ] Classification logs preemption events with document source and retry count
- [ ] Lock timeout is configurable via `ollama.lock_timeout`
- [ ] Graceful degradation: if lock mechanism fails, both refine and classify still work (just without priority)
- [ ] Existing pipeline tests pass without modification (lock is opt-in based on `dataDir` availability)
- [ ] Dashboard shows preemption events in session detail

## Files Modified

- `cli/internal/ollama/priority.go` — NEW: Lock, preempt signal, WatchPreempt
- `cli/internal/ollama/priority_test.go` — NEW: lock and preemption tests
- `cli/internal/pipeline/pipeline.go` — lock acquisition before Ollama calls
- `cli/internal/config/config.go` — `LockTimeout` field on `OllamaConfig`
- `cli/cmd/bootstrap.go` — `startClassifyAsync` preemption-aware classify loop
- `cli/internal/bootstrap/classify.go` — suppress warning log on `context.Canceled`
- `cli/internal/db/migrations/` — optional: preemption event recording

## Risk

**Low.** The lock mechanism is additive — it wraps existing Ollama calls without changing them. The fallback behavior (no lock, requests queue in Ollama) is identical to current behavior. The main risk is platform-specific flock behavior on edge-case filesystems (NFS, Docker volumes with host mounts), mitigated by the `O_EXCL` fallback and the "proceed without lock" degradation path. The preemption polling adds negligible overhead (one stat syscall per 500ms during classify only).
