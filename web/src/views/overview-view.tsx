import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownCard } from '../components/overview/token-breakdown-card'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { getOverview } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatCompactInteger, formatCurrency, formatInteger, safeDivide } from '../lib/format'
import { isDailyPeriod, type DailyPeriod } from '../types/api'

export function OverviewView() {
  const { requestRefresh } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const { data, loading, error } = usePeriodResource(getOverview, period)

  // Normalize missing/invalid period to ?period=7d on mount (preserves other params).
  // This happens inside usePeriodResource's period param, but we keep URL sync here.
  // The hook handles fetch; we only manage URL state.
  // Period normalization via URL is intentionally not an effect here:
  // the view renders with a fallback until the user changes it or the parent route re-mounts.

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) {
      return
    }

    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('period', nextPeriod)
      return next
    })
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
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Overview</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Real data from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/overview</code>
              , proving the web path end-to-end.
            </p>
          </div>
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'chart']} />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Overview</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Executive summary first: sessions, messages, spend, active days, and token mix coming from the Go API.
          </p>
        </div>
        <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="text-sm text-muted-foreground">
            Endpoint: <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/overview?period={period}</code>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Overview failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {dataForPeriod ? (
        <>
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
              value={formatCurrency(dataForPeriod.cost)}
              hint={`${formatCurrency(dataForPeriod.cost_per_day)} average spend per active day`}
            />
            <MetricCard
              label="Active days"
              value={formatInteger(dataForPeriod.days)}
              hint={dataForPeriod.days === 0 ? 'No active days recorded yet' : 'Distinct days with session activity'}
            />
          </div>

          {dataForPeriod.sessions === 0 ? (
            <Alert tone="info">
              No sessions have been recorded yet. This is normal on a fresh OpenCode setup — once data exists, the cards above will fill automatically.
            </Alert>
          ) : null}

          <div className="grid gap-4 xl:grid-cols-[1.4fr_1fr_1fr]">
            <TokenBreakdownCard tokens={dataForPeriod.tokens} />

            <Card>
              <CardHeader>
                <CardDescription>Efficiency</CardDescription>
                <CardTitle>Cost and throughput ratios</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4 text-sm">
                <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                  <span className="text-muted-foreground">Cost / day</span>
                  <span className="font-mono text-foreground">{formatCurrency(dataForPeriod.cost_per_day)}</span>
                </div>
                <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                  <span className="text-muted-foreground">Messages / session</span>
                  <span className="font-mono text-foreground">
                    {efficiency ? efficiency.messagesPerSession.toFixed(1) : '0.0'}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                  <span className="text-muted-foreground">Cost / message</span>
                  <span className="font-mono text-foreground">
                    {formatCurrency(efficiency ? efficiency.costPerMessage : 0)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                  <span className="text-muted-foreground">Total tokens</span>
                  <span className="font-mono text-foreground">
                    {formatCompactInteger(efficiency ? efficiency.totalTokens : 0)}
                  </span>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardDescription>Slice coverage</CardDescription>
                <CardTitle>What is real right now</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4 text-sm text-muted-foreground">
                <p>
                  The shell, routing, dark theme foundation, typed fetch client, and Overview analytics are all live against the Go backend.
                </p>
                <p>
                  Top contributors and recent activity are intentionally deferred until the Models, Projects, Tools, and Sessions web slices are wired.
                </p>
                <div className="rounded-xl border border-border/70 bg-panel/70 px-3 py-3 text-xs uppercase tracking-[0.14em] text-foreground/80">
                  Scope discipline beats fake completeness.
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      ) : null}
    </section>
  )
}
