import { useEffect, useState } from 'react';
import { useActions, useSessions } from '@/store';
import { api } from '@/api/client';
import type { Session, Refinement, SessionMetrics } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';

function formatDuration(minutes: number): string {
  if (minutes < 1) return '<1m';
  if (minutes < 60) return `${Math.round(minutes)}m`;
  const h = Math.floor(minutes / 60);
  const m = Math.round(minutes % 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

function projectName(path: string): string {
  return path.split('/').filter(Boolean).pop() || path;
}

// ---------------------------------------------------------------------------
// Session list sidebar
// ---------------------------------------------------------------------------

function SessionList({
  sessions,
  selectedId,
  onSelect,
}: {
  sessions: Session[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-4 py-3">
        <h2 className="text-sm font-semibold">Sessions</h2>
        <p className="text-muted-foreground text-xs">{sessions.length} total</p>
      </div>
      <ScrollArea className="flex-1">
        {sessions.map((s) => {
          const isSelected = s.id === selectedId;
          return (
            <button
              key={s.id}
              onClick={() => onSelect(s.id)}
              className={`w-full border-b px-4 py-3 text-left transition-colors ${
                isSelected
                  ? 'bg-accent text-accent-foreground'
                  : 'hover:bg-muted/50'
              }`}
            >
              <div className="flex items-center justify-between">
                <span className="truncate text-sm font-medium">
                  {projectName(s.project_path)}
                </span>
                <Badge
                  variant={s.status === 'active' ? 'default' : 'secondary'}
                  className="ml-2 shrink-0 text-[10px]"
                >
                  {s.status}
                </Badge>
              </div>
              <div className="text-muted-foreground mt-1 flex gap-2 text-xs">
                <span>{new Date(s.started_at).toLocaleDateString()}</span>
                <span>{new Date(s.started_at).toLocaleTimeString()}</span>
              </div>
              <p className="text-muted-foreground mt-0.5 truncate font-mono text-xs">
                {s.id.slice(0, 12)}
              </p>
            </button>
          );
        })}
      </ScrollArea>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Session metrics cards
// ---------------------------------------------------------------------------

function MetricsPanel({ metrics }: { metrics: SessionMetrics }) {
  const cacheRate =
    metrics.total_refinements > 0
      ? ((metrics.cache_hits / metrics.total_refinements) * 100).toFixed(0)
      : '0';

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Refinements</p>
          <p className="text-xl font-bold">{metrics.total_refinements}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Duration</p>
          <p className="text-xl font-bold">
            {formatDuration(metrics.duration_minutes)}
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Avg Latency</p>
          <p className="text-xl font-bold">
            {metrics.avg_latency_ms > 0
              ? `${(metrics.avg_latency_ms / 1000).toFixed(1)}s`
              : '—'}
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Cache Hit</p>
          <p className="text-xl font-bold">{cacheRate}%</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Passthroughs</p>
          <p className="text-xl font-bold">{metrics.passthroughs}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Verifications</p>
          <p className="text-xl font-bold">{metrics.total_verifications}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Verify Pass</p>
          <p className="text-xl font-bold text-green-600">
            {metrics.verification_passes}
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Verify Fail</p>
          <p className="text-xl font-bold text-red-600">
            {metrics.verification_failures}
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Session detail (right panel)
// ---------------------------------------------------------------------------

function SessionPanel({
  session,
  onSelectRefinement,
}: {
  session: Session;
  onSelectRefinement: (id: number) => void;
}) {
  const [metrics, setMetrics] = useState<SessionMetrics | null>(null);
  const [refinements, setRefinements] = useState<Refinement[]>([]);
  const { fetchSessionRefinements } = useActions();

  useEffect(() => {
    setMetrics(null);
    setRefinements([]);
    api
      .sessionStats(session.id)
      .then(setMetrics)
      .catch(() => {});
    fetchSessionRefinements(session.id).then(setRefinements);
  }, [session.id, fetchSessionRefinements]);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">
          {projectName(session.project_path)}
        </h1>
        <div className="text-muted-foreground mt-1 flex items-center gap-3 text-sm">
          <Badge
            variant={session.status === 'active' ? 'default' : 'secondary'}
          >
            {session.status}
          </Badge>
          <span>{new Date(session.started_at).toLocaleString()}</span>
          {session.ended_at && (
            <>
              <span>—</span>
              <span>{new Date(session.ended_at).toLocaleTimeString()}</span>
            </>
          )}
        </div>
        <p className="text-muted-foreground mt-1 font-mono text-xs">
          {session.id}
        </p>
      </div>

      {metrics ? (
        <MetricsPanel metrics={metrics} />
      ) : (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <Card key={i}>
              <CardContent className="pt-4">
                <Skeleton className="mb-1 h-3 w-16" />
                <Skeleton className="h-6 w-12" />
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Separator />

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            Refinements ({refinements.length})
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {refinements.length === 0 && (
            <p className="text-muted-foreground p-4 text-sm">
              No refinements in this session.
            </p>
          )}
          {refinements.map((r) => (
            <button
              key={r.id}
              onClick={() => onSelectRefinement(r.id)}
              className="hover:bg-muted/50 w-full border-b px-4 py-3 text-left transition-colors"
            >
              <div className="mb-1 flex items-center justify-between">
                <span className="text-muted-foreground text-xs">
                  {new Date(r.created_at).toLocaleTimeString()}
                </span>
                <div className="flex gap-1.5">
                  {r.status === 'failed' && (
                    <Badge variant="destructive">failed</Badge>
                  )}
                  {r.cache_hit && <Badge variant="secondary">cached</Badge>}
                  {r.passthrough && (
                    <Badge variant="outline">passthrough</Badge>
                  )}
                  {!r.passthrough && !r.cache_hit && r.latency_ms > 0 && (
                    <Badge>{(r.latency_ms / 1000).toFixed(1)}s</Badge>
                  )}
                </div>
              </div>
              <p className="truncate text-sm">{r.raw_prompt}</p>
            </button>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main sessions page with sidebar
// ---------------------------------------------------------------------------

export function Sessions({
  selectedSessionId,
  onSelectSession,
  onSelectRefinement,
}: {
  selectedSessionId?: string;
  onSelectSession: (id: string) => void;
  onSelectRefinement: (id: number) => void;
}) {
  const sessions = useSessions();
  const { fetchSessions } = useActions();

  useEffect(() => {
    fetchSessions();
  }, [fetchSessions]);

  const selectedSession = selectedSessionId
    ? (sessions.find((s) => s.id === selectedSessionId) ?? null)
    : null;

  return (
    <div className="flex h-[calc(100vh-8rem)] gap-0 overflow-hidden rounded-lg border">
      {/* Sidebar */}
      <div className="bg-card w-72 shrink-0 border-r">
        <SessionList
          sessions={sessions}
          selectedId={selectedSessionId ?? null}
          onSelect={onSelectSession}
        />
      </div>

      {/* Main panel */}
      <div className="flex-1 overflow-y-auto p-6">
        {selectedSession ? (
          <SessionPanel
            session={selectedSession}
            onSelectRefinement={onSelectRefinement}
          />
        ) : (
          <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
            Select a session to view details
          </div>
        )}
      </div>
    </div>
  );
}
