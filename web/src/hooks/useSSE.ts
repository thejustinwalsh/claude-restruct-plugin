import {
  useEffect,
  useRef,
  useCallback,
  useState,
  useSyncExternalStore,
} from 'react';
import type { Refinement, PipelineEvent } from '@/api/client';
import { api } from '@/api/client';

export interface RefinementCompleteEvent {
  refinement: Refinement;
  events: PipelineEvent[] | null;
}

export interface StreamStart {
  refinement_id: number;
  session_id: string;
  raw_prompt: string;
  model: string;
}

export interface StreamToken {
  refinement_id: number;
  tokens: string;
  seq_start: number;
  seq_end: number;
}

export interface StreamEnd {
  refinement_id: number;
}

export interface StreamError {
  refinement_id: number;
  error: string;
}

export type SSEEvent =
  | { type: 'refinement:new'; data: RefinementCompleteEvent }
  | { type: 'refinement:stream-start'; data: StreamStart }
  | { type: 'refinement:streaming'; data: StreamToken }
  | { type: 'refinement:stream-end'; data: StreamEnd }
  | { type: 'refinement:stream-error'; data: StreamError }
  | { type: 'connected'; data: { clients: number } };

// ---------------------------------------------------------------------------
// Singleton SSE connection — shared across all hooks, never closed while
// the app is mounted. Reconnects with exponential backoff on failure.
// Heartbeat detection forces reconnect if the server stops sending events.
// ---------------------------------------------------------------------------

type Listener = (evt: SSEEvent) => void;
type ReconnectListener = () => void;

const listeners = new Set<Listener>();
const connectedListeners = new Set<() => void>();
const reconnectListeners = new Set<ReconnectListener>();
let eventSource: EventSource | null = null;
let sseConnected = false;
let retryTimeout: ReturnType<typeof setTimeout> | null = null;
let retryCount = 0;
let lastEventTime = 0;
let heartbeatInterval: ReturnType<typeof setInterval> | null = null;

const RETRY_BASE_MS = 1000;
const RETRY_MAX_MS = 30000;
const HEARTBEAT_TIMEOUT_MS = 45_000; // Force reconnect if no event for 45s
const HEARTBEAT_CHECK_MS = 10_000; // Check staleness every 10s

function setConnected(value: boolean) {
  if (sseConnected === value) return;
  sseConnected = value;
  for (const fn of connectedListeners) fn();
}

function dispatch(evt: SSEEvent) {
  for (const fn of listeners) fn(evt);
}

function notifyReconnect() {
  for (const fn of reconnectListeners) fn();
}

function startHeartbeatCheck() {
  if (heartbeatInterval) return;
  heartbeatInterval = setInterval(() => {
    if (
      sseConnected &&
      lastEventTime > 0 &&
      Date.now() - lastEventTime > HEARTBEAT_TIMEOUT_MS
    ) {
      // Connection appears stale — force reconnect
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
      setConnected(false);
      connect();
    }
  }, HEARTBEAT_CHECK_MS);
}

function connect() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  if (retryTimeout) {
    clearTimeout(retryTimeout);
    retryTimeout = null;
  }

  const es = new EventSource('/api/events');
  eventSource = es;

  const listen = (type: string) => {
    es.addEventListener(type, (e) => {
      lastEventTime = Date.now();
      dispatch({
        type,
        data: JSON.parse((e as MessageEvent).data),
      } as SSEEvent);
    });
  };

  es.addEventListener('connected', (e) => {
    const wasConnected = sseConnected;
    retryCount = 0;
    lastEventTime = Date.now();
    setConnected(true);
    dispatch({ type: 'connected', data: JSON.parse((e as MessageEvent).data) });
    // Notify reconnect listeners so the store can refresh data.
    // Skip the very first connection (page load) — initStore handles that.
    // wasConnected is true only if we were previously connected, meaning this
    // is a reconnect after a drop, not the initial page-load connection.
    if (wasConnected) {
      notifyReconnect();
    }
  });

  // Track heartbeat events for staleness detection (server sends every 15s)
  es.addEventListener('heartbeat', () => {
    lastEventTime = Date.now();
  });

  listen('refinement:new');
  listen('refinement:stream-start');
  listen('refinement:streaming');
  listen('refinement:stream-end');
  listen('refinement:stream-error');

  es.onerror = () => {
    setConnected(false);
    es.close();
    eventSource = null;

    // Retry with exponential backoff
    const delay = Math.min(
      RETRY_BASE_MS * Math.pow(2, retryCount),
      RETRY_MAX_MS,
    );
    retryCount++;
    retryTimeout = setTimeout(connect, delay);
  };

  startHeartbeatCheck();
}

