import { useEffect, useRef } from 'react';
import {
  useAppStore,
  useRefinementDetail,
  useRefinement,
  useStream,
  useActions,
} from '@/store';
import type { StreamState } from '@/store';
import { useState } from 'react';
import { api } from '@/api/client';
import type {
  PipelineEvent,
  VerificationEvent,
  CheckRun,
  ContextSelection,
} from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Streamdown, type ThemeInput } from 'streamdown';
import { code } from '@streamdown/code';

const shikiTheme: [ThemeInput, ThemeInput] = ['night-owl-light', 'night-owl'];

// Stage colors for the waterfall chart
const stageColors: Record<string, string> = {
  rules_load: 'var(--chart-1)',
  cache_check: 'var(--chart-2)',
  git_context: 'var(--chart-3)',
  prompt_build: 'var(--chart-4)',
  ollama_check: 'var(--chart-5)',
  model_ensure: 'var(--chart-1)',
  ollama_inference: 'var(--chart-3)',
  validation: 'var(--chart-2)',
  cache_store: 'var(--chart-4)',
};

function formatDuration(us: number): string {
  if (us < 1000) return `${us}µs`;
  if (us < 1_000_000) return `${(us / 1000).toFixed(1)}ms`;
  return `${(us / 1_000_000).toFixed(1)}s`;
}

function PipelineTimeline({ events }: { events: PipelineEvent[] }) {
  if (events.length === 0) return null;

  const totalUs = events.reduce((sum, e) => sum + e.duration_us, 0);

  const MIN_PCT = 1;
  const rawPcts = events.map((e) =>
    totalUs > 0
      ? Math.max((e.duration_us / totalUs) * 100, MIN_PCT)
      : 100 / events.length,
  );
  const rawSum = rawPcts.reduce((s, v) => s + v, 0);
  const displayPcts = rawPcts.map((p) => (p / rawSum) * 100);

  const rows: { event: PipelineEvent; leftPct: number; widthPct: number }[] =
    [];
  let offset = 0;
  for (let i = 0; i < events.length; i++) {
    rows.push({ event: events[i], leftPct: offset, widthPct: displayPcts[i] });
    offset += displayPcts[i];
  }

  return (
    <div className="space-y-1.5">
      {rows.map(({ event: e, leftPct, widthPct }) => {
        return (
          <div key={e.id} className="flex items-center gap-3">
            <span className="text-muted-foreground w-28 text-right font-mono text-xs">
              {e.stage.replace(/_/g, ' ')}
            </span>
            <div className="bg-muted relative h-6 flex-1 overflow-hidden rounded-sm">
              <div
                className="absolute top-0 flex h-full items-center rounded-sm px-1.5"
                style={{
                  left: `${leftPct}%`,
                  width: `${widthPct}%`,
                  backgroundColor: stageColors[e.stage] ?? 'var(--chart-5)',
                  opacity: e.success ? 0.85 : 1,
                }}
              >
                {widthPct > 8 && (
                  <span className="truncate font-mono text-[10px] text-white/90">
                    {formatDuration(e.duration_us)}
                  </span>
                )}
              </div>
              {!e.success && (
                <div
                  className="bg-destructive absolute top-0 h-full rounded-sm"
                  style={{
                    left: `${leftPct}%`,
                    width: `${widthPct}%`,
                    opacity: 0.85,
                  }}
                />
              )}
            </div>
            <span className="text-muted-foreground w-20 font-mono text-xs">
              {formatDuration(e.duration_us)}
            </span>
          </div>
        );
      })}
      <div className="text-muted-foreground mt-1 text-right font-mono text-xs">
        Total: {formatDuration(totalUs)}
      </div>
    </div>
  );
}

