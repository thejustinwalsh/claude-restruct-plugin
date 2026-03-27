import { useState } from 'react';
import { Dashboard } from '@/pages/Dashboard';
import { Sessions } from '@/pages/Sessions';
import { SessionDetail } from '@/pages/SessionDetail';
import { RefinementDetail } from '@/pages/RefinementDetail';
import { Stats } from '@/pages/Stats';

type View =
  | { page: 'dashboard' }
  | { page: 'sessions' }
  | { page: 'session'; id: string }
  | { page: 'refinement'; id: number }
  | { page: 'stats' };

function App() {
  const [view, setView] = useState<View>({ page: 'dashboard' });

  return (
    <div className="bg-background min-h-screen">
      <nav className="bg-card border-b">
        <div className="mx-auto flex max-w-6xl items-center gap-6 px-4 py-3">
          <span className="text-lg font-bold">restruct</span>
          <button
            onClick={() => setView({ page: 'dashboard' })}
            className={`text-sm ${view.page === 'dashboard' ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}`}
          >
            Dashboard
          </button>
          <button
            onClick={() => setView({ page: 'sessions' })}
            className={`text-sm ${view.page === 'sessions' || view.page === 'session' ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}`}
          >
            Sessions
          </button>
          <button
            onClick={() => setView({ page: 'stats' })}
            className={`text-sm ${view.page === 'stats' ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground'}`}
          >
            Stats
          </button>
        </div>
      </nav>

      <main className="mx-auto max-w-6xl px-4 py-6">
        {view.page === 'dashboard' && (
          <Dashboard
            onSelectRefinement={(id) => setView({ page: 'refinement', id })}
          />
        )}
        {view.page === 'sessions' && (
          <Sessions
            onSelectSession={(id) => setView({ page: 'session', id })}
          />
        )}
        {view.page === 'session' && (
          <SessionDetail
            id={view.id}
            onBack={() => setView({ page: 'sessions' })}
            onSelectRefinement={(id) => setView({ page: 'refinement', id })}
          />
        )}
        {view.page === 'refinement' && (
          <RefinementDetail
            id={view.id}
            onBack={() => setView({ page: 'dashboard' })}
          />
        )}
        {view.page === 'stats' && <Stats />}
      </main>
    </div>
  );
}

export default App;