function ensureConnection() {
  if (eventSource || retryTimeout) return;
  connect();
}

function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  ensureConnection();
  return () => {
    listeners.delete(fn);
    // Never close the connection — keep it alive for the app's lifetime.
    // The connection is cheap and reconnecting loses state.
  };
}

/** Raw subscribe for non-React consumers (e.g., Zustand store bridge). */
export function subscribeSSE(fn: Listener): () => void {
  return subscribe(fn);
}

/** Subscribe to connected state changes. Callback fires on every change. */
export function subscribeConnected(
  fn: (connected: boolean) => void,
): () => void {
  const wrapped = () => fn(sseConnected);
  connectedListeners.add(wrapped);
  ensureConnection();
  return () => {
    connectedListeners.delete(wrapped);
  };
}

/** Subscribe to reconnect events. Fires after the SSE reconnects. */
export function subscribeReconnect(fn: ReconnectListener): () => void {
  reconnectListeners.add(fn);
  return () => {
    reconnectListeners.delete(fn);
  };
}

/** Tear down for HMR. */
export function teardown() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  if (retryTimeout) {
    clearTimeout(retryTimeout);
    retryTimeout = null;
  }
  if (heartbeatInterval) {
    clearInterval(heartbeatInterval);
    heartbeatInterval = null;
  }
  listeners.clear();
  connectedListeners.clear();
  reconnectListeners.clear();
  sseConnected = false;
  lastEventTime = 0;
  retryCount = 0;
}

// ---------------------------------------------------------------------------
// useSSE — subscribe to the shared connection
// ---------------------------------------------------------------------------

export function useSSE(onEvent: (evt: SSEEvent) => void) {
  const cbRef = useRef(onEvent);
  useEffect(() => {
    cbRef.current = onEvent;
  });

  useEffect(() => {
    return subscribe((evt) => cbRef.current(evt));
  }, []);

  // Track connected state via useSyncExternalStore for consistency.
  // connectedListeners is notified on both connect and disconnect.
  const connected = useSyncExternalStore(
    (notify) => {
      connectedListeners.add(notify);
      return () => {
        connectedListeners.delete(notify);
      };
    },
    () => sseConnected,
  );

  return { connected };
}

// ---------------------------------------------------------------------------
// useStreamingTokens — tracks the active streaming refinement
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

export function useStreamingTokens() {
  const [stream, setStream] = useState<StreamState | null>(null);

  const handleEvent = useCallback((evt: SSEEvent) => {
    switch (evt.type) {
      case 'refinement:stream-start':
        setStream({
          refinementId: evt.data.refinement_id,
          sessionId: evt.data.session_id,
          rawPrompt: evt.data.raw_prompt,
          model: evt.data.model,
          text: '',
          isStreaming: true,
          error: null,
          startedAt: Date.now(),
        });
        break;

      case 'refinement:streaming':
        setStream((prev) => {
          if (!prev || prev.refinementId !== evt.data.refinement_id)
            return prev;
          return { ...prev, text: prev.text + evt.data.tokens };
        });
        break;

      case 'refinement:stream-end':
        setStream((prev) => {
          if (!prev || prev.refinementId !== evt.data.refinement_id)
            return prev;
          return { ...prev, isStreaming: false };
        });
        break;

      case 'refinement:stream-error':
        setStream((prev) => {
          if (!prev || prev.refinementId !== evt.data.refinement_id)
            return prev;
          return { ...prev, isStreaming: false, error: evt.data.error };
        });
        break;
    }
  }, []);

  const sse = useSSE(handleEvent);
  return { stream, setStream, ...sse };
}

// ---------------------------------------------------------------------------
// useLiveFeed — tracks completed refinements AND pending ones from stream-start
// ---------------------------------------------------------------------------

