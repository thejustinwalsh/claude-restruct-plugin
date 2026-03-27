import { useApi } from '@/hooks/useApi';
import { api } from '@/api/client';
import type { PipelineEvent } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';

function PipelineTimeline({ events }: { events: PipelineEvent[] }) {
  const maxDuration = Math.max(...events.map((e) => e.duration_ms), 1);

  return (
    <div className="space-y-2">
      {events.map((e) => (
        <div key={e.id} className="flex items-center gap-3">
          <span className="text-muted-foreground w-28 text-right font-mono text-xs">
            {e.stage}
          </span>
          <div className="bg-muted h-5 flex-1 overflow-hidden rounded-sm">
            <div
              className={`h-full rounded-sm ${e.success ? 'bg-primary' : 'bg-destructive'}`}
              style={{
                width: `${Math.max((e.duration_ms / maxDuration) * 100, 2)}%`,
              }}
            />
          </div>
          <span className="text-muted-foreground w-16 font-mono text-xs">
            {e.duration_ms < 1000
              ? `${e.duration_ms}ms`
              : `${(e.duration_ms / 1000).toFixed(1)}s`}
          </span>
        </div>
      ))}
    </div>
  );
}

function PromptDiff({ raw, refined }: { raw: string; refined: string | null }) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <div>
        <h3 className="text-muted-foreground mb-2 text-sm font-medium">
          Raw Prompt
        </h3>
        <pre className="bg-muted rounded-lg p-4 text-sm break-words whitespace-pre-wrap">
          {raw}
        </pre>
      </div>
      <div>
        <h3 className="text-muted-foreground mb-2 text-sm font-medium">
          Refined (additionalContext)
        </h3>
        {refined ? (
          <pre className="bg-muted max-h-[600px] overflow-auto rounded-lg p-4 text-sm break-words whitespace-pre-wrap">
            {refined}
          </pre>
        ) : (
          <p className="text-muted-foreground p-4 text-sm italic">
            Passthrough — no refinement generated
          </p>
        )}
      </div>
    </div>
  );
}

export function RefinementDetail({
  id,
  onBack,
}: {
  id: number;
  onBack: () => void;
}) {
  const { data, loading, error } = useApi(() => api.refinement(id), [id]);

  if (loading) return <p className="text-muted-foreground">Loading...</p>;
  if (error) return <p className="text-destructive">Error: {error}</p>;
  if (!data) return null;

  const { refinement: r, events } = data;

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
          {r.cache_hit && <Badge variant="secondary">Cached</Badge>}
          {r.passthrough && <Badge variant="outline">Passthrough</Badge>}
          {r.output_valid === false && (
            <Badge variant="destructive">Invalid</Badge>
          )}
          {r.output_valid === true && !r.passthrough && <Badge>Valid</Badge>}
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

      <PromptDiff raw={r.raw_prompt} refined={r.refined_prompt} />
    </div>
  );
}
