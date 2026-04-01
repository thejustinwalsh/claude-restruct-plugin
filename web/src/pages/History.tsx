import { useEffect, useState, useMemo, useCallback } from 'react';
import { LegendList } from '@legendapp/list/react';
import { useSessions, useActions } from '@/store';
import { api } from '@/api/client';
import type { Session, ToolDecision, TimelineEvent } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type EventTypeFilter = 'all' | 'refinement' | 'tool_decision' | 'verification';
type DecisionFilter = 'all' | 'allow' | 'deny' | 'passthrough';

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

function tierColor(tier: number): string {
  if (tier <= 2) return 'text-green-600';
  if (tier <= 4) return 'text-blue-600';
  if (tier <= 6) return 'text-yellow-600';
  return 'text-red-600';
}

function decisionVariant(
  decision: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (decision) {
    case 'allow':
      return 'default';
    case 'deny':
      return 'destructive';
    case 'passthrough':
      return 'outline';
    default:
      return 'secondary';
  }
}

function eventIcon(type: string): string {
  switch (type) {
    case 'refinement':
      return '\u2728';
    case 'tool_decision':
      return '\u2699\uFE0F';
    case 'verification':
      return '\u2714\uFE0F';
    default:
      return '\u25CF';
  }
}

// ---------------------------------------------------------------------------
// Stats cards
// ---------------------------------------------------------------------------

function ToolDecisionStats({ decisions }: { decisions: ToolDecision[] }) {
  const total = decisions.length;
  const allowed = decisions.filter((d) => d.hook_decision === 'allow').length;
  const denied = decisions.filter((d) => d.hook_decision === 'deny').length;
  const passthrough = decisions.filter(
    (d) => d.hook_decision === 'passthrough',
  ).length;
  const avgUs =
    total > 0
      ? decisions.reduce((s, d) => s + (d.hook_duration_us || 0), 0) / total
      : 0;

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Total Decisions</p>
          <p className="text-xl font-bold">{total}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Auto-Approved</p>
          <p className="text-xl font-bold text-green-600">{allowed}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Denied</p>
          <p className="text-xl font-bold text-red-600">{denied}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Passthrough</p>
          <p className="text-xl font-bold">{passthrough}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Avg Latency</p>
          <p className="text-xl font-bold">{formatMicros(avgUs)}</p>
        </CardContent>
      </Card>
    </div>
  );
}

