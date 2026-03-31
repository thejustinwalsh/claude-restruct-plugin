import { useEffect } from 'react';
import { Route, Switch, Link, useLocation, useRoute } from 'wouter';
import '@/store'; // side-effect: initializes SSE bridge + periodic sync
import { Dashboard } from '@/pages/Dashboard';
import { Sessions } from '@/pages/Sessions';
import { SessionDetail } from '@/pages/SessionDetail';
import { RefinementDetail } from '@/pages/RefinementDetail';
import { Stats } from '@/pages/Stats';
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

function SessionsRoute() {
  const [, navigate] = useLocation();
  return (
    <Sessions onSelectSession={(id) => navigate(`/sessions/${id}`)} />
  );
}

function SessionDetailRoute({ id }: { id: string }) {
  const [, navigate] = useLocation();
  return (
    <SessionDetail
      id={id}
      onBack={() => navigate('/sessions')}
      onSelectRefinement={(id) => navigate(`/refinements/${id}`)}
    />
  );
}

function RefinementDetailRoute({ id }: { id: string }) {
  const [, navigate] = useLocation();
  return (
    <RefinementDetail
      id={parseInt(id, 10)}
      onBack={() => navigate('/')}
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
          <NavLink href="/stats">Stats</NavLink>
        </div>
      </nav>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
        <Switch>
          <Route path="/" component={DashboardRoute} />
          <Route path="/sessions" component={SessionsRoute} />
          <Route path="/sessions/:id">
            {(params) => <SessionDetailRoute id={params.id} />}
          </Route>
          <Route path="/refinements/:id">
            {(params) => <RefinementDetailRoute id={params.id} />}
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
