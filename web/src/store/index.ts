import { create } from 'zustand';
import { useShallow } from 'zustand/shallow';
import {
  subscribeSSE,
  subscribeConnected,
  subscribeReconnect,
  teardown,
} from '@/hooks/useSSE';
import type { SSEEvent } from '@/hooks/useSSE';
import { api } from '@/api/client';
import type {
  Metrics,
  Refinement,
  PipelineEvent,
  VerificationEvent,
  Session,
  StatsData,
} from '@/api/client';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface StreamState {
  refinementId: number;
  sessionId: string;
  rawPrompt: string;
  model: string;
  text: string;
  isStreaming: boolean;
  error: string | null;
  startedAt: number;
}

interface RefinementDetail {
  refinement: Refinement;
  events: PipelineEvent[];
  verifications: VerificationEvent[];
}

// ---------------------------------------------------------------------------
// Store shape
// ---------------------------------------------------------------------------

interface AppState {
  // SSE connection
  connected: boolean;

  // Streaming
  stream: StreamState | null;

  // Refinements: merged from DB + live SSE events
  refinements: Map<number, Refinement>;
  refinementDetails: Map<number, RefinementDetail>;

  // Metrics
  metrics: Metrics | null;

  // Sessions
  sessions: Session[];

  // Stats
  stats: StatsData | null;

  // Bootstrap
  bootstrapEvents: Map<string, import('@/api/client').BootstrapEvent>;

  // Actions
  setConnected: (v: boolean) => void;

  // Stream actions
  streamStart: (
    id: number,
    sessionId: string,
    rawPrompt: string,
    model: string,
  ) => void;
  streamAppend: (id: number, tokens: string) => void;
  streamEnd: (id: number) => void;
  streamError: (id: number, error: string) => void;

  // Refinement actions
  upsertRefinement: (
    r: Refinement,
    events?: PipelineEvent[] | null,
    verifications?: VerificationEvent[] | null,
  ) => void;
  setRefinementDetail: (id: number, detail: RefinementDetail) => void;

  // Fetch actions (talk to API, update store)
  fetchRefinements: () => Promise<void>;
  fetchRefinement: (id: number) => Promise<void>;
  fetchMetrics: () => Promise<void>;
  fetchSessions: () => Promise<void>;
  fetchSessionRefinements: (sessionId: string) => Promise<Refinement[]>;
  fetchStats: () => Promise<void>;

