const BASE = '/api';

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

export interface Session {
  id: string;
  project_path: string;
  started_at: string;
  ended_at: string | null;
  transcript_path: string;
  status: string;
}

export interface Refinement {
  id: number;
  session_id: string;
  project_path: string;
  raw_prompt: string;
  refined_prompt: string | null;
  input_prompt: string | null;
  llm_output: string | null;
  model: string;
  temperature: number;
  latency_ms: number;
  cache_hit: boolean;
  passthrough: boolean;
  output_valid: boolean | null;
  status: string;
  created_at: string;
}

export interface PipelineEvent {
  id: number;
  refinement_id: number;
  stage: string;
  duration_us: number;
  success: boolean;
  metadata: string;
  created_at: string;
}

export interface Metrics {
  total_sessions: number;
  active_sessions: number;
  total_refinements: number;
  cache_hits: number;
  cache_hit_rate: number;
  avg_latency_ms: number;
  passthroughs: number;
}

export interface Health {
  status: string;
  sse_clients: number;
  metrics: Metrics;
}

export interface ServerInfo {
  version: string;
  mode: string;
  db_path: string;
  plugin_id: string;
}

export interface RefinementStat {
  id: number;
  created_at: string;
  latency_ms: number;
  cache_hit: boolean;
  passthrough: boolean;
  model: string;
  prompt_words: number;
}

export interface PipelineBreakdown {
  refinement_id: number;
  created_at: string;
  stage: string;
  duration_us: number;
}

export interface DailyCount {
  date: string;
  count: number;
}

export interface SessionStat {
  id: string;
  duration_minutes: number;
  refinements: number;
}

export interface SessionMetrics {
  total_refinements: number;
  passthroughs: number;
  cache_hits: number;
  avg_latency_ms: number;
  total_verifications: number;
  verification_passes: number;
  verification_failures: number;
  duration_minutes: number;
}

export interface StatsData {
  refinements: RefinementStat[];
  pipeline: PipelineBreakdown[];
  daily: DailyCount[];
  sessions: SessionStat[];
}

export interface ActiveStream {
  refinement_id: number;
  session_id: string;
  raw_prompt: string;
  model: string;
  text: string;
  seq_end: number;
  is_streaming: boolean;
  error: string;
  started_at: string;
}

export interface VerificationEvent {
  id: number;
  session_id: string;
  refinement_id: number | null;
  scope: string;
  hook_event: string;
  event_type: 'snapshot' | 'verify';
  file_count: number | null;
  duration_us: number | null;
  cwd_input: string;
  project_dir: string;
  changed_files: string | null;
  checks_run: string | null;
  result: string | null;
  created_at: string;
}

export interface CheckRun {
  name: string;
  command: string;
  passed: boolean;
  output: string;
  duration_ms: number;
}

export interface ToolDecision {
  id: number;
  session_id: string;
  project_path: string;
  tool_name: string;
  tool_input_summary: string;
  tool_use_id: string;
  hook_decision: string;
  hook_tier: number;
  hook_reason: string;
  hook_duration_us: number;
  outcome: string;
  tool_duration_ms: number | null;
  reviewed: boolean;
  reviewed_at: string | null;
  created_at: string;
}

// --- Bootstrap & Context Selection ---

export interface BootstrapEvent {
  id: number;
  session_id: string;
  project_path: string;
  files_discovered: number;
  files_processed: number;
  total_rules: number;
  classify_status: string;
  duration_us: number;
  classify_duration_us: number | null;
  error_message: string | null;
  created_at: string;
}

export interface ContextSelection {
  id: number;
  refinement_id: number;
  doc_source: string;
  doc_hash: string;
  rules_selected: number;
  created_at: string;
}

export interface ToolDecisionStats {
  total: number;
  by_decision: Record<string, number>;
  by_tool: Record<string, number>;
  by_tier: Record<string, number>;
}

export interface TimelineEventRaw {
  id: number;
  event_type: 'refinement' | 'tool_decision' | 'verification' | 'bootstrap';
  timestamp: string;
  payload: string;
}

export interface TimelineEvent {
  id: string;
  event_type: 'refinement' | 'tool_decision' | 'verification' | 'bootstrap';
  timestamp: string;
  summary: string;
  detail: string;
  status: string;
}