function PipelineSkeleton() {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">Pipeline Timeline</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-1.5">
          {[
            'w-[60%]',
            'w-[15%]',
            'w-[80%]',
            'w-[40%]',
            'w-[90%]',
            'w-[25%]',
          ].map((width, i) => (
            <div key={i} className="flex items-center gap-3">
              <Skeleton className="h-4 w-28" />
              <div className="bg-muted relative h-6 flex-1 overflow-hidden rounded-sm">
                <Skeleton
                  className={`absolute top-0 h-full rounded-sm ${width}`}
                />
              </div>
              <Skeleton className="h-4 w-20" />
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

// Wrap raw code in a markdown fenced code block for Streamdown rendering.
// Strips existing fences first to prevent double-wrapping (LLMs sometimes
// include fences in their output despite being told not to).
function wrapInFence(text: string, lang: string): string {
  let cleaned = text.trim();
  // Strip existing markdown code fences
  if (cleaned.startsWith('```')) {
    const lines = cleaned.split('\n');
    // Remove opening fence line
    lines.shift();
    // Remove closing fence line
    if (lines.length > 0 && lines[lines.length - 1].trim() === '```') {
      lines.pop();
    }
    cleaned = lines.join('\n');
  }
  return '```' + lang + '\n' + cleaned + '\n```';
}

function FlowStage({
  label,
  description,
  content,
  format,
  stream,
  status,
}: {
  label: string;
  description: string;
  content: string | null;
  format?: 'xml' | 'json' | 'markdown' | 'text';
  stream?: StreamState | null;
  status?: string;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current && stream?.isStreaming) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [stream?.text, stream?.isStreaming]);

  const showStreaming =
    status === 'pending' && stream && (stream.isStreaming || stream.text);

  function renderContent(text: string) {
    // For code formats, wrap in a fenced code block so Streamdown + Shiki highlight it
    if (format === 'json' || format === 'xml') {
      return (
        <div className="max-h-[500px] overflow-auto text-xs [&_pre]:!m-0 [&_pre]:!bg-transparent [&_pre]:!p-0">
          <Streamdown mode="static" plugins={{ code }} shikiTheme={shikiTheme}>
            {wrapInFence(text, format)}
          </Streamdown>
        </div>
      );
    }

    // Markdown content — render directly with Streamdown
    if (format === 'markdown') {
      return (
        <div className="prose-sm max-h-[500px] overflow-auto text-sm [&_pre]:text-xs">
          <Streamdown mode="static" plugins={{ code }} shikiTheme={shikiTheme}>
            {text}
          </Streamdown>
        </div>
      );
    }

    // Plain text fallback
    return (
      <pre className="max-h-[500px] overflow-auto font-mono text-xs break-words whitespace-pre-wrap">
        {text}
      </pre>
    );
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{label}</CardTitle>
        <p className="text-muted-foreground text-xs">{description}</p>
      </CardHeader>
      <CardContent>
        {content ? (
          renderContent(content)
        ) : showStreaming ? (
          <div ref={scrollRef} className="max-h-[500px] overflow-auto text-sm">
            <Streamdown plugins={{ code }} shikiTheme={shikiTheme} isAnimating>
              {stream.text || 'Processing...'}
            </Streamdown>
          </div>
        ) : status === 'pending' ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-[80%]" />
            <Skeleton className="h-3 w-[60%]" />
          </div>
        ) : status === 'failed' ? (
          <p className="text-destructive p-4 text-xs italic">Failed</p>
        ) : (
          <p className="text-muted-foreground p-4 text-xs italic">
            Not available
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function MetricSkeleton() {
  return (
    <Card>
      <CardHeader className="pb-2">
        <Skeleton className="h-3 w-16" />
      </CardHeader>
      <CardContent>
        <Skeleton className="h-6 w-20" />
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Data Flow tab content
// ---------------------------------------------------------------------------

function DataFlowTab({
  r,
  activeStream,
}: {
  r: {
    raw_prompt: string;
    input_prompt: string | null;
    llm_output: string | null;
    refined_prompt: string | null;
    status: string;
  };
  activeStream: StreamState | null;
}) {
  return (
    <div className="space-y-4">
      <FlowStage
        label="1. User Prompt"
        description="What the developer typed in Claude Code"
        content={r.raw_prompt}
        format="markdown"
      />

      <FlowStage
        label="2. LLM Input"
        description="System prompt + assembled user message sent to local Ollama model"
        content={r.input_prompt}
        format="markdown"
        status={r.status}
      />

      <FlowStage
        label="3. LLM Output"
        description="Raw response from the local model (JSON classification)"
        content={r.llm_output}
        format="json"
        stream={activeStream}
        status={r.status}
      />

      <FlowStage
        label="4. Final Context (additionalContext)"
        description="Composed XML injected into Claude's context window"
        content={r.refined_prompt}
        format="xml"
        status={r.status}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Verification tab content
// ---------------------------------------------------------------------------

function parseJSON<T>(s: string | null): T | null {
  if (!s) return null;
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}

function VerificationTab({
  verifications,
}: {
  verifications: VerificationEvent[];
}) {
  if (!verifications || verifications.length === 0) {
    return (
      <p className="text-muted-foreground py-8 text-center text-sm">
        No verification events recorded for this session.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      {verifications.map((v) => {
        const isSnapshot = v.event_type === 'snapshot';
        const changedFiles = parseJSON<string[]>(v.changed_files);
        const checksRun = parseJSON<CheckRun[]>(v.checks_run);
        const cwdMismatch = v.cwd_input !== v.project_dir;

        return (
          <Card key={v.id}>
            <CardContent className="pt-4">
              <div className="flex items-center gap-3">
                <span className="text-lg">
                  {isSnapshot
                    ? '\u{1F4F7}'
                    : v.result === 'pass'
                      ? '\u2705'
                      : v.result === 'fail'
                        ? '\u274C'
                        : '\u23ED\uFE0F'}
                </span>

                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">
                      {isSnapshot ? 'Snapshot' : 'Verify'}
                    </span>
                    <Badge variant="outline" className="text-xs">
                      {v.hook_event}
                    </Badge>
                    {v.scope !== 'prompt' && (
                      <Badge variant="secondary" className="text-xs">
                        task: {v.scope.slice(0, 8)}
                      </Badge>
                    )}
                    {!isSnapshot && v.result && (
                      <Badge
                        variant={
                          v.result === 'pass'
                            ? 'default'
                            : v.result === 'fail'
                              ? 'destructive'
                              : 'secondary'
                        }
                        className="text-xs"
                      >
                        {v.result}
                      </Badge>
                    )}
                  </div>
                  <div className="text-muted-foreground mt-1 flex gap-3 text-xs">
                    <span>{new Date(v.created_at).toLocaleTimeString()}</span>
                    {v.duration_us != null && (
                      <span>{formatDuration(v.duration_us)}</span>
                    )}
                    {isSnapshot && v.file_count != null && (
                      <span>{v.file_count} files tracked</span>
                    )}
                    {changedFiles && (
                      <span>{changedFiles.length} files changed</span>
                    )}
                  </div>
                </div>
              </div>

              {changedFiles && changedFiles.length > 0 && (
                <details className="mt-3">
                  <summary className="text-muted-foreground cursor-pointer text-xs">
                    Changed files ({changedFiles.length})
                  </summary>
                  <div className="bg-muted mt-1 max-h-40 overflow-auto rounded p-2 font-mono text-xs">
                    {changedFiles.map((f) => (
                      <div key={f}>{f}</div>
                    ))}
                  </div>
                </details>
              )}

              {checksRun && checksRun.length > 0 && (
                <div className="mt-3 space-y-2">
                  {checksRun.map((check) => (
                    <div
                      key={check.name}
                      className="border-muted rounded border p-2"
                    >
                      <div className="flex items-center gap-2">
                        <Badge
                          variant={check.passed ? 'default' : 'destructive'}
                          className="text-xs"
                        >
                          {check.passed ? 'pass' : 'fail'}
                        </Badge>
                        <span className="text-sm font-medium">
                          {check.name}
                        </span>
                        <span className="text-muted-foreground font-mono text-xs">
                          {check.duration_ms}ms
                        </span>
                      </div>
                      <div className="text-muted-foreground mt-1 font-mono text-xs">
                        $ {check.command}
                      </div>
                      {check.output && !check.passed && (
                        <details className="mt-1">
                          <summary className="text-destructive cursor-pointer text-xs">
                            Error output
                          </summary>
                          <pre className="bg-muted mt-1 max-h-60 overflow-auto rounded p-2 font-mono text-xs break-words whitespace-pre-wrap">
                            {check.output}
                          </pre>
                        </details>
                      )}
                    </div>
                  ))}
                </div>
              )}

              {cwdMismatch ? (
                <div className="mt-2 rounded bg-yellow-500/10 p-2 text-xs">
                  <span className="font-medium text-yellow-700 dark:text-yellow-400">
                    CWD mismatch
                  </span>
                  <div className="text-muted-foreground mt-1 space-y-0.5 font-mono">
                    <div>
                      cwd (hook input): <code>{v.cwd_input}</code>
                    </div>
                    <div>
                      project root (CLAUDE_PROJECT_DIR):{' '}
                      <code>{v.project_dir}</code>
                    </div>
                  </div>
                </div>
              ) : v.cwd_input && v.project_dir ? (
                <div className="text-muted-foreground mt-2 font-mono text-xs">
                  project: {v.project_dir}
                </div>
              ) : null}
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Context Selection — shows which deep-context documents the LLM selected
// ---------------------------------------------------------------------------

function ContextSelections({ refinementId }: { refinementId: number }) {
  const [selections, setSelections] = useState<ContextSelection[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    api
      .refinementContextSelections(refinementId)
      .then((data) => {
        setSelections(data);
        setLoaded(true);
      })
      .catch(() => setLoaded(true));
  }, [refinementId]);

  if (!loaded || selections.length === 0) return null;

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">
          Context Selection
          <span className="text-muted-foreground ml-1 font-normal">
            ({selections.length}{' '}
            {selections.length === 1 ? 'document' : 'documents'})
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-2">
          {selections.map((s) => (
            <Badge key={s.id} variant="secondary" className="font-mono text-xs">
              {s.doc_source}
              {s.rules_selected > 0 && (
                <span className="text-muted-foreground ml-1">
                  ({s.rules_selected} rules)
                </span>
              )}
            </Badge>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function RefinementDetail({
  id,
  onBack,
}: {
  id: number;
  onBack: () => void;
}) {
  const detail = useRefinementDetail(id);
  const refinement = useRefinement(id);
  const stream = useStream();
  const { fetchRefinement } = useActions();

  useEffect(() => {
    fetchRefinement(id);

    // Poll only while pending — once complete, verification events
    // arrive via SSE broadcast (verification:new).
    const interval = setInterval(() => {
      const current = useAppStore.getState().refinements.get(id);
      if (current && current.status === 'pending') {
        fetchRefinement(id);
      } else {
        clearInterval(interval);
      }
    }, 3000);

    return () => clearInterval(interval);
  }, [id, fetchRefinement]);

  const r = detail?.refinement ?? refinement;
  const events = detail?.events ?? null;

  if (!r) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-4">
          <button
            onClick={onBack}
            className="text-muted-foreground hover:text-foreground text-sm"
          >
            &larr; Back
          </button>
          <Skeleton className="h-8 w-48" />
        </div>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <MetricSkeleton />
          <MetricSkeleton />
          <MetricSkeleton />
          <MetricSkeleton />
        </div>
        <PipelineSkeleton />
      </div>
    );
  }

  const activeStream = stream && stream.refinementId === id ? stream : null;
  const isPending = r.status === 'pending';
  const verifications = detail?.verifications ?? [];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <button
          onClick={onBack}
          className="text-muted-foreground hover:text-foreground text-sm"
        >
          &larr; Back
        </button>
        <h1 className="text-2xl font-bold">Refinement #{r.id}</h1>
        <div className="flex gap-2">
          {r.status === 'pending' && (
            <Badge variant="default" className="animate-pulse">
              Pending
            </Badge>
          )}
          {r.status === 'failed' && <Badge variant="destructive">Failed</Badge>}
          {r.cache_hit && <Badge variant="secondary">Cached</Badge>}
          {r.passthrough && <Badge variant="outline">Passthrough</Badge>}
          {r.status === 'complete' && r.output_valid === false && (
            <Badge variant="destructive">Invalid</Badge>
          )}
          {r.status === 'complete' &&
            r.output_valid === true &&
            !r.passthrough && <Badge>Valid</Badge>}
        </div>
      </div>

      {/* Metrics row */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-muted-foreground text-xs">
              Latency
            </CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-lg font-bold">
              {r.latency_ms > 0 ? (
                `${(r.latency_ms / 1000).toFixed(1)}s`
              ) : isPending ? (
                <Skeleton className="inline-block h-5 w-12" />
              ) : (
                '—'
              )}
            </span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-muted-foreground text-xs">
              Model
            </CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-lg font-bold">{r.model || '—'}</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-muted-foreground text-xs">
              Session
            </CardTitle>
          </CardHeader>
          <CardContent>
            <span className="font-mono text-xs">
              {r.session_id?.slice(0, 12) || '—'}
            </span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-muted-foreground text-xs">
              Time
            </CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-sm">
              {new Date(r.created_at).toLocaleString()}
            </span>
          </CardContent>
        </Card>
      </div>

      {/* Pipeline timeline */}
      {events && events.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Pipeline Timeline</CardTitle>
          </CardHeader>
          <CardContent>
            <PipelineTimeline events={events} />
          </CardContent>
        </Card>
      ) : isPending ? (
        <PipelineSkeleton />
      ) : null}

      {/* Context Selection — which deep-context docs the LLM selected */}
      <ContextSelections refinementId={id} />

      {/* Tabs: Data Flow (default) | Verification */}
      <Tabs defaultValue="data-flow">
        <TabsList>
          <TabsTrigger value="data-flow">Data Flow</TabsTrigger>
          <TabsTrigger value="verification">
            Verification
            {verifications.length > 0 && (
              <Badge
                variant="secondary"
                className="ml-1.5 px-1.5 py-0 text-[10px]"
              >
                {verifications.length}
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="data-flow" className="mt-4">
          <DataFlowTab r={r} activeStream={activeStream} />
        </TabsContent>

        <TabsContent value="verification" className="mt-4">
          <VerificationTab verifications={verifications} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
