import { useEffect, useReducer, useState, useMemo, useCallback } from 'react';
import { LegendList } from '@legendapp/list/react';
import { useSessions, useActions } from '@/store';
import { useSSE } from '@/hooks/useSSE';
import { api, parseTimelineEvents } from '@/api/client';
import type {
  ToolDecision,
  TimelineEvent,
  TimelineEventRaw,
  VerificationEvent,
  CheckRun,
  BootstrapEvent,
  ContextSelection,
} from '@/api/client';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { SearchIcon, SearchXIcon, ListIcon } from 'lucide-react';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const ROW_HEIGHT = 33; // must match --row-h in CSS below

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

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

function tierLabel(tier: number): string {
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

function timelineStatusVariant(
  status: string,
): 'default' | 'destructive' | 'secondary' | 'outline' {
  if (status === 'deny' || status === 'fail' || status === 'failed')
    return 'destructive';
  if (status === 'allow' || status === 'pass') return 'default';
  if (status === 'snapshot') return 'secondary';
  return 'outline';
}

function FormattedTime({ iso }: { iso: string }) {
  const d = new Date(iso);
  const time = d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  const month = d.toLocaleString(undefined, { month: 'short' });
  return (
    <>
      <span className="text-foreground/70">{time}</span>{' '}
      <span className="text-muted-foreground/50">
        {month} {d.getDate()}
      </span>
    </>
  );
}

// ---------------------------------------------------------------------------
// Inline stats bar
// ---------------------------------------------------------------------------

function Stat({
  label,
  value,
  warn,
  maxValue,
}: {
  label: string;
  value: string | number;
  warn?: boolean;
  maxValue?: number;
}) {
  const minWidth =
    maxValue != null ? `${String(maxValue).length}ch` : undefined;
  return (
    <span className="flex items-baseline gap-1">
      <span className="text-muted-foreground text-xs">{label}</span>
      <span
        className={`inline-block text-left font-mono text-sm font-medium tabular-nums ${warn ? 'text-red-600' : ''}`}
        style={minWidth ? { minWidth } : undefined}
      >
        {value}
      </span>
    </span>
  );
}

function ToolDecisionStats({
  decisions,
  all,
}: {
  decisions: ToolDecision[];
  all: ToolDecision[];
}) {
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

  // Per-category unfiltered maxes
  const maxTotal = all.length;
  const maxAllowed = all.filter((d) => d.hook_decision === 'allow').length;
  const maxDenied = all.filter((d) => d.hook_decision === 'deny').length;
  const maxPass = all.filter((d) => d.hook_decision === 'passthrough').length;

  return (
    <div className="flex flex-wrap items-center gap-4">
      <Stat label="total" value={total} maxValue={maxTotal} />
      <Stat label="approved" value={allowed} maxValue={maxAllowed} />
      <Stat
        label="denied"
        value={denied}
        warn={denied > 0}
        maxValue={maxDenied}
      />
      <Stat label="passthrough" value={passthrough} maxValue={maxPass} />
      <Stat label="avg" value={formatMicros(avgUs)} />
    </div>
  );
}

function TimelineStats({
  timeline,
  all,
}: {
  timeline: TimelineEvent[];
  all: TimelineEvent[];
}) {
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

  // Per-category unfiltered maxes
  const maxTotal = all.length;
  const maxRef = all.filter((e) => e.event_type === 'refinement').length;
  const maxTool = all.filter((e) => e.event_type === 'tool_decision').length;
  const maxVer = all.filter((e) => e.event_type === 'verification').length;
  const maxFail = all.filter(
    (e) => e.status === 'deny' || e.status === 'fail' || e.status === 'failed',
  ).length;

  return (
    <div className="flex flex-wrap items-center gap-4">
      <Stat label="events" value={total} maxValue={maxTotal} />
      <Stat label="refinements" value={refinements} maxValue={maxRef} />
      <Stat label="tool use" value={toolDecs} maxValue={maxTool} />
      <Stat label="verifications" value={verifications} maxValue={maxVer} />
      <Stat
        label="failures"
        value={failures}
        warn={failures > 0}
        maxValue={maxFail}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Decision filter chips
// ---------------------------------------------------------------------------

const DECISION_OPTIONS: { value: DecisionFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'allow', label: 'Allow' },
  { value: 'deny', label: 'Deny' },
  { value: 'passthrough', label: 'Pass' },
];

function FilterChips({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <div className="inline-flex h-7 items-center gap-0 rounded-md border p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2 py-0.5 text-xs font-medium transition-colors ${
            value === o.value
              ? 'bg-foreground text-background'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Event type filter chips (timeline only)
// ---------------------------------------------------------------------------

const EVENT_TYPE_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'refinement', label: 'Refinements' },
  { value: 'tool_decision', label: 'Tools' },
  { value: 'verification', label: 'Verify' },
  { value: 'bootstrap', label: 'Context' },
];

// ---------------------------------------------------------------------------
// Shared flex row primitives
// ---------------------------------------------------------------------------

const ROW_CLS = 'flex h-[33px] items-center gap-0 px-3 text-sm';
const HEADER_ROW = `${ROW_CLS} border-b text-xs font-medium text-foreground`;
const DATA_ROW = `${ROW_CLS} border-b transition-colors hover:bg-muted/50`;

// Column widths — tool use
const TD_COL = {
  time: 'w-[120px] shrink-0',
  tool: 'w-[124px] shrink-0',
  decision: 'w-[88px] shrink-0 text-center',
  tier: 'w-[36px] shrink-0 text-center',
  latency: 'w-[64px] shrink-0',
  outcome: 'w-[72px] shrink-0 text-center',
  detail: 'min-w-0 flex-1',
};

// Column widths — timeline
const TL_COL = {
  time: 'w-[120px] shrink-0',
  type: 'w-[96px] shrink-0',
  status: 'w-[80px] shrink-0 text-center',
  detail: 'min-w-0 flex-1',
};

// ---------------------------------------------------------------------------
// Tool decision row
// ---------------------------------------------------------------------------

function ToolDecisionHeader() {
  return (
    <div className={HEADER_ROW}>
      <span className={TD_COL.time}>Time</span>
      <span className={TD_COL.tool}>Tool</span>
      <span className={TD_COL.decision}>Decision</span>
      <span className={TD_COL.tier}>Tier</span>
      <span className={TD_COL.latency}>Latency</span>
      <span className={TD_COL.outcome}>Outcome</span>
      <span className={TD_COL.detail}>Detail</span>
    </div>
  );
}

function ToolDecisionRow({ d }: { d: ToolDecision }) {
  return (
    <div className={DATA_ROW}>
      <span
        className={`${TD_COL.time} text-muted-foreground font-mono text-xs`}
      >
        <FormattedTime iso={d.created_at} />
      </span>
      <span className={`${TD_COL.tool} truncate font-mono text-xs`}>
        {d.tool_name}
      </span>
      <span className={TD_COL.decision}>
        <Badge
          variant={decisionVariant(d.hook_decision)}
          className="text-[10px]"
        >
          {d.hook_decision}
        </Badge>
      </span>
      <span className={TD_COL.tier}>
        <span className={`text-xs font-medium ${tierLabel(d.hook_tier)}`}>
          T{d.hook_tier}
        </span>
      </span>
      <span
        className={`${TD_COL.latency} text-muted-foreground font-mono text-xs`}
      >
        {d.hook_duration_us > 0 ? formatMicros(d.hook_duration_us) : '\u2014'}
      </span>
      <span className={TD_COL.outcome}>
        {d.outcome && d.outcome !== 'pending' && (
          <Badge
            variant={d.outcome === 'executed' ? 'default' : 'destructive'}
            className="text-[10px]"
          >
            {d.outcome}
          </Badge>
        )}
      </span>
      <span
        className={`${TD_COL.detail} text-muted-foreground truncate text-xs`}
      >
        {d.tool_input_summary || d.hook_reason}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Timeline row
// ---------------------------------------------------------------------------

function TimelineHeader() {
  return (
    <div className={HEADER_ROW}>
      <span className={TL_COL.time}>Time</span>
      <span className={TL_COL.type}>Type</span>
      <span className={TL_COL.status}>Status</span>
      <span className={TL_COL.detail}>Detail</span>
    </div>
  );
}

function TimelineRow({ event }: { event: TimelineEvent }) {
  return (
    <div className={DATA_ROW}>
      <span
        className={`${TL_COL.time} text-muted-foreground font-mono text-xs`}
      >
        <FormattedTime iso={event.timestamp} />
      </span>
      <span className={`${TL_COL.type} text-xs font-medium`}>
        {event.event_type.replace('_', ' ')}
      </span>
      <span className={TL_COL.status}>
        {event.status && (
          <Badge
            variant={timelineStatusVariant(event.status)}
            className="text-[10px]"
          >
            {event.status}
          </Badge>
        )}
      </span>
      <span className={`${TL_COL.detail} truncate text-xs`}>
        <span>
          {event.summary.includes('failed:') ? (
            <>
              {event.summary.split('failed:')[0]}
              <span className="text-red-600">
                failed:{event.summary.split('failed:')[1]}
              </span>
            </>
          ) : (
            event.summary
          )}
        </span>
        {event.detail && (
          <span className="text-muted-foreground ml-2">{event.detail}</span>
        )}
      </span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Verification helpers
// ---------------------------------------------------------------------------

function parseChecks(raw: string | null): CheckRun[] {
  if (!raw) return [];
  try {
    return JSON.parse(raw);
  } catch {
    return [];
  }
}

function verificationResultVariant(
  result: string | null,
): 'default' | 'destructive' | 'outline' {
  if (result === 'pass') return 'default';
  if (result === 'fail') return 'destructive';
  return 'outline';
}

// Column widths — verifications
const VF_COL = {
  time: 'w-[120px] shrink-0',
  type: 'w-[72px] shrink-0 text-center',
  hook: 'w-[128px] shrink-0',
  scope: 'w-[72px] shrink-0',
  result: 'w-[64px] shrink-0 text-center',
  files: 'w-[52px] shrink-0 text-center',
  latency: 'w-[72px] shrink-0',
  checks: 'min-w-0 flex-1',
};

function VerificationHeader() {
  return (
    <div className={HEADER_ROW}>
      <span className={VF_COL.time}>Time</span>
      <span className={VF_COL.type}>Type</span>
      <span className={VF_COL.hook}>Hook</span>
      <span className={VF_COL.scope}>Scope</span>
      <span className={VF_COL.result}>Result</span>
      <span className={VF_COL.files}>Files</span>
      <span className={VF_COL.latency}>Latency</span>
      <span className={VF_COL.checks}>Checks</span>
    </div>
  );
}

function VerificationRow({ v }: { v: VerificationEvent }) {
  const checks = parseChecks(v.checks_run);
  const passed = checks.filter((c) => c.passed);
  const failed = checks.filter((c) => !c.passed);

  return (
    <div className={DATA_ROW}>
      <span
        className={`${VF_COL.time} text-muted-foreground font-mono text-xs`}
      >
        <FormattedTime iso={v.created_at} />
      </span>
      <span className={VF_COL.type}>
        <Badge
          variant={v.event_type === 'verify' ? 'default' : 'secondary'}
          className="text-[10px]"
        >
          {v.event_type}
        </Badge>
      </span>
      <span className={`${VF_COL.hook} text-muted-foreground text-xs`}>
        {v.hook_event}
      </span>
      <span className={`${VF_COL.scope} text-xs`}>{v.scope}</span>
      <span className={VF_COL.result}>
        {v.result && (
          <Badge
            variant={verificationResultVariant(v.result)}
            className="text-[10px]"
          >
            {v.result}
          </Badge>
        )}
      </span>
      <span className={`${VF_COL.files} text-muted-foreground text-xs`}>
        {v.file_count != null ? v.file_count : '\u2014'}
      </span>
      <span
        className={`${VF_COL.latency} text-muted-foreground font-mono text-xs`}
      >
        {v.duration_us ? formatMicros(v.duration_us) : '\u2014'}
      </span>
      <span className={`${VF_COL.checks} truncate text-xs`}>
        {checks.length > 0 ? (
          <>
            {failed.length > 0 ? (
              <span>
                {passed.length}/{checks.length} passed
                {' \u00B7 '}
                <span className="text-red-600">
                  failed: {failed.map((c) => c.name).join(', ')}
                </span>
              </span>
            ) : (
              <span className="text-muted-foreground">
                {checks.length}/{checks.length} passed
              </span>
            )}
          </>
        ) : v.event_type === 'snapshot' ? (
          <span className="text-muted-foreground">snapshot captured</span>
        ) : null}
      </span>
    </div>
  );
}

function VerificationStats({
  verifications,
  all,
}: {
  verifications: VerificationEvent[];
  all: VerificationEvent[];
}) {
  const total = verifications.length;
  const snapshots = verifications.filter(
    (v) => v.event_type === 'snapshot',
  ).length;
  const checks = verifications.filter((v) => v.event_type === 'verify').length;
  const passed = verifications.filter((v) => v.result === 'pass').length;
  const failed = verifications.filter((v) => v.result === 'fail').length;

  const maxTotal = all.length;
  const maxSnap = all.filter((v) => v.event_type === 'snapshot').length;
  const maxChecks = all.filter((v) => v.event_type === 'verify').length;
  const maxPassed = all.filter((v) => v.result === 'pass').length;
  const maxFailed = all.filter((v) => v.result === 'fail').length;

  return (
    <div className="flex flex-wrap items-center gap-4">
      <Stat label="total" value={total} maxValue={maxTotal} />
      <Stat label="snapshots" value={snapshots} maxValue={maxSnap} />
      <Stat label="checks" value={checks} maxValue={maxChecks} />
      <Stat label="passed" value={passed} maxValue={maxPassed} />
      <Stat
        label="failed"
        value={failed}
        warn={failed > 0}
        maxValue={maxFailed}
      />
    </div>
  );
}

type VerificationTypeFilter = 'all' | 'snapshot' | 'verify';
type VerificationResultFilter = 'all' | 'pass' | 'fail';

const VF_TYPE_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'snapshot', label: 'Snapshots' },
  { value: 'verify', label: 'Checks' },
];

const VF_RESULT_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'pass', label: 'Pass' },
  { value: 'fail', label: 'Fail' },
];

// ---------------------------------------------------------------------------
// Context tab — bootstrap events + context selections
// ---------------------------------------------------------------------------

// Unified row type for the context tab
interface ContextRow {
  id: string;
  type: 'bootstrap' | 'selection';
  timestamp: string;
  data: BootstrapEvent | ContextSelection;
}

function buildContextRows(
  bootstraps: BootstrapEvent[],
  selections: ContextSelection[],
): ContextRow[] {
  const rows: ContextRow[] = [];
  for (const b of bootstraps) {
    rows.push({
      id: `b-${b.id}`,
      type: 'bootstrap',
      timestamp: b.created_at,
      data: b,
    });
  }
  for (const s of selections) {
    rows.push({
      id: `s-${s.id}`,
      type: 'selection',
      timestamp: s.created_at,
      data: s,
    });
  }
  rows.sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime(),
  );
  return rows;
}

const CX_COL = {
  time: 'w-[120px] shrink-0',
  type: 'w-[88px] shrink-0 text-center',
  status: 'w-[80px] shrink-0 text-center',
  files: 'w-[52px] shrink-0 text-center',
  rules: 'w-[52px] shrink-0 text-center',
  latency: 'w-[80px] shrink-0',
  detail: 'min-w-0 flex-1',
};

function ContextHeader() {
  return (
    <div className={HEADER_ROW}>
      <span className={CX_COL.time}>Time</span>
      <span className={CX_COL.type}>Event</span>
      <span className={CX_COL.status}>Status</span>
      <span className={CX_COL.files}>Files</span>
      <span className={CX_COL.rules}>Rules</span>
      <span className={CX_COL.latency}>Duration</span>
      <span className={CX_COL.detail}>Detail</span>
    </div>
  );
}

function ContextRow({ row }: { row: ContextRow }) {
  if (row.type === 'bootstrap') {
    const b = row.data as BootstrapEvent;
    return (
      <div className={DATA_ROW}>
        <span
          className={`${CX_COL.time} text-muted-foreground font-mono text-xs`}
        >
          <FormattedTime iso={b.created_at} />
        </span>
        <span className={CX_COL.type}>
          <Badge variant="secondary" className="text-[10px]">
            bootstrap
          </Badge>
        </span>
        <span className={CX_COL.status}>
          <Badge
            variant={
              b.classify_status === 'complete'
                ? 'default'
                : b.classify_status === 'failed'
                  ? 'destructive'
                  : 'outline'
            }
            className="text-[10px]"
          >
            {b.classify_status}
          </Badge>
        </span>
        <span className={`${CX_COL.files} text-xs tabular-nums`}>
          {b.files_processed}
        </span>
        <span className={`${CX_COL.rules} text-xs tabular-nums`}>
          {b.total_rules}
        </span>
        <span
          className={`${CX_COL.latency} text-muted-foreground font-mono text-xs`}
        >
          {formatMicros(b.duration_us)}
          {b.classify_duration_us != null && b.classify_duration_us > 0 && (
            <span className="text-muted-foreground/60">
              {' '}
              + {formatMicros(b.classify_duration_us)}
            </span>
          )}
        </span>
        <span
          className={`${CX_COL.detail} text-muted-foreground truncate text-xs`}
        >
          {b.error_message ||
            `${b.files_discovered} discovered, ${b.files_processed} processed`}
        </span>
      </div>
    );
  }

  const s = row.data as ContextSelection;
  return (
    <div className={DATA_ROW}>
      <span
        className={`${CX_COL.time} text-muted-foreground font-mono text-xs`}
      >
        <FormattedTime iso={s.created_at} />
      </span>
      <span className={CX_COL.type}>
        <Badge variant="default" className="text-[10px]">
          selection
        </Badge>
      </span>
      <span className={CX_COL.status}>{'\u2014'}</span>
      <span className={CX_COL.files}>{'\u2014'}</span>
      <span className={`${CX_COL.rules} text-xs tabular-nums`}>
        {s.rules_selected > 0 ? s.rules_selected : '\u2014'}
      </span>
      <span className={CX_COL.latency}>{'\u2014'}</span>
      <span className={`${CX_COL.detail} truncate font-mono text-xs`}>
        {s.doc_source}
        <span className="text-muted-foreground ml-2 font-sans">
          refinement #{s.refinement_id}
        </span>
      </span>
    </div>
  );
}

function ContextStats({
  bootstraps,
  selections,
}: {
  bootstraps: BootstrapEvent[];
  selections: ContextSelection[];
}) {
  const classified = bootstraps.filter(
    (b) => b.classify_status === 'complete',
  ).length;
  const failed = bootstraps.filter(
    (b) => b.classify_status === 'failed',
  ).length;
  const uniqueDocs = new Set(selections.map((s) => s.doc_source)).size;

  return (
    <div className="flex flex-wrap items-center gap-4">
      <Stat label="bootstraps" value={bootstraps.length} />
      <Stat label="classified" value={classified} />
      <Stat label="failed" value={failed} warn={failed > 0} />
      <Stat label="selections" value={selections.length} />
      <Stat label="unique docs" value={uniqueDocs} />
    </div>
  );
}

type ContextTypeFilter = 'all' | 'bootstrap' | 'selection';

const CX_TYPE_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'bootstrap', label: 'Bootstrap' },
  { value: 'selection', label: 'Selections' },
];