export function useLiveFeed() {
  const [events, setEvents] = useState<Refinement[]>([]);

  const handleEvent = useCallback((evt: SSEEvent) => {
    switch (evt.type) {
      case 'refinement:stream-start':
        // Immediately add a pending entry so the list shows the in-progress refinement
        setEvents((prev) => {
          const pending: Refinement = {
            id: evt.data.refinement_id,
            session_id: evt.data.session_id,
            project_path: '',
            raw_prompt: evt.data.raw_prompt,
            refined_prompt: null,
            input_prompt: null,
            llm_output: null,
            model: evt.data.model,
            temperature: 0,
            latency_ms: 0,
            cache_hit: false,
            passthrough: false,
            output_valid: null,
            status: 'pending',
            created_at: new Date().toISOString(),
          };
          return [pending, ...prev.filter((r) => r.id !== pending.id)].slice(
            0,
            100,
          );
        });
        break;

      case 'refinement:new': {
        // Replace pending entry with the completed one from the DB
        const ref = evt.data.refinement;
        setEvents((prev) =>
          [ref, ...prev.filter((r) => r.id !== ref.id)].slice(0, 100),
        );
        break;
      }
    }
  }, []);

  const sse = useSSE(handleEvent);
  return { events, setEvents, ...sse };
}

// ---------------------------------------------------------------------------
// useStreamForRefinement — follows a specific refinement's streaming state
// ---------------------------------------------------------------------------

export function useStreamForRefinement(
  refinementId: number | null,
): StreamState | null {
  const [stream, setStream] = useState<StreamState | null>(null);
  // Track seqEnd per refinementId to deduplicate tokens after buffer catch-up.
  // Stored as a Map so we don't need refs during render.
  const [seqEnds] = useState(() => new Map<number, number>());

  // On mount / refinementId change, fetch the current buffer from the server
  useEffect(() => {
    if (refinementId == null) return;

    let cancelled = false;
    seqEnds.set(refinementId, 0);

    api
      .streamBuffer(refinementId)
      .then((active) => {
        if (cancelled) return;
        if (active.is_streaming || active.text) {
          seqEnds.set(refinementId, active.seq_end);
          setStream({
            refinementId: active.refinement_id,
            sessionId: active.session_id,
            rawPrompt: active.raw_prompt,
            model: active.model,
            text: active.text,
            isStreaming: active.is_streaming,
            error: active.error || null,
            startedAt: new Date(active.started_at).getTime(),
          });
        }
      })
      .catch(() => {
        // No active stream for this refinement — that's fine
      });

    return () => {
      cancelled = true;
    };
  }, [refinementId, seqEnds]);

  // Subscribe to SSE for live updates, filtered to this refinementId
  const handleEvent = useCallback(
    (evt: SSEEvent) => {
      if (refinementId == null) return;

      switch (evt.type) {
        case 'refinement:stream-start':
          if (evt.data.refinement_id !== refinementId) return;
          seqEnds.set(refinementId, 0);
          setStream({
            refinementId: evt.data.refinement_id,
            sessionId: evt.data.session_id,
            rawPrompt: evt.data.raw_prompt,
            model: evt.data.model,
            text: '',
            isStreaming: true,
            error: null,
            startedAt: Date.now(),
          });
          break;

        case 'refinement:streaming':
          if (evt.data.refinement_id !== refinementId) return;
          {
            // Skip tokens we already have from the buffer catch-up
            const knownSeq = seqEnds.get(refinementId) ?? 0;
            if (evt.data.seq_end <= knownSeq) return;
            seqEnds.set(refinementId, evt.data.seq_end);
            const newTokens =
              evt.data.seq_start < knownSeq ? '' : evt.data.tokens;
            setStream((prev) => {
              if (!prev || prev.refinementId !== refinementId) return prev;
              return { ...prev, text: prev.text + newTokens };
            });
          }
          break;

        case 'refinement:stream-end':
          if (evt.data.refinement_id !== refinementId) return;
          setStream((prev) => (prev ? { ...prev, isStreaming: false } : prev));
          break;

        case 'refinement:stream-error':
          if (evt.data.refinement_id !== refinementId) return;
          setStream((prev) =>
            prev
              ? { ...prev, isStreaming: false, error: evt.data.error }
              : prev,
          );
          break;
      }
    },
    [refinementId, seqEnds],
  );

  useSSE(handleEvent);

  // Only return stream if it belongs to the current refinementId
  if (stream && refinementId != null && stream.refinementId !== refinementId) {
    return null;
  }
  return stream;
}
