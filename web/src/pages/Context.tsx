import { useEffect, useState } from 'react';
import { useSessions, useBootstrapEvent, useActions } from '@/store';
import { api } from '@/api/client';
import type { BootstrapEvent } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  DatabaseIcon,
  CheckCircleIcon,
  AlertCircleIcon,
  LoaderIcon,
} from 'lucide-react';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function projectName(path: string): string {
  return path.split('/').filter(Boolean).pop() || path;
}

function formatMicros(us: number): string {
  if (us < 1000) return `${Math.round(us)}\u00B5s`;
  if (us < 1_000_000) return `${(us / 1000).toFixed(1)}ms`;
  return `${(us / 1_000_000).toFixed(2)}s`;
}

function classifyStatusIcon(status: string) {
  switch (status) {
    case 'complete':
      return <CheckCircleIcon className="size-3.5 text-green-600" />;
    case 'pending':
      return <LoaderIcon className="size-3.5 animate-spin text-yellow-600" />;
    case 'failed':
      return <AlertCircleIcon className="size-3.5 text-red-600" />;
    case 'skipped':
      return <AlertCircleIcon className="text-muted-foreground size-3.5" />;
    default:
      return null;
  }
}

// ---------------------------------------------------------------------------
// Bootstrap status header
// ---------------------------------------------------------------------------

