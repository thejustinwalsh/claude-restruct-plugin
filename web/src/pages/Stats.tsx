import { useApi } from '@/hooks/useApi';
import { api } from '@/api/client';
import type {
  StatsData,
  RefinementStat,
  PipelineBreakdown,
  DailyCount,
  SessionStat,
} from '@/api/client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart';
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Scatter,
  ScatterChart,
  XAxis,
  YAxis,
} from 'recharts';

// -- Chart 1: Latency over time --

const latencyConfig: ChartConfig = {
  latency: { label: 'Latency (s)', color: 'var(--chart-1)' },
};

function LatencyChart({ data }: { data: RefinementStat[] }) {
  const chartData = [...data]
    .filter((d) => !d.passthrough && !d.cache_hit)
    .reverse()
    .map((d) => ({
      time: new Date(d.created_at).toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      }),
      latency: +(d.latency_ms / 1000).toFixed(1),
    }));

  if (chartData.length === 0) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Latency Over Time</CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer config={latencyConfig} className="h-[250px] w-full">
          <AreaChart data={chartData}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis
              dataKey="time"
              tick={{ fontSize: 11 }}
              interval="preserveStartEnd"
            />
            <YAxis tick={{ fontSize: 11 }} unit="s" />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Area
              dataKey="latency"
              type="monotone"
              fill="var(--color-latency)"
              fillOpacity={0.3}
              stroke="var(--color-latency)"
              strokeWidth={2}
            />
          </AreaChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}

// -- Chart 2: Cache hit rate over time --

const cacheConfig: ChartConfig = {
  hit: { label: 'Cache Hit', color: 'var(--chart-2)' },
  miss: { label: 'LLM Call', color: 'var(--chart-1)' },
  passthrough: { label: 'Passthrough', color: 'var(--chart-4)' },
};

function CacheChart({ data }: { data: RefinementStat[] }) {
  // Group into buckets of 10 refinements for a rolling view
  const reversed = [...data].reverse();
  const buckets: {
    label: string;
    hit: number;
    miss: number;
    passthrough: number;
  }[] = [];
  const size = Math.max(5, Math.floor(reversed.length / 15));

  for (let i = 0; i < reversed.length; i += size) {
    const chunk = reversed.slice(i, i + size);
    const hit = chunk.filter((r) => r.cache_hit).length;
    const pass = chunk.filter((r) => r.passthrough).length;
    const miss = chunk.length - hit - pass;
    const first = new Date(chunk[0].created_at);
    buckets.push({
      label: first.toLocaleDateString(undefined, {
        month: 'short',
        day: 'numeric',
      }),
      hit,
      miss,
      passthrough: pass,
    });
  }

  if (buckets.length === 0) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Cache Hit Rate</CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer config={cacheConfig} className="h-[250px] w-full">
          <BarChart data={buckets}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis dataKey="label" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar
              dataKey="hit"
              stackId="a"
              fill="var(--color-hit)"
              radius={[0, 0, 0, 0]}
            />
            <Bar
              dataKey="miss"
              stackId="a"
              fill="var(--color-miss)"
              radius={[0, 0, 0, 0]}
            />
            <Bar
              dataKey="passthrough"
              stackId="a"
              fill="var(--color-passthrough)"
              radius={[4, 4, 0, 0]}
            />
          </BarChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}

// -- Chart 3: Pipeline stage breakdown --

const stageColors: Record<string, string> = {
  rules_load: 'var(--chart-1)',
  cache_check: 'var(--chart-2)',
  git_context: 'var(--chart-3)',
  prompt_build: 'var(--chart-4)',
  ollama_check: 'var(--chart-5)',
  model_ensure: 'var(--chart-1)',
  ollama_inference: 'var(--chart-3)',
  validation: 'var(--chart-2)',
  cache_store: 'var(--chart-4)',
};

