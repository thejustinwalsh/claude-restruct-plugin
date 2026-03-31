import { useEffect, useRef } from 'react';
import type { StreamState } from '@/store';
import type { Refinement } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { XmlHighlight } from '@/components/XmlHighlight';

interface Props {
  stream: StreamState | null;
  lastRefinement: Refinement | null;
}

export function StreamingCard({ stream, lastRefinement }: Props) {
  const scrollRef = useRef<HTMLPreElement>(null);

  // Auto-scroll during streaming
  useEffect(() => {
    if (scrollRef.current && stream?.isStreaming) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [stream?.text, stream?.isStreaming]);

  // Active stream takes priority, otherwise show last refinement
  if (stream) {
    return (
      <Card className={stream.isStreaming ? 'border-primary/50' : ''}>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2 text-lg">
              {stream.isStreaming ? (
                <>
                  <span className="relative flex h-2.5 w-2.5">
                    <span className="bg-primary absolute inline-flex h-full w-full animate-ping rounded-full opacity-75" />
                    <span className="bg-primary relative inline-flex h-2.5 w-2.5 rounded-full" />
                  </span>
                  Current Refinement
                </>
              ) : stream.error ? (
                'Error'
              ) : (
                'Last Refinement'
              )}
            </CardTitle>
            <Badge variant="secondary">{stream.model}</Badge>
          </div>
          <p className="text-muted-foreground mt-1 truncate text-sm">
            {stream.rawPrompt}
          </p>
        </CardHeader>
        <CardContent>
          {stream.error ? (
            <p className="text-destructive text-sm">{stream.error}</p>
          ) : (
            <pre
              ref={scrollRef}
              className="bg-muted/50 max-h-[400px] overflow-y-auto rounded-md p-4 font-mono text-sm whitespace-pre-wrap"
            >
              <XmlHighlight
                code={
                  stream.text || (stream.isStreaming ? 'Processing...' : '')
                }
              />
              {stream.isStreaming && <span className="animate-pulse">▌</span>}
            </pre>
          )}
        </CardContent>
      </Card>
    );
  }

  // No active stream — show last refinement
  if (lastRefinement) {
    const hasOutput = lastRefinement.refined_prompt != null;
    return (
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-lg">Last Refinement</CardTitle>
            <div className="flex gap-1.5">
              {lastRefinement.status === 'pending' && (
                <Badge variant="default" className="animate-pulse">
                  pending
                </Badge>
              )}
              {lastRefinement.status === 'failed' && (
                <Badge variant="destructive">failed</Badge>
              )}
              {lastRefinement.passthrough && (
                <Badge variant="outline">passthrough</Badge>
              )}
              {lastRefinement.model && (
                <Badge variant="secondary">{lastRefinement.model}</Badge>
              )}
              {hasOutput && lastRefinement.latency_ms > 0 && (
                <Badge variant="outline">
                  {(lastRefinement.latency_ms / 1000).toFixed(1)}s
                </Badge>
              )}
            </div>
          </div>
          <p className="text-muted-foreground mt-1 truncate text-sm">
            {lastRefinement.raw_prompt}
          </p>
        </CardHeader>
        <CardContent>
          {hasOutput && lastRefinement.refined_prompt ? (
            <pre className="bg-muted/50 max-h-[400px] overflow-y-auto rounded-md p-4 font-mono text-sm whitespace-pre-wrap">
              <XmlHighlight code={lastRefinement.refined_prompt} />
            </pre>
          ) : lastRefinement.status === 'pending' ? (
            <p className="text-muted-foreground text-sm italic">
              Waiting for refinement to complete...
            </p>
          ) : lastRefinement.status === 'failed' ? (
            <p className="text-destructive text-sm italic">Refinement failed</p>
          ) : (
            <p className="text-muted-foreground text-sm italic">
              No additional context generated (passthrough)
            </p>
          )}
        </CardContent>
      </Card>
    );
  }

  // Truly empty state — no refinements at all yet
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-muted-foreground text-lg">
          No Refinements Yet
        </CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-muted-foreground text-sm">
          Send a prompt through Claude Code to see the first refinement here.
        </p>
      </CardContent>
    </Card>
  );
}
