import { useEffect, useRef, useCallback, useState } from 'react';
import type { Refinement } from '@/api/client';

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

type SSEEvent =
  | { type: 'refinement:new'; data: Refinement }
  | { type: 'refinement:stream-start'; data: StreamStart }
  | { type: 'refinement:streaming'; data: StreamToken }
  | { type: 'refinement:stream-end'; data: StreamEnd }
  | { type: 'refinement:stream-error'; data: StreamError }
  | { type: 'connected'; data: { clients: number } };

export function useSSE(onEvent: (evt: SSEEvent) => void) {
  const [connected, setConnected] = useState(false);
  const cbRef = useRef(onEvent);
  useEffect(() => {
    cbRef.current = onEvent;
  });

  useEffect(() => {
    const es = new EventSource('/api/events');

    const listen = (type: string) => {
      es.addEventListener(type, (e) => {
        cbRef.current({
          type,
          data: JSON.parse((e as MessageEvent).data),
        } as SSEEvent);
      });
    };

    es.addEventListener('connected', (e) => {
      setConnected(true);
      cbRef.current({
        type: 'connected',
        data: JSON.parse((e as MessageEvent).data),
      });
    });

    listen('refinement:new');
    listen('refinement:stream-start');
    listen('refinement:streaming');
    listen('refinement:stream-end');
    listen('refinement:stream-error');

    es.onerror = () => setConnected(false);

    return () => es.close();
  }, []);

  return { connected };
}

export function useLiveFeed() {
  const [events, setEvents] = useState<Refinement[]>([]);

  const handleEvent = useCallback((evt: SSEEvent) => {
    if (evt.type === 'refinement:new') {
      setEvents((prev) => [evt.data, ...prev].slice(0, 100));
    }
  }, []);

  const sse = useSSE(handleEvent);
  return { events, ...sse };
}

export interface StreamState {
  refinementId: number;
  sessionId: string;
  rawPrompt: string;
  model: string;
  text: string;
  isStreaming: boolean;
  error: string | null;
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
  return { stream, ...sse };
}
