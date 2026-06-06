import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import type { PeriodMode } from '../components/daily/period-toggle'
import { Alert } from '../components/ui/alert'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownCard } from '../components/overview/token-breakdown-card'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { getOverview } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatCompactInteger, formatCurrencyWithProvenance, formatInteger, safeDivide } from '../lib/format'
import { usePeriodState, serializeCustomPeriod, applyPeriodToUrl } from '../lib/use-period-state'
import type { CustomPeriod, DailyPeriod } from '../types/api'

export function OverviewView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [, setSearchParams] = useSearchParams()

  const periodState = usePeriodState()
  const cacheKey = periodState.mode === 'custom' && periodState.customRange
    ? serializeCustomPeriod(periodState.customRange.from, periodState.customRange.to)
    : periodState.preset
  const { data, loading, error } = usePeriodResource(getOverview, cacheKey)
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  const handlePresetChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === periodState.preset && periodState.mode === 'preset') return
    setSearchParams((previous) => {
      return applyPeriodToUrl(previous, { mode: 'preset', preset: nextPeriod })
    })
  }

  const handleCustomRangeChange = (range: CustomPeriod) => {
    setSearchParams((previous) => {
      return applyPeriodToUrl(previous, {
        mode: 'custom',
        customRange: range,
      })
    })
  }

  const handleModeChange = (mode: PeriodMode) => {
    if (mode === 'preset') {
      setSearchParams((previous) => {
        return applyPeriodToUrl(previous, { mode: 'preset', preset: periodState.preset })
      })
    } else {
      setSearchParams((previous) => {
        return applyPeriodToUrl(previous, {
          mode: 'custom',
          customRange: periodState.customRange ?? { from: '' },
        })
      })
    }
  }

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
        <div className="flex items-center justify-between">
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Overview</h2>
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'chart']} />
      </section>
    )
  }

  return (
    <section className="space-y-8">
      {/* Page header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Overview</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Executive summary of sessions, messages, spend, and token mix
          </p>
        </div>
        <PeriodToggle
          mode={periodState.mode}
          preset={periodState.preset}
          customRange={periodState.customRange}
          onPresetChange={handlePresetChange}
          onCustomRangeChange={handleCustomRangeChange}
          onModeChange={handleModeChange}
          disabled={loading}
        />
      </div>

      {/* Error state */}
      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Failed to load overview</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {dataForPeriod ? (
        <>
          {/* KPI grid */}
          <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-4">
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
          </div>

          {/* Empty state */}
          {dataForPeriod.sessions === 0 ? (
            <Alert tone="info">
              {selectedSourceId === 'claude_code'
                ? 'No persisted Claude Code transcripts were found for this view. OpenCode data is not being mixed into the selected Claude source.'
                : `No sessions have been recorded yet. This is normal on a fresh ${sourceLabel} setup — once data exists, the cards above will fill automatically.`}
            </Alert>
          ) : null}

          {/* Bottom analytics grid */}
          <div className="grid gap-4 xl:grid-cols-[1.4fr_1fr_1fr]">
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

            <Card>
              <CardHeader>
                <CardDescription>Project status</CardDescription>
                <CardTitle>Live features</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>
                  Dashboard shell, routing, theming, and Overview analytics are live against the Go backend.
                </p>
                <p>
                  Remaining views (Daily, Models, Tools, Projects, Sessions, Config) will be wired incrementally as their respective API slices are completed.
                </p>
              </CardContent>
            </Card>
          </div>
        </>
      ) : null}
    </section>
  )
}
