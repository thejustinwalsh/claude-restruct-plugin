import { useApi } from '@/hooks/useApi';
import { useLiveFeed, useStreamingTokens } from '@/hooks/useSSE';
import { api } from '@/api/client';
import type { Metrics, Refinement } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { ScrollArea } from '@/components/ui/scroll-area';
import { StreamingCard } from '@/components/StreamingCard';

function MetricCard({
  label,
  value,
  sub,
}: {
  label: string;
  value: string | number;
  sub?: string;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-muted-foreground text-sm font-medium">
          {label}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
        {sub && <p className="text-muted-foreground mt-1 text-xs">{sub}</p>}
      </CardContent>
    </Card>
  );
}

function RefinementRow({ r, onClick }: { r: Refinement; onClick: () => void }) {
  const time = new Date(r.created_at).toLocaleTimeString();
  const words = r.raw_prompt.split(/\s+/).length;
  return (
    <button
      onClick={onClick}
      className="hover:bg-muted/50 w-full border-b px-4 py-3 text-left transition-colors"
    >
      <div className="mb-1 flex items-center justify-between">
        <span className="text-muted-foreground text-xs">{time}</span>
        <div className="flex gap-1.5">
          {r.cache_hit && <Badge variant="secondary">cached</Badge>}
          {r.passthrough && <Badge variant="outline">passthrough</Badge>}
          {!r.passthrough && !r.cache_hit && (
            <Badge>{(r.latency_ms / 1000).toFixed(1)}s</Badge>
          )}
        </div>
      </div>
      <p className="truncate text-sm">{r.raw_prompt}</p>
      <p className="text-muted-foreground mt-0.5 text-xs">
        {words} words &middot; {r.model || 'n/a'}
      </p>
    </button>
  );
}

export function Dashboard({
  onSelectRefinement,
}: {
  onSelectRefinement: (id: number) => void;
}) {
  const { data: metrics } = useApi<Metrics>(() => api.metrics(), []);
  const { data: recent } = useApi<Refinement[]>(() => api.refinements(20), []);
  const { events: live, connected } = useLiveFeed();
  const { stream } = useStreamingTokens();

  // Merge live events with initial load, dedupe by id
  const allRefinements = (() => {
    const map = new Map<number, Refinement>();
    for (const r of recent ?? []) map.set(r.id, r);
    for (const r of live) map.set(r.id, r);
    return [...map.values()].sort((a, b) => b.id - a.id);
  })();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Restruct Dashboard</h1>
        <Badge variant={connected ? 'default' : 'destructive'}>
          {connected ? 'Live' : 'Disconnected'}
        </Badge>
      </div>

      {metrics && (
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <MetricCard
            label="Total Refinements"
            value={metrics.total_refinements}
          />
          <MetricCard
            label="Avg Latency"
            value={`${(metrics.avg_latency_ms / 1000).toFixed(1)}s`}
          />
          <MetricCard
            label="Cache Hit Rate"
            value={`${(metrics.cache_hit_rate * 100).toFixed(0)}%`}
            sub={`${metrics.cache_hits} hits`}
          />
          <MetricCard
            label="Sessions"
            value={metrics.total_sessions}
            sub={`${metrics.active_sessions} active`}
          />
        </div>
      )}

      <StreamingCard
        stream={stream}
        lastRefinement={allRefinements[0] ?? null}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Recent Refinements</CardTitle>
        </CardHeader>
        <ScrollArea className="h-[500px]">
          {allRefinements.length === 0 ? (
            <p className="text-muted-foreground p-4 text-sm">
              No refinements yet. Send a prompt through Claude Code to see data
              here.
            </p>
          ) : (
            allRefinements.map((r) => (
              <RefinementRow
                key={r.id}
                r={r}
                onClick={() => onSelectRefinement(r.id)}
              />
            ))
          )}
        </ScrollArea>
      </Card>
    </div>
  );
}
