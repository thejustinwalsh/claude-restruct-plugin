import { useEffect } from 'react';
import { Route, Switch, Link, useLocation, useRoute } from 'wouter';
import { useConnected } from '@/store';
import '@/store'; // side-effect: initializes SSE bridge + periodic sync
import { Dashboard } from '@/pages/Dashboard';
import { Sessions } from '@/pages/Sessions';
import { RefinementDetail } from '@/pages/RefinementDetail';
import { Stats } from '@/pages/Stats';
import { History } from '@/pages/History';
import { api } from '@/api/client';
import type { ServerInfo } from '@/api/client';
import { useState } from 'react';

function NavLink({ href, children }: { href: string; children: React.ReactNode }) {
  const [isActive] = useRoute(href === '/' ? '/' : `${href}/:rest*`);
  const [isExact] = useRoute(href);
  const active = href === '/' ? isExact : isActive || isExact;

  return (
    <Link
      href={href}
      className={`relative py-3 text-sm transition-colors ${active ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}`}
    >
      {children}
      {active && (
        <span className="bg-foreground absolute inset-x-0 -bottom-px h-0.5 rounded-full" />
      )}
    </Link>
  );
}

function DashboardRoute() {
  const [, navigate] = useLocation();
  return (
    <Dashboard onSelectRefinement={(id) => navigate(`/refinements/${id}`)} />
  );
}

function SessionsRoute({ id }: { id?: string }) {
  const [, navigate] = useLocation();
  return (
    <Sessions
      selectedSessionId={id}
      onSelectSession={(sid) => navigate(`/sessions/${sid}`)}
      onSelectRefinement={(rid) => navigate(`/refinements/${rid}`)}
    />
  );
}

function RefinementDetailRoute({ id }: { id: string }) {
  return (
    <RefinementDetail
      id={parseInt(id, 10)}
      onBack={() => history.back()}
    />
  );
}

function LiveIndicator() {
  const connected = useConnected();
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs ${connected ? 'text-muted-foreground' : 'text-red-500'}`}>
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500 animate-pulse'}`} />
      {connected ? 'Live' : 'Disconnected'}
    </span>
  );
}

function App() {
  const [info, setInfo] = useState<ServerInfo | null>(null);

  useEffect(() => {
    api.info().then(setInfo).catch(() => {});
  }, []);

  return (
    <div className="bg-background flex min-h-screen flex-col">
      <nav className="bg-card border-b">
        <div className="mx-auto flex max-w-6xl items-center gap-6 px-4 py-3">
          <Link href="/" className="text-lg font-bold">restruct</Link>
          <NavLink href="/">Dashboard</NavLink>
          <NavLink href="/sessions">Sessions</NavLink>
          <NavLink href="/history">History</NavLink>
          <NavLink href="/stats">Stats</NavLink>
          <span className="ml-auto"><LiveIndicator /></span>
        </div>
      </nav>

      <main className="mx-auto flex w-full max-w-6xl flex-1 flex-col px-4 py-6">
        <Switch>
          <Route path="/" component={DashboardRoute} />
          <Route path="/sessions">
            {() => <SessionsRoute />}
          </Route>
          <Route path="/sessions/:id">
            {(params) => <SessionsRoute id={params.id} />}
          </Route>
          <Route path="/refinements/:id">
            {(params) => <RefinementDetailRoute id={params.id} />}
          </Route>
          <Route path="/history">
            {() => <History />}
          </Route>
          <Route path="/history/:id">
            {(params) => <History selectedSessionId={params.id} />}
          </Route>
          <Route path="/stats" component={Stats} />
          <Route>
            <DashboardRoute />
          </Route>
        </Switch>
      </main>

      {info && (
        <footer className="border-t bg-card text-muted-foreground">
          <div className="mx-auto flex max-w-6xl items-center gap-4 px-4 py-2 font-mono text-xs">
            <span className={info.mode === 'debug' ? 'text-yellow-500' : 'text-green-500'}>
              {info.mode}
            </span>
            <span>v{info.version}</span>
            <span className="truncate" title={info.db_path}>{info.db_path}</span>
            <span className="ml-auto">{info.plugin_id}</span>
          </div>
        </footer>
      )}
    </div>
  );
}

export default App;