// ---------------------------------------------------------------------------
// Main History page
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Filter state — useReducer + URL sync
// ---------------------------------------------------------------------------

type TabValue = 'timeline' | 'tool_decisions' | 'verifications' | 'context';
const VALID_TABS: TabValue[] = [
  'timeline',
  'tool_decisions',
  'verifications',
  'context',
];

interface FilterState {
  session: string;
  tab: TabValue;
  eventType: string;
  decision: DecisionFilter;
  toolName: string;
  vfType: VerificationTypeFilter;
  vfResult: VerificationResultFilter;
  cxType: ContextTypeFilter;
  search: string;
}

type FilterAction = { [K in keyof FilterState]?: FilterState[K] };

function filtersReducer(state: FilterState, action: FilterAction): FilterState {
  return { ...state, ...action };
}

// Param keys map state fields to URL param names
const PARAM_KEYS: Record<keyof FilterState, string> = {
  session: 'session',
  tab: 'tab',
  eventType: 'eventType',
  decision: 'decision',
  toolName: 'tool',
  vfType: 'vfType',
  vfResult: 'vfResult',
  cxType: 'cxType',
  search: 'q',
};

function readFiltersFromURL(selectedSessionId?: string): FilterState {
  const p = new URLSearchParams(window.location.search);
  return {
    session: selectedSessionId || p.get('session') || '',
    tab: VALID_TABS.includes(p.get('tab') as TabValue)
      ? (p.get('tab') as TabValue)
      : 'timeline',
    eventType: p.get('eventType') || 'all',
    decision: (p.get('decision') || 'all') as DecisionFilter,
    toolName: p.get('tool') || 'all',
    vfType: (p.get('vfType') || 'all') as VerificationTypeFilter,
    vfResult: (p.get('vfResult') || 'all') as VerificationResultFilter,
    cxType: (p.get('cxType') || 'all') as ContextTypeFilter,
    search: p.get('q') || '',
  };
}