function BootstrapStatus({ event }: { event: BootstrapEvent }) {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
      <Card>
        <CardHeader className="pb-1">
          <CardTitle className="text-muted-foreground text-xs font-medium">
            Files Discovered
          </CardTitle>
        </CardHeader>
        <CardContent>
          <span className="text-xl font-bold tabular-nums">
            {event.files_discovered}
          </span>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-1">
          <CardTitle className="text-muted-foreground text-xs font-medium">
            Files Processed
          </CardTitle>
        </CardHeader>
        <CardContent>
          <span className="text-xl font-bold tabular-nums">
            {event.files_processed}
          </span>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-1">
          <CardTitle className="text-muted-foreground text-xs font-medium">
            Total Rules
          </CardTitle>
        </CardHeader>
        <CardContent>
          <span className="text-xl font-bold tabular-nums">
            {event.total_rules}
          </span>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-1">
          <CardTitle className="text-muted-foreground text-xs font-medium">
            Bootstrap Time
          </CardTitle>
        </CardHeader>
        <CardContent>
          <span className="text-xl font-bold tabular-nums">
            {formatMicros(event.duration_us)}
          </span>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-1">
          <CardTitle className="text-muted-foreground text-xs font-medium">
            Classification
          </CardTitle>
        </CardHeader>
        <CardContent>
          <span className="flex items-center gap-1.5 text-sm font-medium">
            {classifyStatusIcon(event.classify_status)}
            {event.classify_status}
          </span>
          {event.classify_duration_us != null && (
            <span className="text-muted-foreground text-xs">
              {formatMicros(event.classify_duration_us)}
            </span>
          )}
        </CardContent>
      </Card>
      {event.error_message && (
        <Card>
          <CardHeader className="pb-1">
            <CardTitle className="text-xs font-medium text-red-600">
              Error
            </CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-xs text-red-600">{event.error_message}</span>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Document browser
// ---------------------------------------------------------------------------
// Recent selections
// ---------------------------------------------------------------------------

interface SelectionEntry {
  refinement_id: number;
  doc_source: string;
  created_at: string;
}

function SelectionRow({ s }: { s: SelectionEntry }) {
  const time = new Date(s.created_at).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  return (
    <div className="flex items-center gap-3 border-b px-3 py-1.5 text-xs">
      <span className="text-muted-foreground w-[80px] shrink-0 font-mono">
        {time}
      </span>
      <span className="font-mono font-medium">{s.doc_source}</span>
      <span className="text-muted-foreground ml-auto">
        refinement #{s.refinement_id}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main Context page
// ---------------------------------------------------------------------------

export function Context({ selectedSessionId }: { selectedSessionId?: string }) {
  const sessions = useSessions();
  const { fetchSessions } = useActions();

  useEffect(() => {
    fetchSessions();
  }, [fetchSessions]);

  // Session selection — derive default from sessions list, no effect needed
  const [manualSessionId, setManualSessionId] = useState(
    selectedSessionId || '',
  );
  const sessionId =
    manualSessionId ||
    (() => {
      if (sessions.length === 0) return '';
      const sorted = [...sessions].sort((a, b) => {
        if (a.status === 'active' && b.status !== 'active') return -1;
        if (b.status === 'active' && a.status !== 'active') return 1;
        return (
          new Date(b.started_at).getTime() - new Date(a.started_at).getTime()
        );
      });
      return sorted[0].id;
    })();

  // Bootstrap data
  const liveBootstrap = useBootstrapEvent(sessionId);
  const [bootstrap, setBootstrap] = useState<BootstrapEvent | null>(null);
  const [selections, setSelections] = useState<SelectionEntry[]>([]);

  useEffect(() => {
    if (!sessionId) return;
    api.sessionBootstrap(sessionId).then((data) => {
      if (data) setBootstrap(data);
    });
  }, [sessionId]);

  // Use live SSE data if available, fall back to fetched data
  const displayBootstrap = liveBootstrap || bootstrap;

  // Load context selections via the session-level endpoint (single request)
  useEffect(() => {
    if (!sessionId) return;
    api
      .sessionContextSelections(sessionId)
      .then((sels) => {
        setSelections(
          sels.map((s) => ({
            refinement_id: s.refinement_id,
            doc_source: s.doc_source,
            created_at: s.created_at,
          })),
        );
      })
      .catch(() => setSelections([]));
  }, [sessionId]);

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-medium">Context</h1>
        <Select
          value={sessionId}
          onValueChange={(v) => v && setManualSessionId(v)}
        >
          <SelectTrigger size="sm" className="w-[280px]">
            <SelectValue placeholder="Select session" />
          </SelectTrigger>
          <SelectContent className="w-[340px]">
            {[...sessions]
              .sort((a, b) => {
                if (a.status === 'active' && b.status !== 'active') return -1;
                if (b.status === 'active' && a.status !== 'active') return 1;
                return (
                  new Date(b.started_at).getTime() -
                  new Date(a.started_at).getTime()
                );
              })
              .map((s) => (
                <SelectItem key={s.id} value={s.id}>
                  <span className="flex items-center gap-2">
                    <span
                      className={`inline-block h-1.5 w-1.5 rounded-full ${s.status === 'active' ? 'bg-green-500' : 'bg-muted-foreground/40'}`}
                    />
                    <span className="truncate">
                      {projectName(s.project_path)}
                    </span>
                    <span className="text-muted-foreground font-mono text-xs">
                      {s.id.slice(0, 8)}
                    </span>
                  </span>
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      {/* Bootstrap status */}
      {displayBootstrap ? (
        <BootstrapStatus event={displayBootstrap} />
      ) : (
        <Card>
          <CardContent className="flex items-center gap-2 py-6">
            <DatabaseIcon className="text-muted-foreground size-5" />
            <span className="text-muted-foreground text-sm">
              No bootstrap data for this session. Bootstrap runs automatically
              at session start.
            </span>
          </CardContent>
        </Card>
      )}

      {/* Document browser — placeholder until GET /api/bootstrap/documents endpoint */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">Rule Documents</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {displayBootstrap ? (
            <div className="text-muted-foreground px-3 py-4 text-center text-xs">
              <p>{displayBootstrap.files_processed} documents indexed.</p>
              <p className="mt-1">
                Document browser will be available in a future update.
              </p>
            </div>
          ) : (
            <div className="text-muted-foreground px-3 py-4 text-center text-xs">
              No documents indexed yet.
            </div>
          )}
        </CardContent>
      </Card>

      {/* Recent context selections */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            Recent Context Selections
            {selections.length > 0 && (
              <span className="text-muted-foreground ml-1 font-normal">
                ({selections.length})
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {selections.length > 0 ? (
            <div className="flex items-center gap-3 border-b px-3 py-1.5 text-xs font-medium">
              <span className="w-[80px] shrink-0">Time</span>
              <span>Document</span>
              <span className="ml-auto">Refinement</span>
            </div>
          ) : null}
          {selections.length > 0 ? (
            selections.map((s, i) => <SelectionRow key={i} s={s} />)
          ) : (
            <div className="text-muted-foreground px-3 py-4 text-center text-xs">
              No context selections recorded yet. Selections appear when the
              local LLM chooses relevant rule documents during refinement.
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
