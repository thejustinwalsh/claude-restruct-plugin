import { useEffect } from 'react';
import { Route, Switch, Link, useLocation, useRoute } from 'wouter';
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
  const active = href === '/' ? isExact : isActive;

  return (
    <Link
      href={href}
      className={`text-sm ${active ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}`}
    >
      {children}
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
        </div>
      </nav>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
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
