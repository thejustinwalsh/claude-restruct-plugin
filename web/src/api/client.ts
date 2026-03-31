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
};
