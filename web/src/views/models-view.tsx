import { useEffect, useMemo, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { SortButton } from '../components/ui/sort-button'
import { Progress } from '../components/ui/progress'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'
import { getModels } from '../lib/api'
import {
  formatCompactCurrency,
  formatCompactInteger,
  formatCurrency,
  formatInteger,
  formatPercentage,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import type { ModelEntry, ModelStats } from '../types/api'
import { ModelsSkeleton } from '../components/models/models-skeleton'

type SortKey = 'cost' | 'messages' | 'sessions' | 'model' | 'provider' | 'avgCostPerMessage'

interface EnrichedModelRow extends ModelEntry {
  totalTokens: number
  avgCostPerMessage: number
  costShare: number
}

function getModelLabel(model: ModelEntry) {
  return model.model_id || 'Unknown model'
}

function getProviderLabel(model: ModelEntry) {
  return model.provider_id || 'Unknown provider'
}

function getTotalTokens(model: ModelEntry) {
  return model.tokens.input + model.tokens.output + model.tokens.reasoning + model.tokens.cache.read + model.tokens.cache.write
}

function compareRows(sortKey: SortKey, left: EnrichedModelRow, right: EnrichedModelRow) {
  switch (sortKey) {
    case 'model':
      return getModelLabel(left).localeCompare(getModelLabel(right))
    case 'provider':
      return getProviderLabel(left).localeCompare(getProviderLabel(right))
    case 'sessions':
      return right.sessions - left.sessions
    case 'messages':
      return right.messages - left.messages
    case 'avgCostPerMessage':
      return right.avgCostPerMessage - left.avgCostPerMessage
    case 'cost':
    default:
      return right.cost - left.cost
  }
}

export function ModelsView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [data, setData] = useState<ModelStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sortKey, setSortKey] = useState<SortKey>('cost')
  const hasLoadedOnceRef = useRef(false)

  useEffect(() => {
    const controller = new AbortController()

    async function loadModels() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      }

      try {
        const next = await getModels(controller.signal)
        setData(next)
        hasLoadedOnceRef.current = true
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load model stats')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadModels()

    return () => controller.abort()
  }, [refreshNonce, setLastUpdatedAt, setRefreshing])

  const summary = useMemo(() => {
    if (!data) {
      return null
    }

    const totalCost = data.models.reduce((accumulator, model) => accumulator + model.cost, 0)
    const totalMessages = data.models.reduce((accumulator, model) => accumulator + model.messages, 0)
    const totalSessions = data.models.reduce((accumulator, model) => accumulator + model.sessions, 0)

    const rows = data.models.map<EnrichedModelRow>((model) => {
      const totalTokens = getTotalTokens(model)

      return {
        ...model,
        totalTokens,
        avgCostPerMessage: safeDivide(model.cost, model.messages),
        costShare: safeDivide(model.cost, totalCost) * 100,
      }
    })

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(sortKey, left, right)
      if (primary !== 0) {
        return primary
      }

      if (right.cost !== left.cost) {
        return right.cost - left.cost
      }

      return getModelLabel(left).localeCompare(getModelLabel(right))
    })

    const costLeader = [...rows].sort((left, right) => right.cost - left.cost)[0] ?? null
    const usageLeader = [...rows].sort((left, right) => right.messages - left.messages)[0] ?? null
    const efficiencyLeader = [...rows]
      .filter((row) => row.messages > 0)
      .sort((left, right) => left.avgCostPerMessage - right.avgCostPerMessage)[0] ?? null

    return {
      rows: sortedRows,
      totalCost,
      totalMessages,
      totalSessions,
      empty: rows.length === 0,
      costLeader,
      usageLeader,
      efficiencyLeader,
    }
  }, [data, sortKey])

  const handleRetry = () => {
    requestRefresh()
  }

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Models</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Ranked model usage from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/models</code>, designed for dense comparison instead of fluffy cards.
            </p>
          </div>
        </div>
        <ModelsSkeleton />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Models</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Dense model comparison for spend, usage, and token posture. Summary first, table second — exactly how this route should behave.
          </p>
        </div>

        <div className="text-sm text-muted-foreground">
          Endpoint: <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/models</code>
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Models failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard
              label="Tracked models"
              value={formatInteger(summary.rows.length)}
              hint={summary.rows.length === 1 ? 'One assistant model detected' : 'Distinct assistant model/provider combinations'}
            />
            <MetricCard
              label="Total cost"
              value={formatCurrency(summary.totalCost)}
              hint={`${formatCompactInteger(summary.totalMessages)} assistant messages attributed`}
            />
            <MetricCard
              label="Sessions touched"
              value={formatInteger(summary.totalSessions)}
              hint={summary.usageLeader ? `${getModelLabel(summary.usageLeader)} leads message volume` : 'Awaiting activity'}
            />
            <MetricCard
              label="Spend / message"
              value={formatCurrency(safeDivide(summary.totalCost, summary.totalMessages))}
              hint={summary.efficiencyLeader ? `${getModelLabel(summary.efficiencyLeader)} is the cheapest active option` : 'Not enough data yet'}
            />
          </div>

          {summary.empty ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>No model usage recorded yet</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>
                  This endpoint stays empty until assistant messages with <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">modelID</code> metadata exist in the database.
                </p>
                <p>
                  Once data lands, this route will rank models by cost out of the box and expose cost-share, message volume, token load, and per-message spend.
                </p>
              </CardContent>
            </Card>
          ) : (
            <>
              <div className="grid gap-4 xl:grid-cols-[1fr_1fr_1fr]">
                <Card>
                  <CardHeader>
                    <CardDescription>Highest cost model</CardDescription>
                    <CardTitle>{summary.costLeader ? getModelLabel(summary.costLeader) : 'No data'}</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-muted-foreground">
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Provider</span>
                      <span className="font-mono text-foreground">{summary.costLeader ? getProviderLabel(summary.costLeader) : '—'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Cost share</span>
                      <span className="font-mono text-foreground">{summary.costLeader ? formatPercentage(summary.costLeader.costShare) : '0%'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Total cost</span>
                      <span className="font-mono text-foreground">{summary.costLeader ? formatCurrency(summary.costLeader.cost) : '$0.00'}</span>
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardDescription>Most used model</CardDescription>
                    <CardTitle>{summary.usageLeader ? getModelLabel(summary.usageLeader) : 'No data'}</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-muted-foreground">
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Messages</span>
                      <span className="font-mono text-foreground">{summary.usageLeader ? formatInteger(summary.usageLeader.messages) : '0'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Sessions</span>
                      <span className="font-mono text-foreground">{summary.usageLeader ? formatInteger(summary.usageLeader.sessions) : '0'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Total tokens</span>
                      <span className="font-mono text-foreground">
                        {summary.usageLeader ? formatTokenCount(summary.usageLeader.totalTokens) : '0'}
                      </span>
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardDescription>Best cost / message</CardDescription>
                    <CardTitle>{summary.efficiencyLeader ? getModelLabel(summary.efficiencyLeader) : 'Insufficient data'}</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-muted-foreground">
                    <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
                      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Cost per message</div>
                      <div className="mt-2 font-mono text-lg text-foreground">
                        {summary.efficiencyLeader ? formatCurrency(summary.efficiencyLeader.avgCostPerMessage) : '$0.00'}
                      </div>
                      <div className="text-sm text-muted-foreground">
                        {summary.efficiencyLeader
                          ? `${formatInteger(summary.efficiencyLeader.messages)} assistant messages sampled`
                          : 'Need at least one active model to compute this'}
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </div>

              <Card>
                <CardHeader className="gap-3 lg:flex-row lg:items-end lg:justify-between">
                  <div className="space-y-1.5">
                    <CardDescription>Primary artifact</CardDescription>
                    <CardTitle>Model usage ranking</CardTitle>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Badge tone="success">Dense table</Badge>
                    <Badge>Sticky summary cues</Badge>
                  </div>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="hidden lg:block">
                    <Table className="overflow-hidden rounded-2xl border border-border/70">
                      <TableHeader className="bg-panel/75">
                        <TableRow className="border-b border-border/70 hover:bg-transparent">
                          <TableHead className="min-w-[14rem]" aria-sort={sortKey === 'model' ? 'descending' : 'none'}>
                            <SortButton active={sortKey === 'model'} label="Model" onClick={() => setSortKey('model')} />
                          </TableHead>
                          <TableHead className="w-[9rem]" aria-sort={sortKey === 'provider' ? 'descending' : 'none'}>
                            <SortButton active={sortKey === 'provider'} label="Provider" onClick={() => setSortKey('provider')} />
                          </TableHead>
                          <TableHead className="w-[5.5rem]" aria-sort={sortKey === 'sessions' ? 'descending' : 'none'}>
                            <SortButton active={sortKey === 'sessions'} label="Sessions" onClick={() => setSortKey('sessions')} />
                          </TableHead>
                          <TableHead className="w-[6rem]" aria-sort={sortKey === 'messages' ? 'descending' : 'none'}>
                            <SortButton active={sortKey === 'messages'} label="Messages" onClick={() => setSortKey('messages')} />
                          </TableHead>
                          <TableHead className="w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Input</TableHead>
                          <TableHead className="w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Output</TableHead>
                          <TableHead className="w-[7rem]" aria-sort={sortKey === 'cost' ? 'descending' : 'none'}>
                            <SortButton active={sortKey === 'cost'} label="Cost" onClick={() => setSortKey('cost')} />
                          </TableHead>
                          <TableHead className="w-[8rem]" aria-sort={sortKey === 'avgCostPerMessage' ? 'descending' : 'none'}>
                            <SortButton active={sortKey === 'avgCostPerMessage'} label="Avg / msg" onClick={() => setSortKey('avgCostPerMessage')} />
                          </TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {summary.rows.map((row) => (
                          <TableRow key={`${row.provider_id}:${row.model_id}`} className="bg-card/40 hover:bg-white/4">
                            <TableCell className="min-w-[14rem]">
                              <div className="space-y-2">
                                <div className="truncate font-medium text-foreground">{getModelLabel(row)}</div>
                                <div className="flex items-center gap-3">
                                  <Progress value={Math.max(row.costShare, row.cost > 0 ? 4 : 0)} className="flex-1" />
                                  <span className="font-mono text-xs text-muted-foreground">{formatPercentage(row.costShare)}</span>
                                </div>
                              </div>
                            </TableCell>
                            <TableCell className="truncate font-mono text-sm text-muted-foreground">{getProviderLabel(row)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.messages)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatTokenCount(row.tokens.input)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatTokenCount(row.tokens.output)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerMessage)}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>

                  <div className="space-y-3 lg:hidden">
                    {summary.rows.map((row) => (
                      <div
                        key={`${row.provider_id}:${row.model_id}`}
                        className="rounded-2xl border border-border/70 bg-panel/65 p-4"
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0">
                            <div className="truncate font-medium text-foreground">{getModelLabel(row)}</div>
                            <div className="mt-1 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                              {getProviderLabel(row)}
                            </div>
                          </div>
                          <div className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</div>
                        </div>

<Progress
                           className="mt-3"
                           value={Math.max(row.costShare, row.cost > 0 ? 4 : 0)}
                         />

                        <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                          <div className="rounded-lg bg-background/40 px-2.5 py-2">
                            <div className="uppercase tracking-[0.14em]">Sessions</div>
                            <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</div>
                          </div>
                          <div className="rounded-lg bg-background/40 px-2.5 py-2">
                            <div className="uppercase tracking-[0.14em]">Messages</div>
                            <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.messages)}</div>
                          </div>
                          <div className="rounded-lg bg-background/40 px-2.5 py-2">
                            <div className="uppercase tracking-[0.14em]">Input</div>
                            <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(row.tokens.input)}</div>
                          </div>
                          <div className="rounded-lg bg-background/40 px-2.5 py-2">
                            <div className="uppercase tracking-[0.14em]">Output</div>
                            <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(row.tokens.output)}</div>
                          </div>
                          <div className="rounded-lg bg-background/40 px-2.5 py-2">
                            <div className="uppercase tracking-[0.14em]">Avg / msg</div>
                            <div className="mt-1 font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerMessage)}</div>
                          </div>
                          <div className="rounded-lg bg-background/40 px-2.5 py-2">
                            <div className="uppercase tracking-[0.14em]">Cost share</div>
                            <div className="mt-1 font-mono text-sm text-foreground">{formatPercentage(row.costShare)}</div>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            </>
          )}
        </>
      ) : null}
    </section>
  )
}
