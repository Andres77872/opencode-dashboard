import { useMemo } from 'react'
import { Alert } from '../components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownCard } from '../components/overview/token-breakdown-card'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { ErrorState } from '../components/common/error-state'
import { KpiGrid } from '../components/common/kpi-grid'
import { PageHeader } from '../components/layout/page-header'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { getOverview } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatCompactInteger, formatCurrencyWithProvenance, formatInteger, safeDivide } from '../lib/format'
import { usePeriodControls } from '../lib/use-period-controls'

export function OverviewView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()

  const { cacheKey } = usePeriodControls()
  const { data, loading, error } = usePeriodResource(getOverview, cacheKey)
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  const dataForPeriod = data
  const efficiency = useMemo(() => {
    if (!dataForPeriod) {
      return null
    }

    return {
      messagesPerSession: safeDivide(dataForPeriod.messages, dataForPeriod.sessions),
      costPerMessage: safeDivide(dataForPeriod.cost, dataForPeriod.messages),
      totalTokens:
        dataForPeriod.tokens.input +
        dataForPeriod.tokens.output +
        dataForPeriod.tokens.reasoning +
        dataForPeriod.tokens.cache.read +
        dataForPeriod.tokens.cache.write,
    }
  }, [dataForPeriod])

  const handleRetry = () => {
    requestRefresh()
  }

  if (loading && !dataForPeriod) {
    return (
      <section className="space-y-6">
        <PageHeader title="Overview" description="Executive summary of sessions, messages, spend, and token mix" />
        <DataPageSkeleton sections={['kpi-grid', 'chart']} />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <PageHeader title="Overview" description="Executive summary of sessions, messages, spend, and token mix" />

      {error ? <ErrorState title="Failed to load overview" message={error} onRetry={handleRetry} /> : null}

      {dataForPeriod ? (
        <>
          <KpiGrid>
            <MetricCard
              label="Total sessions"
              value={formatInteger(dataForPeriod.sessions)}
              hint={`${formatCompactInteger(dataForPeriod.sessions)} recorded sessions`}
            />
            <MetricCard
              label="Total messages"
              value={formatInteger(dataForPeriod.messages)}
              hint={`${formatCompactInteger(dataForPeriod.messages)} messages captured`}
            />
            <MetricCard
              label="Total cost"
              value={formatCurrencyWithProvenance(dataForPeriod.cost, dataForPeriod.cost_status, dataForPeriod.cost_provenance)}
              hint={`${formatCurrencyWithProvenance(dataForPeriod.cost_per_day, dataForPeriod.cost_status, dataForPeriod.cost_provenance)} avg per active day`}
            />
            <MetricCard
              label="Active days"
              value={formatInteger(dataForPeriod.days)}
              hint={dataForPeriod.days === 0 ? 'No active days recorded yet' : 'Distinct days with session activity'}
            />
          </KpiGrid>

          {dataForPeriod.sessions === 0 ? (
            <Alert tone="info">
              {selectedSourceId === 'claude_code'
                ? 'No persisted Claude Code transcripts were found for this view. OpenCode data is not being mixed into the selected Claude source.'
                : `No sessions have been recorded yet. This is normal on a fresh ${sourceLabel} setup — once data exists, the cards above will fill automatically.`}
            </Alert>
          ) : null}

          <div className="grid gap-4 xl:grid-cols-[1.4fr_1fr]">
            <TokenBreakdownCard tokens={dataForPeriod.tokens} />

            <Card>
              <CardHeader>
                <CardDescription>Efficiency</CardDescription>
                <CardTitle>Cost and throughput ratios</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="divide-y divide-border/15">
                  <div className="flex items-center justify-between py-2.5 first:pt-0 last:pb-0">
                    <span className="text-sm text-muted-foreground">Cost / day</span>
                    <span className="font-mono text-sm text-foreground">{formatCurrencyWithProvenance(dataForPeriod.cost_per_day, dataForPeriod.cost_status, dataForPeriod.cost_provenance)}</span>
                  </div>
                  <div className="flex items-center justify-between py-2.5 first:pt-0 last:pb-0">
                    <span className="text-sm text-muted-foreground">Messages / session</span>
                    <span className="font-mono text-sm text-foreground">
                      {efficiency ? efficiency.messagesPerSession.toFixed(1) : '0.0'}
                    </span>
                  </div>
                  <div className="flex items-center justify-between py-2.5 first:pt-0 last:pb-0">
                    <span className="text-sm text-muted-foreground">Cost / message</span>
                    <span className="font-mono text-sm text-foreground">
                      {formatCurrencyWithProvenance(efficiency ? efficiency.costPerMessage : 0, dataForPeriod.cost_status, dataForPeriod.cost_provenance)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between py-2.5 first:pt-0 last:pb-0">
                    <span className="text-sm text-muted-foreground">Total tokens</span>
                    <span className="font-mono text-sm text-foreground">
                      {formatCompactInteger(efficiency ? efficiency.totalTokens : 0)}
                    </span>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      ) : null}
    </section>
  )
}