function syncFiltersToURL(state: FilterState) {
  const p = new URLSearchParams();
  for (const [field, paramKey] of Object.entries(PARAM_KEYS)) {
    const v = state[field as keyof FilterState];
    if (v && v !== 'all' && v !== '' && v !== 'timeline') {
      p.set(paramKey, v);
    }
  }
  // Always write session even if it matches default
  if (state.session) p.set('session', state.session);
  const qs = p.toString();
  const url = window.location.pathname + (qs ? `?${qs}` : '');
  window.history.replaceState(null, '', url);
}

// ---------------------------------------------------------------------------
// Main History page
// ---------------------------------------------------------------------------

export function History({ selectedSessionId }: { selectedSessionId?: string }) {
  const sessions = useSessions();
  const { fetchSessions } = useActions();

  useEffect(() => {
    fetchSessions();
  }, [fetchSessions]);

  // All filter state in one reducer — URL syncs on every change
  const [filters, dispatch] = useReducer(
    filtersReducer,
    selectedSessionId,
    (sid) => readFiltersFromURL(sid),
  );

  // Derive effective session: filters.session > most recent active > most recent
  const sessionId = useMemo(() => {
    if (filters.session) return filters.session;
    if (sessions.length === 0) return '';
    const sorted = [...sessions].sort((a, b) => {
      if (a.status === 'active' && b.status !== 'active') return -1;
      if (b.status === 'active' && a.status !== 'active') return 1;
      return (
        new Date(b.started_at).getTime() - new Date(a.started_at).getTime()
      );
    });
    return sorted[0].id;
  }, [filters.session, sessions]);

  // Sync all state to URL whenever anything changes (including auto-selected session)
  const stateWithSession = useMemo(
    () => ({ ...filters, session: sessionId }),
    [filters, sessionId],
  );
  useEffect(() => {
    syncFiltersToURL(stateWithSession);
  }, [stateWithSession]);

  // Convenience setters
  const setSessionId = useCallback((v: string) => dispatch({ session: v }), []);
  const setTab = useCallback(
    (v: string) => dispatch({ tab: v as TabValue }),
    [],
  );
  const setEventType = useCallback(
    (v: string) => dispatch({ eventType: v }),
    [],
  );
  const setDecision = useCallback(
    (v: string) => dispatch({ decision: v as DecisionFilter }),
    [],
  );
  const setToolName = useCallback((v: string) => dispatch({ toolName: v }), []);
  const setVfType = useCallback(
    (v: string) => dispatch({ vfType: v as VerificationTypeFilter }),
    [],
  );
  const setVfResult = useCallback(
    (v: string) => dispatch({ vfResult: v as VerificationResultFilter }),
    [],
  );
  const setCxType = useCallback(
    (v: string) => dispatch({ cxType: v as ContextTypeFilter }),
    [],
  );
  const setSearch = useCallback((v: string) => dispatch({ search: v }), []);

  const {
    tab,
    eventType,
    decision,
    toolName,
    vfType,
    vfResult,
    cxType,
    search,
  } = filters;

  // Data
  const [toolDecisions, setToolDecisions] = useState<ToolDecision[]>([]);
  const [timeline, setTimeline] = useState<TimelineEvent[]>([]);
  const [verifications, setVerifications] = useState<VerificationEvent[]>([]);
  const [bootstrapEvents, setBootstrapEvents] = useState<BootstrapEvent[]>([]);
  const [contextSelections, setContextSelections] = useState<
    ContextSelection[]
  >([]);
  const [loading, setLoading] = useState(true);

  const fetchDecisions = useCallback(async () => {
    if (!sessionId) return;
    try {
      const data = await api.sessionToolDecisions(sessionId, 500);
      setToolDecisions(data);
    } catch {
      setToolDecisions([]);
    }
  }, [sessionId]);

  const fetchTimeline = useCallback(async () => {
    if (!sessionId) return;
    try {
      const data = await api.sessionTimeline(sessionId, 200);
      setTimeline(data);
    } catch {
      setTimeline([]);
    }
  }, [sessionId]);

  const fetchVerifications = useCallback(async () => {
    if (!sessionId) return;
    try {
      const data = await api.sessionVerifications(sessionId, 500);
      setVerifications(data);
    } catch {
      setVerifications([]);
    }
  }, [sessionId]);

  const fetchContext = useCallback(async () => {
    if (!sessionId) return;
    try {
      const [bEvents, cSels] = await Promise.all([
        api.sessionBootstrapEvents(sessionId),
        api.sessionContextSelections(sessionId),
      ]);
      setBootstrapEvents(bEvents);
      setContextSelections(cSels);
    } catch {
      setBootstrapEvents([]);
      setContextSelections([]);
    }
  }, [sessionId]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      await Promise.all([
        fetchDecisions(),
        fetchTimeline(),
        fetchVerifications(),
        fetchContext(),
      ]);
      if (!cancelled) setLoading(false);
    };
    load();
    return () => {
      cancelled = true;
    };
  }, [fetchDecisions, fetchTimeline, fetchVerifications, fetchContext]);

  // SSE: append new events in real-time
  useSSE((evt) => {
    if (evt.type === 'tool-decision:new') {
      const td = evt.data;
      if (td.session_id !== sessionId) return;
      setToolDecisions((prev) => {
        const idx = prev.findIndex((d) => d.tool_use_id === td.tool_use_id);
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = td;
          return next;
        }
        return [td, ...prev];
      });
      // Also append to timeline
      const raw: TimelineEventRaw = {
        id: td.id,
        event_type: 'tool_decision',
        timestamp: td.created_at,
        payload: JSON.stringify(td),
      };
      const [parsed] = parseTimelineEvents([raw]);
      setTimeline((prev) => [parsed, ...prev]);
    }
    if (evt.type === 'verification:new') {
      const v = evt.data;
      if (v.session_id !== sessionId) return;
      setVerifications((prev) => [v, ...prev]);
      // Also append to timeline
      const raw: TimelineEventRaw = {
        id: v.id,
        event_type: 'verification',
        timestamp: v.created_at,
        payload: JSON.stringify(v),
      };
      const [parsed] = parseTimelineEvents([raw]);
      setTimeline((prev) => [parsed, ...prev]);
    }
    if (evt.type === 'refinement:new') {
      const r = evt.data.refinement;
      if (r.session_id !== sessionId) return;
      const raw: TimelineEventRaw = {
        id: r.id,
        event_type: 'refinement',
        timestamp: r.created_at,
        payload: JSON.stringify(r),
      };
      const [parsed] = parseTimelineEvents([raw]);
      setTimeline((prev) => [parsed, ...prev]);
    }
    if (evt.type === 'bootstrap:new' || evt.type === 'bootstrap:classify') {
      const b = evt.data;
      if (b.session_id !== sessionId) return;
      setBootstrapEvents((prev) => {
        const idx = prev.findIndex((e) => e.id === b.id);
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = b;
          return next;
        }
        return [b, ...prev];
      });
    }
  });

  // Unique tool names
  const toolNames = useMemo(() => {
    const names = new Set(toolDecisions.map((d) => d.tool_name));
    return Array.from(names).sort();
  }, [toolDecisions]);

  // Filtered tool decisions
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

  // Filtered timeline (only event type + search, no decision filter)
  const filteredTimeline = useMemo(() => {
    return timeline.filter((e) => {
      if (eventType !== 'all' && e.event_type !== eventType) return false;
      if (
        search &&
        !e.summary?.toLowerCase().includes(search.toLowerCase()) &&
        !e.detail?.toLowerCase().includes(search.toLowerCase())
      )
        return false;
      return true;
    });
  }, [timeline, eventType, search]);

  // Filtered verifications
  const filteredVerifications = useMemo(() => {
    return verifications.filter((v) => {
      if (vfType !== 'all' && v.event_type !== vfType) return false;
      if (vfResult !== 'all' && v.result !== vfResult) return false;
      if (
        search &&
        !v.scope?.toLowerCase().includes(search.toLowerCase()) &&
        !v.checks_run?.toLowerCase().includes(search.toLowerCase())
      )
        return false;
      return true;
    });
  }, [verifications, vfType, vfResult, search]);

  // Context rows (bootstrap + selections merged)
  const contextRows = useMemo(() => {
    return buildContextRows(bootstrapEvents, contextSelections);
  }, [bootstrapEvents, contextSelections]);

  const filteredContextRows = useMemo(() => {
    return contextRows.filter((r) => {
      if (cxType !== 'all' && r.type !== cxType) return false;
      if (search) {
        const s = search.toLowerCase();
        if (r.type === 'bootstrap') {
          const b = r.data as BootstrapEvent;
          if (
            !b.error_message?.toLowerCase().includes(s) &&
            !b.classify_status.toLowerCase().includes(s)
          )
            return false;
        } else {
          const sel = r.data as ContextSelection;
          if (!sel.doc_source.toLowerCase().includes(s)) return false;
        }
      }
      return true;
    });
  }, [contextRows, cxType, search]);

  /*
   * Table height strategy: flex chain fills available space.
   *
   * The wrapper (min-h-0 flex-1) gets its height from the flex parent.
   * The table is a flex column that fills the wrapper (h-full).
   * The list body is flex-1 min-h-0, taking remaining space after the header.
   *
   * Previously used CSS container queries (100cqb) but container-type: size
   * on flex children resolves block dimension to 0 in Chrome — the flex
   * algorithm hasn't assigned a definite height before containment evaluates.
   */

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {/* Header + session selector */}
      <div className="flex shrink-0 items-center justify-between gap-3">
        <h1 className="text-lg font-medium">History</h1>
        <Select value={sessionId} onValueChange={(v) => v && setSessionId(v)}>
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

      {/* Tabs for Timeline / Tool Use */}
      <Tabs
        value={tab}
        onValueChange={setTab}
        className="flex min-h-0 flex-1 flex-col"
      >
        <div className="flex shrink-0 items-center justify-between gap-3">
          <TabsList variant="line">
            <TabsTrigger value="timeline">
              Timeline
              <span
                className="text-muted-foreground ml-1 inline-block text-right text-xs tabular-nums"
                style={{ minWidth: `${String(timeline.length).length || 1}ch` }}
              >
                {filteredTimeline.length}
              </span>
            </TabsTrigger>
            <TabsTrigger value="tool_decisions">
              Tool Use
              <span
                className="text-muted-foreground ml-1 inline-block text-right text-xs tabular-nums"
                style={{
                  minWidth: `${String(toolDecisions.length).length || 1}ch`,
                }}
              >
                {filteredDecisions.length}
              </span>
            </TabsTrigger>
            <TabsTrigger value="verifications">
              Verifications
              <span
                className="text-muted-foreground ml-1 inline-block text-right text-xs tabular-nums"
                style={{
                  minWidth: `${String(verifications.length).length || 1}ch`,
                }}
              >
                {filteredVerifications.length}
              </span>
            </TabsTrigger>
            <TabsTrigger value="context">
              Context
              <span
                className="text-muted-foreground ml-1 inline-block text-right text-xs tabular-nums"
                style={{
                  minWidth: `${String(contextRows.length).length || 1}ch`,
                }}
              >
                {filteredContextRows.length}
              </span>
            </TabsTrigger>
          </TabsList>

          {/* Search */}
          <div className="relative">
            <SearchIcon className="text-muted-foreground absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
            <Input
              placeholder="Search..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-7 w-[180px] pl-7 text-xs"
            />
          </div>
        </div>

        {/* Timeline tab */}
        <TabsContent value="timeline" className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 py-2">
            <div className="flex items-center gap-2">
              <FilterChips
                value={eventType}
                onChange={setEventType}
                options={EVENT_TYPE_OPTIONS}
              />
            </div>
            <TimelineStats timeline={filteredTimeline} all={timeline} />
          </div>

          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
              <TimelineHeader />
              <div className="min-h-0 flex-1 overflow-hidden">
                {loading ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <ListIcon className="text-border size-8" />
                    <span className="text-xs">Loading...</span>
                  </div>
                ) : filteredTimeline.length === 0 ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <SearchXIcon className="text-border size-8" />
                    <span className="text-xs">
                      No events match the current filters
                    </span>
                  </div>
                ) : (
                  <LegendList
                    data={filteredTimeline}
                    keyExtractor={(item) => item.id}
                    renderItem={({ item }) => <TimelineRow event={item} />}
                    getItemType={(item) => item.event_type}
                    estimatedItemSize={ROW_HEIGHT}
                    recycleItems
                    style={{ height: '100%' }}
                  />
                )}
              </div>
            </div>
          </div>
        </TabsContent>

        {/* Tool Use tab */}
        <TabsContent
          value="tool_decisions"
          className="flex min-h-0 flex-1 flex-col"
        >
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 py-2">
            <div className="flex items-center gap-2">
              <FilterChips
                value={decision}
                onChange={setDecision}
                options={DECISION_OPTIONS}
              />
              {toolNames.length > 1 && (
                <Select
                  value={toolName}
                  onValueChange={(v) => v && setToolName(v)}
                >
                  <SelectTrigger size="sm" className="h-7 w-[130px] text-xs">
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
              )}
            </div>
            <ToolDecisionStats
              decisions={filteredDecisions}
              all={toolDecisions}
            />
          </div>

          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
              <ToolDecisionHeader />
              <div className="min-h-0 flex-1 overflow-hidden">
                {loading ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <ListIcon className="text-border size-8" />
                    <span className="text-xs">Loading...</span>
                  </div>
                ) : filteredDecisions.length === 0 ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <SearchXIcon className="text-border size-8" />
                    <span className="text-xs">
                      No tool decisions match the current filters
                    </span>
                  </div>
                ) : (
                  <LegendList
                    data={filteredDecisions}
                    keyExtractor={(item) => String(item.id)}
                    renderItem={({ item }) => <ToolDecisionRow d={item} />}
                    estimatedItemSize={ROW_HEIGHT}
                    recycleItems
                    style={{ height: '100%' }}
                  />
                )}
              </div>
            </div>
          </div>
        </TabsContent>

        {/* Verifications tab */}
        <TabsContent
          value="verifications"
          className="flex min-h-0 flex-1 flex-col"
        >
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 py-2">
            <div className="flex items-center gap-2">
              <FilterChips
                value={vfType}
                onChange={setVfType}
                options={VF_TYPE_OPTIONS}
              />
              <FilterChips
                value={vfResult}
                onChange={setVfResult}
                options={VF_RESULT_OPTIONS}
              />
            </div>
            <VerificationStats
              verifications={filteredVerifications}
              all={verifications}
            />
          </div>

          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
              <VerificationHeader />
              <div className="min-h-0 flex-1 overflow-hidden">
                {loading ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <ListIcon className="text-border size-8" />
                    <span className="text-xs">Loading...</span>
                  </div>
                ) : filteredVerifications.length === 0 ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <SearchXIcon className="text-border size-8" />
                    <span className="text-xs">
                      No verifications match the current filters
                    </span>
                  </div>
                ) : (
                  <LegendList
                    data={filteredVerifications}
                    keyExtractor={(item) => String(item.id)}
                    renderItem={({ item }) => <VerificationRow v={item} />}
                    estimatedItemSize={ROW_HEIGHT}
                    recycleItems
                    style={{ height: '100%' }}
                  />
                )}
              </div>
            </div>
          </div>
        </TabsContent>

        {/* Context tab */}
        <TabsContent value="context" className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 py-2">
            <div className="flex items-center gap-2">
              <FilterChips
                value={cxType}
                onChange={setCxType}
                options={CX_TYPE_OPTIONS}
              />
            </div>
            <ContextStats
              bootstraps={bootstrapEvents}
              selections={contextSelections}
            />
          </div>

          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
              <ContextHeader />
              <div className="min-h-0 flex-1 overflow-hidden">
                {loading ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <ListIcon className="text-border size-8" />
                    <span className="text-xs">Loading...</span>
                  </div>
                ) : filteredContextRows.length === 0 ? (
                  <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                    <SearchXIcon className="text-border size-8" />
                    <span className="text-xs">
                      No context events match the current filters
                    </span>
                  </div>
                ) : (
                  <LegendList
                    data={filteredContextRows}
                    keyExtractor={(item) => item.id}
                    renderItem={({ item }) => <ContextRow row={item} />}
                    getItemType={(item) => item.type}
                    estimatedItemSize={ROW_HEIGHT}
                    recycleItems
                    style={{ height: '100%' }}
                  />
                )}
              </div>
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
