import { useEffect, useState } from 'react';
import { useActions } from '@/store';
import { api } from '@/api/client';
import type { Session, Refinement } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

export function SessionDetail({
  id,
  onBack,
  onSelectRefinement,
}: {
  id: string;
  onBack: () => void;
  onSelectRefinement: (id: number) => void;
}) {
  const [session, setSession] = useState<Session | null>(null);
  const [refinements, setRefinements] = useState<Refinement[]>([]);
  const { fetchSessionRefinements } = useActions();

  useEffect(() => {
    api
      .session(id)
      .then(setSession)
      .catch(() => {});
    fetchSessionRefinements(id).then(setRefinements);
  }, [id, fetchSessionRefinements]);

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <button
          onClick={onBack}
          className="text-muted-foreground hover:text-foreground text-sm"
        >
          &larr; Sessions
        </button>
        <h1 className="text-2xl font-bold">Session</h1>
        {session && (
          <Badge
            variant={session.status === 'active' ? 'default' : 'secondary'}
          >
            {session.status}
          </Badge>
        )}
      </div>

      {session && (
        <Card>
          <CardContent className="space-y-1 pt-6 text-sm">
            <p>
              <span className="text-muted-foreground">ID:</span>{' '}
              <code className="text-xs">{session.id}</code>
            </p>
            <p>
              <span className="text-muted-foreground">Project:</span>{' '}
              {session.project_path}
            </p>
            <p>
              <span className="text-muted-foreground">Started:</span>{' '}
              {new Date(session.started_at).toLocaleString()}
            </p>
            {session.ended_at && (
              <p>
                <span className="text-muted-foreground">Ended:</span>{' '}
                {new Date(session.ended_at).toLocaleString()}
              </p>
            )}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            Refinements ({refinements.length})
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {refinements.length === 0 && (
            <p className="text-muted-foreground p-4 text-sm">
              No refinements in this session.
            </p>
          )}
          {refinements.map((r) => (
            <button
              key={r.id}
              onClick={() => onSelectRefinement(r.id)}
              className="hover:bg-muted/50 w-full border-b px-4 py-3 text-left transition-colors"
            >
              <div className="mb-1 flex items-center justify-between">
                <span className="text-muted-foreground text-xs">
                  {new Date(r.created_at).toLocaleTimeString()}
                </span>
                <div className="flex gap-1.5">
                  {r.cache_hit && <Badge variant="secondary">cached</Badge>}
                  {r.passthrough && (
                    <Badge variant="outline">passthrough</Badge>
                  )}
                  {!r.passthrough && !r.cache_hit && (
                    <Badge>{(r.latency_ms / 1000).toFixed(1)}s</Badge>
                  )}
                </div>
              </div>
              <p className="truncate text-sm">{r.raw_prompt}</p>
            </button>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
