import { useState, useCallback, useRef } from 'react';
import { useAppSelector } from '../store/hooks';
import { selectAccessToken } from '../store/slices/authSlice';
import type { SourceRef, QueryMeta, RagQueryRequest } from '../types/rag';

interface SSESourcesEvent {
  sources: SourceRef[];
  metadata: Partial<QueryMeta>;
}

interface SSETokenEvent {
  text: string;
}

interface SSEDoneEvent {
  metadata: QueryMeta;
}

interface SSEErrorEvent {
  error: string;
}

interface StreamState {
  isStreaming: boolean;
  answer: string;
  sources: SourceRef[];
  metadata: QueryMeta | null;
  error: string | null;
}

export function useRAGStream() {
  const [state, setState] = useState<StreamState>({
    isStreaming: false,
    answer: '',
    sources: [],
    metadata: null,
    error: null,
  });
  const abortRef = useRef<AbortController | null>(null);
  const accessToken = useAppSelector(selectAccessToken);

  const streamQuery = useCallback(async (request: RagQueryRequest) => {
    // Cancel any in-flight request
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    setState({
      isStreaming: true,
      answer: '',
      sources: [],
      metadata: null,
      error: null,
    });

    try {
      const baseUrl = import.meta.env.VITE_BACKEND_URL || 'http://localhost:3000';
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (accessToken) {
        headers['Authorization'] = `Bearer ${accessToken}`;
      }

      const response = await fetch(`${baseUrl}/v1/rag/query/stream`, {
        method: 'POST',
        headers,
        credentials: 'include',
        body: JSON.stringify(request),
        signal: controller.signal,
      });

      if (!response.ok) {
        const text = await response.text();
        let msg = `Request failed (${response.status})`;
        try {
          const err = JSON.parse(text);
          if (err.error) msg = err.error;
        } catch { /* use default msg */ }
        setState(prev => ({ ...prev, isStreaming: false, error: msg }));
        return;
      }

      const reader = response.body?.getReader();
      if (!reader) {
        setState(prev => ({ ...prev, isStreaming: false, error: 'Streaming not supported' }));
        return;
      }

      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // Parse SSE events separated by double newlines
        const parts = buffer.split('\n\n');
        // Keep the last incomplete part in the buffer
        buffer = parts.pop() || '';

        for (const part of parts) {
          if (!part.trim()) continue;

          let eventType = '';
          let data = '';

          for (const line of part.split('\n')) {
            if (line.startsWith('event: ')) {
              eventType = line.slice(7).trim();
            } else if (line.startsWith('data: ')) {
              data = line.slice(6);
            }
          }

          if (!eventType || !data) continue;

          try {
            switch (eventType) {
              case 'sources': {
                const evt = JSON.parse(data) as SSESourcesEvent;
                setState(prev => ({
                  ...prev,
                  sources: evt.sources || [],
                }));
                break;
              }
              case 'token': {
                const evt = JSON.parse(data) as SSETokenEvent;
                setState(prev => ({
                  ...prev,
                  answer: prev.answer + evt.text,
                }));
                break;
              }
              case 'answer': {
                // Full answer for no-results case
                const evt = JSON.parse(data) as SSETokenEvent;
                setState(prev => ({
                  ...prev,
                  answer: evt.text,
                }));
                break;
              }
              case 'done': {
                const evt = JSON.parse(data) as SSEDoneEvent;
                setState(prev => ({
                  ...prev,
                  isStreaming: false,
                  metadata: evt.metadata,
                }));
                break;
              }
              case 'error': {
                const evt = JSON.parse(data) as SSEErrorEvent;
                setState(prev => ({
                  ...prev,
                  isStreaming: false,
                  error: evt.error,
                }));
                break;
              }
            }
          } catch {
            // Skip malformed events
          }
        }
      }

      // If stream ended without a done event, mark as complete
      setState(prev => {
        if (prev.isStreaming) {
          return { ...prev, isStreaming: false };
        }
        return prev;
      });
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        // User cancelled, don't update state
        return;
      }
      setState(prev => ({
        ...prev,
        isStreaming: false,
        error: err instanceof Error ? err.message : 'Stream failed',
      }));
    }
  }, [accessToken]);

  const cancel = useCallback(() => {
    abortRef.current?.abort();
    setState(prev => ({ ...prev, isStreaming: false }));
  }, []);

  return {
    streamQuery,
    cancel,
    ...state,
  };
}
