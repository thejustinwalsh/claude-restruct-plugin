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
  model: string;
  temperature: number;
  latency_ms: number;
  cache_hit: boolean;
  passthrough: boolean;
  output_valid: boolean | null;
  created_at: string;
}

export interface PipelineEvent {
  id: number;
  refinement_id: number;
  stage: string;
  duration_ms: number;
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

export interface RefinementStat {
  id: number;
  created_at: string;
  latency_ms: number;
  cache_hit: boolean;
  passthrough: boolean;
  model: string;
}

export interface PipelineBreakdown {
  refinement_id: number;
  created_at: string;
  stage: string;
  duration_ms: number;
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

export interface StatsData {
  refinements: RefinementStat[];
  pipeline: PipelineBreakdown[];
  daily: DailyCount[];
  sessions: SessionStat[];
}

export const api = {
  health: () => fetchJSON<Health>('/health'),
  metrics: () => fetchJSON<Metrics>('/metrics'),
  sessions: (limit = 50, offset = 0) =>
    fetchJSON<Session[]>(`/sessions?limit=${limit}&offset=${offset}`),
  session: (id: string) => fetchJSON<Session>(`/sessions/${id}`),
  sessionRefinements: (id: string) =>
    fetchJSON<Refinement[]>(`/sessions/${id}/refinements`),
  refinements: (limit = 50, offset = 0) =>
    fetchJSON<Refinement[]>(`/refinements?limit=${limit}&offset=${offset}`),
  refinement: (id: number) =>
    fetchJSON<{ refinement: Refinement; events: PipelineEvent[] }>(
      `/refinements/${id}`,
    ),
  refinementEvents: (id: number) =>
    fetchJSON<PipelineEvent[]>(`/refinements/${id}/events`),
  stats: (limit = 200) => fetchJSON<StatsData>(`/stats?limit=${limit}`),
};
