import { useMemo } from 'react'
import { Alert } from '../components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownCard } from '../components/overview/token-breakdown-card'
import { SourceUsageTable } from '../components/overview/source-usage-table'
import { SourceTrendChart } from '../components/overview/source-trend-chart'
import { TopSignals } from '../components/overview/top-signals'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { ErrorState } from '../components/common/error-state'
import { KpiGrid } from '../components/common/kpi-grid'
import { TooltipProvider } from '../components/ui/tooltip'
import { PageHeader } from '../components/layout/page-header'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { useOverviewAll } from '../lib/use-overview-all'
import { usePeriodControls } from '../lib/use-period-controls'
import { getAvgTokenTotal, getTokenTotal } from '../lib/token-breakdown'
import { formatCompactInteger, formatInteger, formatTokenCount } from '../lib/format'
import type { SourceID } from '../types/api'

export function OverviewView() {
  const { requestRefresh } = useDashboardContext()
  const { cacheKey } = usePeriodControls()
  const { data, loading, error } = useOverviewAll(cacheKey)

  const labelFor = useMemo(() => {
    const map = new Map<string, string>()
    for (const src of data?.sources ?? []) {
      map.set(src.source_id, src.label ?? src.source_id)
    }
    return (id?: SourceID) => (id ? map.get(id) ?? id : 'unknown')
  }, [data?.sources])

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <PageHeader title="Overview" description="Combined analytics across all data sources" />
        <DataPageSkeleton sections={['kpi-grid', 'table', 'chart']} />
      </section>
    )
  }

  const activeSources = data ? data.sources.filter((s) => s.overview.sessions > 0 || s.overview.messages > 0).length : 0
  const totalTokens = data ? getTokenTotal(data.token_distribution) : 0

  return (
    <section className="space-y-6">
      <PageHeader title="Overview" description="Combined analytics across all data sources" />

      {error ? <ErrorState title="Failed to load overview" message={error} onRetry={requestRefresh} /> : null}

      {data ? (
        <>
          <TooltipProvider>
            <KpiGrid>
              <MetricCard
                label="Total sessions"
                value={formatInteger(data.total.sessions)}
                hint={`${data.sources.length} sources • ${data.total.days} active days`}
              />
              <MetricCard
                label="Total messages"
                value={formatInteger(data.total.messages)}
                hint={`${data.messages_per_session.toFixed(1)} per session`}
              />
              <MetricCard
                label="Total tokens"
                value={formatTokenCount(totalTokens)}
                tooltipValue={formatInteger(totalTokens)}
                hint={`${formatCompactInteger(getAvgTokenTotal(data.tokens_per_message))} per message`}
              />
              <MetricCard
                label="Sources active"
                value={`${activeSources} / ${data.sources.length}`}
                hint="sources with activity in range"
              />
            </KpiGrid>
          </TooltipProvider>

          {data.errors && data.errors.length > 0
            ? data.errors.map((e) => (
                <Alert key={e.source_id} tone="danger">
                  {labelFor(e.source_id)} could not be loaded: {e.message}
                </Alert>
              ))
            : null}

          {data.total.sessions === 0 ? (
            <Alert tone="info">
              No activity recorded across any source for this range. Adjust the time range or check that at least one
              source has data.
            </Alert>
          ) : null}

          <SourceUsageTable sources={data.sources} />

          <div className="grid gap-4 xl:grid-cols-[1.4fr_1fr]">
            <TokenBreakdownCard tokens={data.token_distribution} description="Combined token mix" />

            <Card>
              <CardHeader>
                <CardDescription>Efficiency</CardDescription>
                <CardTitle>Throughput ratios (all sources)</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="divide-y divide-border/15">
                  <Row label="Messages / session" value={data.messages_per_session.toFixed(1)} />
                  <Row label="Tokens / message" value={formatCompactInteger(getAvgTokenTotal(data.tokens_per_message))} />
                  <Row label="Input / message" value={formatCompactInteger(Math.round(data.tokens_per_message.input))} />
                  <Row label="Output / message" value={formatCompactInteger(Math.round(data.tokens_per_message.output))} />
                  <Row label="Reasoning / message" value={formatCompactInteger(Math.round(data.tokens_per_message.reasoning))} />
                  <Row label="Total tokens" value={formatTokenCount(totalTokens)} />
                </div>
              </CardContent>
            </Card>
          </div>

          <SourceTrendChart sources={data.sources} />

          <TopSignals models={data.top_models} projects={data.top_projects} tools={data.top_tools} labelFor={labelFor} />
        </>
      ) : null}
    </section>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between py-2.5 first:pt-0 last:pb-0">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="font-mono text-sm text-foreground">{value}</span>
    </div>
  )
}