function TimelineStats({ timeline }: { timeline: TimelineEvent[] }) {
  const total = timeline.length;
  const refinements = timeline.filter(
    (e) => e.event_type === 'refinement',
  ).length;
  const toolDecs = timeline.filter(
    (e) => e.event_type === 'tool_decision',
  ).length;
  const verifications = timeline.filter(
    (e) => e.event_type === 'verification',
  ).length;
  const failures = timeline.filter(
    (e) => e.status === 'deny' || e.status === 'fail' || e.status === 'failed',
  ).length;

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Total Events</p>
          <p className="text-xl font-bold">{total}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Refinements</p>
          <p className="text-xl font-bold">{refinements}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Tool Decisions</p>
          <p className="text-xl font-bold">{toolDecs}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Verifications</p>
          <p className="text-xl font-bold">{verifications}</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="pt-4">
          <p className="text-muted-foreground text-xs">Failures</p>
          <p className="text-xl font-bold text-red-600">{failures}</p>
        </CardContent>
      </Card>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tool decision row
// ---------------------------------------------------------------------------

function ToolDecisionRow({ d }: { d: ToolDecision }) {
  return (
    <div className="hover:bg-muted/50 flex items-start gap-3 border-b px-4 py-3 transition-colors">
      <span className="text-muted-foreground mt-0.5 shrink-0 font-mono text-xs">
        {new Date(d.created_at).toLocaleTimeString()}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Badge variant="secondary" className="text-xs">
            {d.tool_name}
          </Badge>
          <Badge variant={decisionVariant(d.hook_decision)} className="text-xs">
            {d.hook_decision}
          </Badge>
          <span className={`text-xs font-medium ${tierColor(d.hook_tier)}`}>
            T{d.hook_tier}
          </span>
          {d.hook_duration_us > 0 && (
            <span className="text-muted-foreground text-xs">
              {formatMicros(d.hook_duration_us)}
            </span>
          )}
          {d.outcome && d.outcome !== 'pending' && (
            <Badge
              variant={d.outcome === 'executed' ? 'default' : 'destructive'}
              className="text-[10px]"
            >
              {d.outcome}
            </Badge>
          )}
        </div>
        {d.tool_input_summary && (
          <p className="text-muted-foreground mt-1 truncate font-mono text-xs">
            {d.tool_input_summary}
          </p>
        )}
        <p className="text-muted-foreground mt-0.5 text-xs">{d.hook_reason}</p>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Timeline event row
// ---------------------------------------------------------------------------

function TimelineRow({ event }: { event: TimelineEvent }) {
  return (
    <div className="hover:bg-muted/50 flex items-start gap-3 border-b px-4 py-3 transition-colors">
      <span className="text-muted-foreground mt-0.5 shrink-0 font-mono text-xs">
        {new Date(event.timestamp).toLocaleTimeString()}
      </span>
      <span className="mt-0.5 shrink-0 text-sm">
        {eventIcon(event.event_type)}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Badge
            variant={
              event.event_type === 'refinement'
                ? 'default'
                : event.event_type === 'verification'
                  ? 'secondary'
                  : 'outline'
            }
            className="text-xs"
          >
            {event.event_type}
          </Badge>
          {event.status && (
            <Badge
              variant={
                event.status === 'deny' || event.status === 'fail'
                  ? 'destructive'
                  : event.status === 'allow' || event.status === 'pass'
                    ? 'default'
                    : 'outline'
              }
              className="text-xs"
            >
              {event.status}
            </Badge>
          )}
        </div>
        <p className="mt-1 truncate text-sm">{event.summary}</p>
        {event.detail && (
          <p className="text-muted-foreground mt-0.5 truncate text-xs">
            {event.detail}
          </p>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Filters bar
// ---------------------------------------------------------------------------

function Filters({
  sessions,
  sessionId,
  onSessionChange,
  viewMode,
  eventType,
  onEventTypeChange,
  decision,
  onDecisionChange,
  toolName,
  onToolNameChange,
  search,
  onSearchChange,
  toolNames,
}: {
  sessions: Session[];
  sessionId: string;
  onSessionChange: (v: string) => void;
  viewMode: ViewMode;
  eventType: EventTypeFilter;
  onEventTypeChange: (v: EventTypeFilter) => void;
  decision: DecisionFilter;
  onDecisionChange: (v: DecisionFilter) => void;
  toolName: string;
  onToolNameChange: (v: string) => void;
  search: string;
  onSearchChange: (v: string) => void;
  toolNames: string[];
}) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Select value={sessionId} onValueChange={(v) => v && onSessionChange(v)}>
        <SelectTrigger className="w-[320px]">
          <SelectValue placeholder="All sessions" />
        </SelectTrigger>
        <SelectContent className="w-[380px]">
          <SelectItem value="all">All sessions</SelectItem>
          {[...sessions]
            .sort(
              (a, b) =>
                new Date(b.started_at).getTime() -
                new Date(a.started_at).getTime(),
            )
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
                  <span className="text-muted-foreground text-xs">
                    {new Date(s.started_at).toLocaleDateString()}
                  </span>
                </span>
              </SelectItem>
            ))}
        </SelectContent>
      </Select>

      {viewMode === 'timeline' && (
        <Select
          value={eventType}
          onValueChange={(v) => v && onEventTypeChange(v as EventTypeFilter)}
        >
          <SelectTrigger className="w-[160px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All events</SelectItem>
            <SelectItem value="refinement">Refinements</SelectItem>
            <SelectItem value="tool_decision">Tool decisions</SelectItem>
            <SelectItem value="verification">Verifications</SelectItem>
          </SelectContent>
        </Select>
      )}

      <Select
        value={decision}
        onValueChange={(v) => v && onDecisionChange(v as DecisionFilter)}
      >
        <SelectTrigger className="w-[140px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All decisions</SelectItem>
          <SelectItem value="allow">Allow</SelectItem>
          <SelectItem value="deny">Deny</SelectItem>
          <SelectItem value="passthrough">Passthrough</SelectItem>
        </SelectContent>
      </Select>

      <Select value={toolName} onValueChange={(v) => v && onToolNameChange(v)}>
        <SelectTrigger className="w-[140px]">
          <SelectValue placeholder="All tools" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All tools</SelectItem>
          {toolNames.map((t) => (
            <SelectItem key={t} value={t}>
              {t}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Input
        placeholder="Search..."
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        className="w-[200px]"
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main History page
// ---------------------------------------------------------------------------

type ViewMode = 'timeline' | 'tool_decisions';

export function History({ selectedSessionId }: { selectedSessionId?: string }) {
  const sessions = useSessions();
  const { fetchSessions } = useActions();

  // Load sessions on mount
  useEffect(() => {
    fetchSessions();
  }, [fetchSessions]);

  // View mode
  const [viewMode, setViewMode] = useState<ViewMode>('timeline');

  // Filters
  const [sessionId, setSessionId] = useState(selectedSessionId || 'all');
  const [eventType, setEventType] = useState<EventTypeFilter>('all');
  const [decision, setDecision] = useState<DecisionFilter>('all');
  const [toolName, setToolName] = useState('all');
  const [search, setSearch] = useState('');

  // Data
  const [toolDecisions, setToolDecisions] = useState<ToolDecision[]>([]);
  const [timeline, setTimeline] = useState<TimelineEvent[]>([]);
  const [loading, setLoading] = useState(true);

  // Fetch tool decisions
  const fetchDecisions = useCallback(async () => {
    try {
      const data =
        sessionId !== 'all'
          ? await api.sessionToolDecisions(sessionId, 500)
          : await api.toolDecisions(500);
      setToolDecisions(data);
    } catch {
      setToolDecisions([]);
    }
  }, [sessionId]);

  // Fetch timeline
  const fetchTimeline = useCallback(async () => {
    if (sessionId === 'all') {
      // Need a session for timeline; show tool decisions instead
      setTimeline([]);
      return;
    }
    try {
      const data = await api.sessionTimeline(sessionId, 200);
      setTimeline(data);
    } catch {
      setTimeline([]);
    }
  }, [sessionId]);

  useEffect(() => {
    setLoading(true);
    Promise.all([fetchDecisions(), fetchTimeline()]).finally(() =>
      setLoading(false),
    );
  }, [fetchDecisions, fetchTimeline]);

  // Auto-select active session if none specified
  useEffect(() => {
    if (sessionId === 'all' && sessions.length > 0) {
      const active = sessions.find((s) => s.status === 'active');
      if (active) setSessionId(active.id);
    }
  }, [sessions, sessionId]);

  // Extract unique tool names for filter
  const toolNames = useMemo(() => {
    const names = new Set(toolDecisions.map((d) => d.tool_name));
    return Array.from(names).sort();
  }, [toolDecisions]);

  // Filter tool decisions
  const filteredDecisions = useMemo(() => {
    return toolDecisions.filter((d) => {
      if (decision !== 'all' && d.hook_decision !== decision) return false;
      if (toolName !== 'all' && d.tool_name !== toolName) return false;
      if (
        search &&
        !d.tool_input_summary?.toLowerCase().includes(search.toLowerCase()) &&
        !d.hook_reason?.toLowerCase().includes(search.toLowerCase()) &&
        !d.tool_name.toLowerCase().includes(search.toLowerCase())
      )
        return false;
      return true;
    });
  }, [toolDecisions, decision, toolName, search]);

  // Filter timeline
  const filteredTimeline = useMemo(() => {
    return timeline.filter((e) => {
      if (eventType !== 'all' && e.event_type !== eventType) return false;
      if (decision !== 'all' && e.status !== decision) return false;
      if (
        search &&
        !e.summary?.toLowerCase().includes(search.toLowerCase()) &&
        !e.detail?.toLowerCase().includes(search.toLowerCase())
      )
        return false;
      return true;
    });
  }, [timeline, eventType, decision, search]);

  // Pick which view to show
  const showTimeline = viewMode === 'timeline' && sessionId !== 'all';

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">History</h1>
        <div className="flex gap-2">
          <button
            onClick={() => setViewMode('timeline')}
            className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
              viewMode === 'timeline'
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted text-muted-foreground hover:text-foreground'
            }`}
          >
            Timeline
          </button>
          <button
            onClick={() => setViewMode('tool_decisions')}
            className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
              viewMode === 'tool_decisions'
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted text-muted-foreground hover:text-foreground'
            }`}
          >
            Tool Decisions
          </button>
        </div>
      </div>

      {showTimeline ? (
        <TimelineStats timeline={filteredTimeline} />
      ) : (
        <ToolDecisionStats decisions={filteredDecisions} />
      )}

      <Separator />

      <Filters
        sessions={sessions}
        sessionId={sessionId}
        onSessionChange={setSessionId}
        viewMode={viewMode}
        eventType={eventType}
        onEventTypeChange={setEventType}
        decision={decision}
        onDecisionChange={setDecision}
        toolName={toolName}
        onToolNameChange={setToolName}
        search={search}
        onSearchChange={setSearch}
        toolNames={toolNames}
      />

      <Card>
        <CardHeader className="pb-0">
          <CardTitle className="text-lg">
            {showTimeline
              ? `Timeline (${filteredTimeline.length})`
              : `Tool Decisions (${filteredDecisions.length})`}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : showTimeline ? (
            filteredTimeline.length === 0 ? (
              <p className="text-muted-foreground p-4 text-sm">
                No events match the current filters.
              </p>
            ) : (
              <div style={{ height: 'calc(100vh - 28rem)', minHeight: 0 }}>
                <LegendList
                  data={filteredTimeline}
                  keyExtractor={(item) => item.id}
                  renderItem={({ item }) => <TimelineRow event={item} />}
                  getItemType={(item) => item.event_type}
                  estimatedItemSize={72}
                  recycleItems
                  style={{ height: '100%' }}
                />
              </div>
            )
          ) : filteredDecisions.length === 0 ? (
            <p className="text-muted-foreground p-4 text-sm">
              No tool decisions match the current filters.
            </p>
          ) : (
            <div style={{ height: 'calc(100vh - 28rem)', minHeight: 0 }}>
              <LegendList
                data={filteredDecisions}
                keyExtractor={(item) => String(item.id)}
                renderItem={({ item }) => <ToolDecisionRow d={item} />}
                estimatedItemSize={80}
                recycleItems
                style={{ height: '100%' }}
              />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
