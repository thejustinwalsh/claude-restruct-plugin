import { useApi } from '@/hooks/useApi';
import { api } from '@/api/client';
import type { Session } from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

function SessionRow({ s, onClick }: { s: Session; onClick: () => void }) {
  const started = new Date(s.started_at).toLocaleString();
  const projectName = s.project_path.split('/').pop() || s.project_path;

  return (
    <button
      onClick={onClick}
      className="hover:bg-muted/50 w-full border-b px-4 py-3 text-left transition-colors"
    >
      <div className="mb-1 flex items-center justify-between">
        <span className="text-sm font-medium">{projectName}</span>
        <Badge variant={s.status === 'active' ? 'default' : 'secondary'}>
          {s.status}
        </Badge>
      </div>
      <p className="text-muted-foreground text-xs">{s.project_path}</p>
      <p className="text-muted-foreground mt-0.5 text-xs">
        Started: {started}
        {s.ended_at && ` — Ended: ${new Date(s.ended_at).toLocaleString()}`}
      </p>
      <p className="text-muted-foreground mt-0.5 font-mono text-xs">
        ID: {s.id.slice(0, 16)}
      </p>
    </button>
  );
}

export function Sessions({
  onSelectSession,
}: {
  onSelectSession: (id: string) => void;
}) {
  const { data: sessions, loading } = useApi<Session[]>(
    () => api.sessions(),
    [],
  );

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Sessions</h1>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">All Sessions</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading && (
            <p className="text-muted-foreground p-4 text-sm">Loading...</p>
          )}
          {sessions && sessions.length === 0 && (
            <p className="text-muted-foreground p-4 text-sm">
              No sessions yet.
            </p>
          )}
          {sessions?.map((s) => (
            <SessionRow
              key={s.id}
              s={s}
              onClick={() => onSelectSession(s.id)}
            />
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