function PipelineChart({ data }: { data: PipelineBreakdown[] }) {
  // Group by refinement, pivot stages to columns
  const byRef = new Map<
    number,
    { created_at: string; stages: Record<string, number> }
  >();
  const allStages = new Set<string>();

  for (const d of data) {
    if (!byRef.has(d.refinement_id)) {
      byRef.set(d.refinement_id, { created_at: d.created_at, stages: {} });
    }
    const row = byRef.get(d.refinement_id)!;
    row.stages[d.stage] = +(d.duration_ms / 1000).toFixed(2);
    allStages.add(d.stage);
  }

  const stages = [...allStages];
  const chartData = [...byRef.values()]
    .sort((a, b) => a.created_at.localeCompare(b.created_at))
    .map((row) => ({
      ...row.stages,
      time: new Date(row.created_at).toLocaleTimeString(undefined, {
        hour: '2-digit',
        minute: '2-digit',
      }),
    }));

  const config: ChartConfig = {};
  for (const s of stages) {
    config[s] = {
      label: s.replace(/_/g, ' '),
      color: stageColors[s] || 'var(--chart-5)',
    };
  }

  if (chartData.length === 0) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Pipeline Stage Breakdown</CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer config={config} className="h-[250px] w-full">
          <BarChart data={chartData.slice(-30)}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis dataKey="time" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} unit="s" />
            <ChartTooltip content={<ChartTooltipContent />} />
            {stages.map((stage) => (
              <Bar
                key={stage}
                dataKey={stage}
                stackId="a"
                fill={`var(--color-${stage})`}
              />
            ))}
          </BarChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}

// -- Chart 4: Refinements per day --

const dailyConfig: ChartConfig = {
  count: { label: 'Refinements', color: 'var(--chart-1)' },
};

function DailyChart({ data }: { data: DailyCount[] }) {
  const chartData = [...data].reverse().map((d) => ({
    date: new Date(d.date).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
    }),
    count: d.count,
  }));

  if (chartData.length === 0) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Refinements Per Day</CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer config={dailyConfig} className="h-[250px] w-full">
          <BarChart data={chartData}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar
              dataKey="count"
              fill="var(--color-count)"
              radius={[4, 4, 0, 0]}
            />
          </BarChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}

// -- Chart 5: Session duration vs refinements --

const scatterConfig: ChartConfig = {
  session: { label: 'Session', color: 'var(--chart-3)' },
};

function SessionScatterChart({ data }: { data: SessionStat[] }) {
  const chartData = data
    .filter((d) => d.duration_minutes > 0 && d.refinements > 0)
    .map((d) => ({
      duration: +d.duration_minutes.toFixed(1),
      refinements: d.refinements,
    }));

  if (chartData.length === 0) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">
          Session Duration vs Refinements
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer config={scatterConfig} className="h-[250px] w-full">
          <ScatterChart>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis
              dataKey="duration"
              name="Duration"
              unit="min"
              tick={{ fontSize: 11 }}
            />
            <YAxis
              dataKey="refinements"
              name="Refinements"
              tick={{ fontSize: 11 }}
            />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Scatter
              data={chartData}
              fill="var(--color-session)"
              shape="circle"
            />
          </ScatterChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}

// -- Stats page --

export function Stats() {
  const { data, loading } = useApi<StatsData>(() => api.stats(), []);

  if (loading || !data) {
    return (
      <div className="flex items-center justify-center py-20">
        <p className="text-muted-foreground text-sm">Loading stats...</p>
      </div>
    );
  }

  const noData =
    (!data.refinements || data.refinements.length === 0) &&
    (!data.daily || data.daily.length === 0);

  if (noData) {
    return (
      <div className="flex items-center justify-center py-20">
        <p className="text-muted-foreground text-sm">
          No refinement data yet. Use restruct to generate stats.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Stats</h1>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <LatencyChart data={data.refinements ?? []} />
        <CacheChart data={data.refinements ?? []} />
        <PipelineChart data={data.pipeline ?? []} />
        <DailyChart data={data.daily ?? []} />
      </div>

      <SessionScatterChart data={data.sessions ?? []} />
    </div>
  );
}