export function parseTimelineEvents(raw: TimelineEventRaw[]): TimelineEvent[] {
  return raw.map((r) => {
    const p = JSON.parse(r.payload);
    let summary = '';
    let detail = '';
    let status = '';

    switch (r.event_type) {
      case 'refinement':
        summary = p.raw_prompt?.slice(0, 120) || 'Refinement';
        detail = p.passthrough
          ? 'passthrough'
          : p.cache_hit
            ? 'cached'
            : `${p.latency_ms}ms`;
        status = p.status || '';
        break;
      case 'tool_decision':
        summary = `${p.tool_name}: ${p.tool_input_summary || ''}`.slice(0, 120);
        detail = p.hook_reason || '';
        status = p.hook_decision || '';
        break;
      case 'verification': {
        const isSnapshot = p.event_type === 'snapshot';
        status = isSnapshot ? 'snapshot' : p.result || 'verify';
        if (isSnapshot) {
          summary = p.file_count
            ? `snapshot · ${p.file_count} files`
            : 'snapshot';
          detail = p.scope || '';
        } else {
          if (p.checks_run) {
            try {
              const checks = JSON.parse(p.checks_run) as {
                name: string;
                passed: boolean;
              }[];
              const failed = checks.filter((c) => !c.passed);
              if (failed.length > 0) {
                summary = `${checks.map((c) => c.name).join(', ')} · failed: ${failed.map((c) => c.name).join(', ')}`;
              } else {
                summary = `${checks.map((c) => c.name).join(', ')} · all passed`;
              }
            } catch {
              summary = 'verify';
            }
          } else {
            summary = 'verify';
          }
          detail = p.file_count ? `${p.file_count} files changed` : '';
        }
        break;
      }
      case 'bootstrap': {
        const bMs = Math.round((p.duration_us || 0) / 1000);
        summary = `bootstrap: ${p.files_discovered} files, ${p.total_rules} rules in ${bMs}ms`;
        if (p.classify_status === 'complete' && p.classify_duration_us) {
          detail = `classified in ${(p.classify_duration_us / 1_000_000).toFixed(1)}s`;
        } else {
          detail = `classify: ${p.classify_status || 'pending'}`;
        }
        status = p.classify_status || 'pending';
        break;
      }
    }

    return {
      id: `${r.event_type}-${r.id}`,
      event_type: r.event_type,
      timestamp: r.timestamp,
      summary,
      detail,
      status,
    };
  });
}

export const api = {
  info: () => fetchJSON<ServerInfo>('/info'),
  health: () => fetchJSON<Health>('/health'),
  metrics: () => fetchJSON<Metrics>('/metrics'),
  sessions: (limit = 50, offset = 0) =>
    fetchJSON<Session[]>(`/sessions?limit=${limit}&offset=${offset}`),
  session: (id: string) => fetchJSON<Session>(`/sessions/${id}`),
  sessionRefinements: (id: string) =>
    fetchJSON<Refinement[]>(`/sessions/${id}/refinements`),
  sessionStats: (id: string) =>
    fetchJSON<SessionMetrics>(`/sessions/${id}/stats`),
  refinements: (limit = 50, offset = 0) =>
    fetchJSON<Refinement[]>(`/refinements?limit=${limit}&offset=${offset}`),
  refinement: (id: number) =>
    fetchJSON<{
      refinement: Refinement;
      events: PipelineEvent[];
      verifications: VerificationEvent[];
    }>(`/refinements/${id}`),
  refinementEvents: (id: number) =>
    fetchJSON<PipelineEvent[]>(`/refinements/${id}/events`),
  stats: (limit = 200) => fetchJSON<StatsData>(`/stats?limit=${limit}`),
  streamBuffer: (id: number) => fetchJSON<ActiveStream>(`/stream/buffer/${id}`),
  streamActive: () => fetchJSON<ActiveStream[]>('/stream/active'),

  // Tool decisions
  toolDecisions: (limit = 100, offset = 0) =>
    fetchJSON<ToolDecision[]>(
      `/tool-decisions?limit=${limit}&offset=${offset}`,
    ),
  toolDecisionStats: () =>
    fetchJSON<ToolDecisionStats>('/tool-decisions/stats'),
  sessionToolDecisions: (id: string, limit = 100, offset = 0) =>
    fetchJSON<ToolDecision[]>(
      `/sessions/${id}/tool-decisions?limit=${limit}&offset=${offset}`,
    ),

  // Verifications
  sessionVerifications: (id: string, limit = 200, offset = 0) =>
    fetchJSON<VerificationEvent[]>(
      `/sessions/${id}/verifications?limit=${limit}&offset=${offset}`,
    ),

  // Bootstrap & context
  sessionBootstrap: (id: string) =>
    fetchJSON<BootstrapEvent | null>(`/sessions/${id}/bootstrap`),
  refinementContextSelections: (id: number) =>
    fetchJSON<ContextSelection[]>(`/refinements/${id}/context-selections`),

  // Timeline
  sessionTimeline: async (id: string, limit = 200, offset = 0) => {
    const raw = await fetchJSON<TimelineEventRaw[]>(
      `/sessions/${id}/timeline?limit=${limit}&offset=${offset}`,
    );
    return parseTimelineEvents(raw);
  },
};
