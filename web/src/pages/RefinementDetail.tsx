import { useEffect, useRef } from 'react';
import { useRefinementDetail, useStream, useActions } from '@/store';
import type { StreamState } from '@/store';
import type { PipelineEvent } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import { XmlHighlight } from '@/components/XmlHighlight';

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

  // Compute display widths: each stage gets at least MIN_PCT so tiny stages
  // are visible, then normalize so all widths sum to exactly 100%.
  const MIN_PCT = 1;
  const rawPcts = events.map((e) =>
    totalUs > 0
      ? Math.max((e.duration_us / totalUs) * 100, MIN_PCT)
      : 100 / events.length,
  );
  const rawSum = rawPcts.reduce((s, v) => s + v, 0);
  const displayPcts = rawPcts.map((p) => (p / rawSum) * 100);

  // Build rows with cumulative left offsets
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
  format?: 'xml' | 'text';
  stream?: StreamState | null;
  status?: string;
}) {
  const scrollRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (scrollRef.current && stream?.isStreaming) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [stream?.text, stream?.isStreaming]);

  const showStreaming =
    status === 'pending' && stream && (stream.isStreaming || stream.text);

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{label}</CardTitle>
        <p className="text-muted-foreground text-xs">{description}</p>
      </CardHeader>
      <CardContent>
        {content ? (
          <pre className="bg-muted max-h-[500px] overflow-auto rounded-lg p-4 font-mono text-xs break-words whitespace-pre-wrap">
            {format === 'xml' ? <XmlHighlight code={content} /> : content}
          </pre>
        ) : showStreaming ? (
          <pre
            ref={scrollRef}
            className="bg-muted max-h-[500px] overflow-auto rounded-lg p-4 font-mono text-xs break-words whitespace-pre-wrap"
          >
            {stream.text || 'Processing...'}
            {stream.isStreaming && <span className="animate-pulse">▌</span>}
          </pre>
        ) : status === 'pending' ? (
          <p className="text-muted-foreground p-4 text-xs italic">Pending...</p>
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

export function RefinementDetail({
  id,
  onBack,
}: {
  id: number;
  onBack: () => void;
}) {
  const detail = useRefinementDetail(id);
  const stream = useStream();
  const { fetchRefinement } = useActions();

  // Fetch on mount if not in cache
  useEffect(() => {
    fetchRefinement(id);
  }, [id, fetchRefinement]);

  if (!detail) return <p className="text-muted-foreground">Loading...</p>;

  const { refinement: r, events } = detail;
  const activeStream = stream && stream.refinementId === id ? stream : null;

  return (
    <div className="space-y-6">
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

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-muted-foreground text-xs">
              Latency
            </CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-lg font-bold">
              {r.latency_ms > 0 ? `${(r.latency_ms / 1000).toFixed(1)}s` : '—'}
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

      {events && events.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Pipeline Timeline</CardTitle>
          </CardHeader>
          <CardContent>
            <PipelineTimeline events={events} />
          </CardContent>
        </Card>
      )}

      <Separator />

      <h2 className="text-lg font-semibold">Data Flow</h2>

      <FlowStage
        label="1. User Prompt"
        description="What the developer typed in Claude Code"
        content={r.raw_prompt}
      />

      <FlowStage
        label="2. LLM Input"
        description="System prompt + assembled user message sent to local Ollama model"
        content={r.input_prompt}
      />

      <FlowStage
        label="3. LLM Output"
        description="Raw response from the local model (JSON classification)"
        content={r.llm_output}
        stream={activeStream}
        status={r.status}
      />

      <FlowStage
        label="4. Final Context (additionalContext)"
        description="Composed XML injected into Claude's context window"
        content={r.refined_prompt}
        format="xml"
      />
    </div>
  );
}