  // DB sync — reconcile stale pending entries
  syncFromDB: () => Promise<void>;
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

export const useAppStore = create<AppState>((set, get) => ({
  connected: false,
  stream: null,
  refinements: new Map(),
  refinementDetails: new Map(),
  metrics: null,
  sessions: [],
  stats: null,
  bootstrapEvents: new Map(),

  setConnected: (v) => set({ connected: v }),

  // -- Stream --

  streamStart: (id, sessionId, rawPrompt, model) => {
    // Add pending entry to refinements map
    const pending: Refinement = {
      id,
      session_id: sessionId,
      project_path: '',
      raw_prompt: rawPrompt,
      refined_prompt: null,
      input_prompt: null,
      llm_output: null,
      model,
      temperature: 0,
      latency_ms: 0,
      cache_hit: false,
      passthrough: false,
      output_valid: null,
      status: 'pending',
      created_at: new Date().toISOString(),
    };
    const refs = new Map(get().refinements);
    refs.set(id, pending);

    // Also populate refinementDetails so the detail page can render immediately
    const details = new Map(get().refinementDetails);
    details.set(id, { refinement: pending, events: [], verifications: [] });

    set({
      stream: {
        refinementId: id,
        sessionId,
        rawPrompt,
        model,
        text: '',
        isStreaming: true,
        error: null,
        startedAt: Date.now(),
      },
      refinements: refs,
      refinementDetails: details,
    });
  },

  streamAppend: (id, tokens) =>
    set((s) => {
      if (!s.stream || s.stream.refinementId !== id) return s;
      return { stream: { ...s.stream, text: s.stream.text + tokens } };
    }),

  streamEnd: (id) =>
    set((s) => {
      if (!s.stream || s.stream.refinementId !== id) return s;
      return { stream: { ...s.stream, isStreaming: false } };
    }),

  streamError: (id, error) =>
    set((s) => {
      if (!s.stream || s.stream.refinementId !== id) return s;
      return { stream: { ...s.stream, isStreaming: false, error } };
    }),

  // -- Refinements --

  upsertRefinement: (r, events, verifications) => {
    const refs = new Map(get().refinements);
    refs.set(r.id, r);
    const next: Partial<AppState> = { refinements: refs };

    // If events provided, update detail cache too
    if (events) {
      const details = new Map(get().refinementDetails);
      details.set(r.id, {
        refinement: r,
        events,
        verifications: verifications ?? [],
      });
      next.refinementDetails = details;
    } else {
      // Update the refinement in existing detail if cached
      const existing = get().refinementDetails.get(r.id);
      if (existing) {
        const details = new Map(get().refinementDetails);
        details.set(r.id, { ...existing, refinement: r });
        next.refinementDetails = details;
      }
    }

    // Clear streaming state if this refinement is done
    const stream = get().stream;
    if (stream && stream.refinementId === r.id && r.status !== 'pending') {
      next.stream = null;
    }

    set(next);
  },

  setRefinementDetail: (id, detail) => {
    const details = new Map(get().refinementDetails);
    details.set(id, detail);
    const refs = new Map(get().refinements);
    refs.set(id, detail.refinement);
    set({ refinementDetails: details, refinements: refs });
  },

  // -- API fetches --

  fetchRefinements: async () => {
    try {
      const list = await api.refinements(50);
      const refs = new Map(get().refinements);
      for (const r of list) {
        const existing = refs.get(r.id);
        // DB version wins unless we have a fresher pending from SSE
        if (
          !existing ||
          existing.status === 'pending' ||
          r.status !== 'pending'
        ) {
          refs.set(r.id, r);
        }
      }
      set({ refinements: refs });
    } catch {
      /* ignore */
    }
  },

  fetchRefinement: async (id) => {
    try {
      const data = await api.refinement(id);
      get().setRefinementDetail(id, data);
    } catch {
      /* ignore */
    }
  },

  fetchMetrics: async () => {
    try {
      const m = await api.metrics();
      set({ metrics: m });
    } catch {
      /* ignore */
    }
  },

  fetchSessions: async () => {
    try {
      const s = await api.sessions();
      set({ sessions: s });
    } catch {
      /* ignore */
    }
  },

  fetchSessionRefinements: async (sessionId) => {
    try {
      const refs = await api.sessionRefinements(sessionId);
      const map = new Map(get().refinements);
      for (const r of refs) map.set(r.id, r);
      set({ refinements: map });
      return refs;
    } catch {
      return [];
    }
  },

  fetchStats: async () => {
    try {
      const s = await api.stats();
      set({ stats: s });
    } catch {
      /* ignore */
    }
  },

  // -- DB sync --

  syncFromDB: async () => {
    try {
      const [dbRecent, metrics, freshSessions] = await Promise.all([
        api.refinements(50),
        api.metrics(),
        api.sessions(),
      ]);
      const refs = new Map(get().refinements);
      const dbMap = new Map(dbRecent.map((r) => [r.id, r]));

      // Update existing entries with DB state
      for (const [id, r] of refs) {
        const dbVersion = dbMap.get(id);
        if (r.status === 'pending') {
          if (dbVersion && dbVersion.status !== 'pending') {
            refs.set(id, dbVersion);
          } else {
            // Prune pending older than 5 min
            const age = Date.now() - new Date(r.created_at).getTime();
            if (age > 5 * 60 * 1000) refs.delete(id);
          }
        }
      }
      // Add any new DB entries we don't have
      for (const r of dbRecent) {
        if (!refs.has(r.id)) refs.set(r.id, r);
      }

      // Clear stale stream
      const stream = get().stream;
      let nextStream = stream;
      if (stream && stream.isStreaming) {
        const dbVersion = dbMap.get(stream.refinementId);
        if (dbVersion && dbVersion.status !== 'pending') {
          nextStream = null;
        } else if (Date.now() - stream.startedAt > 5 * 60 * 1000) {
          nextStream = null;
        }
      }

      set({
        refinements: refs,
        metrics,
        sessions: freshSessions,
        stream: nextStream,
      });
    } catch {
      /* ignore */
    }
  },
}));

// ---------------------------------------------------------------------------
// Selectors (used with useShallow to prevent unnecessary re-renders)
// ---------------------------------------------------------------------------

/** Sorted refinements list (most recent first), max 100 */
export function useRefinementsList(): Refinement[] {
  return useAppStore(
    useShallow((s) =>
      [...s.refinements.values()].sort((a, b) => b.id - a.id).slice(0, 100),
    ),
  );
}

/** Single refinement detail (refinement + pipeline events) */
export function useRefinementDetail(id: number): RefinementDetail | null {
  return useAppStore((s) => s.refinementDetails.get(id) ?? null);
}

/** Single refinement from the flat map (available immediately for pending entries) */
export function useRefinement(id: number): Refinement | null {
  return useAppStore((s) => s.refinements.get(id) ?? null);
}

/** Active stream state */
export function useStream(): StreamState | null {
  return useAppStore((s) => s.stream);
}

/** SSE connected flag */
export function useConnected(): boolean {
  return useAppStore((s) => s.connected);
}

/** Metrics */
export function useMetrics(): Metrics | null {
  return useAppStore((s) => s.metrics);
}

/** Sessions list */
export function useSessions(): Session[] {
  return useAppStore(useShallow((s) => s.sessions));
}

/** Stats data */
export function useStats(): StatsData | null {
  return useAppStore((s) => s.stats);
}

export function useBootstrapEvent(
  sessionId: string,
): import('@/api/client').BootstrapEvent | undefined {
  return useAppStore((s) => s.bootstrapEvents.get(sessionId));
}

/** Store actions (stable references, never cause re-render) */
export function useActions() {
  return useAppStore(
    useShallow((s) => ({
      fetchRefinements: s.fetchRefinements,
      fetchRefinement: s.fetchRefinement,
      fetchMetrics: s.fetchMetrics,
      fetchSessions: s.fetchSessions,
      fetchSessionRefinements: s.fetchSessionRefinements,
      fetchStats: s.fetchStats,
      syncFromDB: s.syncFromDB,
    })),
  );
}

// ---------------------------------------------------------------------------
// SSE → Store bridge (module-level side effect, runs once on import)
// ---------------------------------------------------------------------------

function handleSSEEvent(evt: SSEEvent) {
  const s = useAppStore.getState();
  switch (evt.type) {
    case 'refinement:stream-start':
      s.streamStart(
        evt.data.refinement_id,
        evt.data.session_id,
        evt.data.raw_prompt,
        evt.data.model,
      );
      break;
    case 'refinement:streaming':
      s.streamAppend(evt.data.refinement_id, evt.data.tokens);
      break;
    case 'refinement:stream-end':
      s.streamEnd(evt.data.refinement_id);
      break;
    case 'refinement:stream-error':
      s.streamError(evt.data.refinement_id, evt.data.error);
      break;
    case 'refinement:input': {
      // Step 2: LLM input prompt is ready (before inference starts)
      const refId = evt.data.refinement_id;
      const refs = new Map(s.refinements);
      const existing = refs.get(refId);
      if (existing) {
        refs.set(refId, { ...existing, input_prompt: evt.data.input_prompt });
        const details = new Map(s.refinementDetails);
        const detail = details.get(refId);
        if (detail) {
          details.set(refId, {
            ...detail,
            refinement: {
              ...detail.refinement,
              input_prompt: evt.data.input_prompt,
            },
          });
        }
        useAppStore.setState({ refinements: refs, refinementDetails: details });
      }
      break;
    }
    case 'refinement:complete': {
      // Step 4: Final context + pipeline timings
      const refId = evt.data.refinement_id;
      const refs = new Map(s.refinements);
      const existing = refs.get(refId);
      if (existing) {
        const updated = {
          ...existing,
          refined_prompt: evt.data.refined_prompt,
          llm_output: evt.data.llm_output,
          latency_ms: evt.data.latency_ms,
          status: 'complete' as const,
        };
        refs.set(refId, updated);
        const details = new Map(s.refinementDetails);
        const detail = details.get(refId);
        if (detail) {
          const events = evt.data.timings.map(
            (t: { stage: string; duration_us: number }, i: number) => ({
              id: -(i + 1), // temp negative IDs until poller replaces
              refinement_id: refId,
              stage: t.stage,
              duration_us: t.duration_us,
              success: true,
              metadata: '',
              created_at: new Date().toISOString(),
            }),
          );
          details.set(refId, { ...detail, refinement: updated, events });
        }
        useAppStore.setState({
          refinements: refs,
          refinementDetails: details,
          stream: null,
        });
      }
      break;
    }
    case 'refinement:new':
      s.upsertRefinement(
        evt.data.refinement,
        evt.data.events,
        evt.data.verifications,
      );
      s.fetchMetrics();
      break;
    case 'verification:new': {
      // Append verification event to the correct refinement's detail cache
      const refId = evt.data.refinement_id;
      if (refId) {
        const details = new Map(s.refinementDetails);
        const existing = details.get(refId);
        if (existing) {
          details.set(refId, {
            ...existing,
            verifications: [...existing.verifications, evt.data],
          });
          useAppStore.setState({ refinementDetails: details });
        }
      }
      break;
    }
    case 'bootstrap:new':
    case 'bootstrap:classify': {
      const be = evt.data;
      const bmap = new Map(s.bootstrapEvents);
      bmap.set(be.session_id, be);
      useAppStore.setState({ bootstrapEvents: bmap });
      break;
    }
  }
}

function handleReconnect() {
  // After SSE reconnect, refresh data that might have been missed
  const s = useAppStore.getState();
  s.fetchRefinements();
  s.fetchMetrics();
}

// Wire up SSE and periodic sync. Guarded for HMR — only runs once.
let _initialized = false;
let _syncInterval: ReturnType<typeof setInterval> | null = null;
let _unsubSSE: (() => void) | null = null;
let _unsubConnected: (() => void) | null = null;
let _unsubReconnect: (() => void) | null = null;

function initStore() {
  if (_initialized) return;
  _initialized = true;

  _unsubSSE = subscribeSSE(handleSSEEvent);
  _unsubConnected = subscribeConnected((connected) =>
    useAppStore.setState({ connected }),
  );
  _unsubReconnect = subscribeReconnect(handleReconnect);

  // Initial data load
  useAppStore.getState().fetchRefinements();
  useAppStore.getState().fetchMetrics();

  // Periodic DB sync (every 15s)
  _syncInterval = setInterval(
    () => useAppStore.getState().syncFromDB(),
    15_000,
  );
}

initStore();

// HMR cleanup — Vite calls this before re-executing the module
if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    _initialized = false;
    if (_unsubSSE) _unsubSSE();
    if (_unsubConnected) _unsubConnected();
    if (_unsubReconnect) _unsubReconnect();
    if (_syncInterval) clearInterval(_syncInterval);
    teardown();
  });
}
