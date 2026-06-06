import { useMemo } from 'react'
import { Alert } from '../ui/alert'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { SessionMessageRow } from './session-message-row'
import { TokenBreakdownList } from '../overview/token-breakdown-card'
import { useDashboardContext } from '../layout/dashboard-context'
import { getTokenTotal } from '../../lib/token-breakdown'
import {
  formatCurrencyWithProvenance,
  formatDateTime,
  formatInteger,
  formatTokenCount,
  safeDivide,
} from '../../lib/format'
import type { SessionDetail, SessionMessage } from '../../types/api'

// ── Props ──────────────────────────────────────────────────────

interface SessionDetailCardProps {
  detail: SessionDetail | null
  loading: boolean
  error: string | null
  onRetry: () => void
}

// ── Helper components (extracted from sessions-view) ───────────

function DetailMetric({
  label,
  value,
  hint,
  tooltipValue,
}: {
  label: string
  value: string
  hint: string
  tooltipValue?: string
}) {
  const valueElement = tooltipValue ? (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="mt-2 cursor-default font-mono text-lg text-foreground transition-opacity hover:opacity-80 sm:text-xl">
            {value}
          </div>
        </TooltipTrigger>
        <TooltipContent side="top" className="font-mono">
          <p>{tooltipValue}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    <div className="mt-2 font-mono text-lg text-foreground sm:text-xl">{value}</div>
  )

  return (
    <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      {valueElement}
      <div className="mt-2 text-sm leading-6 text-muted-foreground">{hint}</div>
    </div>
  )
}

function DetailFact({
  label,
  value,
  subtle = false,
}: {
  label: string
  value: string
  subtle?: boolean
}) {
  return (
    <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      <div className={`mt-2 break-all font-mono text-sm ${subtle ? 'text-muted-foreground' : 'text-foreground'}`}>{value}</div>
    </div>
  )
}

// ── Helpers ────────────────────────────────────────────────────

function getSessionProjectLabel(
  session: Pick<SessionDetail, 'project_name' | 'project_id'>,
) {
  return session.project_name || session.project_id || 'No linked project'
}

function formatSessionWindow(createdAt: string, updatedAt: string) {
  const created = new Date(createdAt)
  const updated = new Date(updatedAt)
  const deltaMinutes = Math.max(0, Math.round((updated.getTime() - created.getTime()) / 60000))

  if (deltaMinutes < 1) {
    return 'Under 1 minute'
  }

  if (deltaMinutes < 60) {
    return `${deltaMinutes}m span`
  }

  const deltaHours = safeDivide(deltaMinutes, 60)
  if (deltaHours < 24) {
    return `${deltaHours.toFixed(deltaHours >= 10 ? 0 : 1)}h span`
  }

  return `${safeDivide(deltaHours, 24).toFixed(1)}d span`
}

// ── Component ──────────────────────────────────────────────────

export function SessionDetailCard({ detail, loading, error, onRetry }: SessionDetailCardProps) {
  const { selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  // Memoized computations extracted from sessions-view
  const detailMessageMix = useMemo(() => {
    if (!detail) {
      return { assistant: 0, user: 0, other: 0 }
    }

    return detail.messages.reduce(
      (accumulator, message) => {
        if (message.role === 'assistant') {
          accumulator.assistant += 1
        } else if (message.role === 'user') {
          accumulator.user += 1
        } else {
          accumulator.other += 1
        }

        return accumulator
      },
      { assistant: 0, user: 0, other: 0 },
    )
  }, [detail])

  const detailMessageStats = useMemo(() => {
    if (!detail) {
      return {
        hottestMessageId: null as string | null,
        hottestMessageCost: 0,
        heaviestTokenMessageId: null as string | null,
        heaviestTokenTotal: 0,
      }
    }

    let hottestMessage: SessionMessage | null = null
    let heaviestTokenMessage: SessionMessage | null = null

    for (const message of detail.messages) {
      if (!hottestMessage || (message.cost ?? 0) > (hottestMessage.cost ?? 0)) {
        hottestMessage = message
      }

      const msgTokens = message.tokens ? getTokenTotal(message.tokens) : 0
      const heaviestTokens = heaviestTokenMessage?.tokens ? getTokenTotal(heaviestTokenMessage.tokens) : 0
      if (!heaviestTokenMessage || msgTokens > heaviestTokens) {
        heaviestTokenMessage = message
      }
    }

    return {
      hottestMessageId: hottestMessage?.id ?? null,
      hottestMessageCost: hottestMessage?.cost ?? 0,
      heaviestTokenMessageId: heaviestTokenMessage?.id ?? null,
      heaviestTokenTotal: heaviestTokenMessage?.tokens ? getTokenTotal(heaviestTokenMessage.tokens) : 0,
    }
  }, [detail])

  // ── Loading state ──
  if (loading) {
    return (
      <div className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, index) => (
            <div key={index} className="rounded-2xl border border-border/70 bg-panel/45 px-4 py-4">
              <div className="h-3 w-24 rounded bg-white/8" />
              <div className="mt-3 h-8 w-28 rounded bg-white/8" />
              <div className="mt-3 h-4 w-40 rounded bg-white/8" />
            </div>
          ))}
        </div>
        {Array.from({ length: 5 }).map((_, index) => (
          <div key={index} className="h-28 rounded-2xl border border-border/70 bg-panel/45" />
        ))}
      </div>
    )
  }

  // ── Error state ──
  if (error) {
    return (
      <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="font-medium text-foreground">Session detail failed to load</div>
          <div className="text-sm opacity-90">{error}</div>
        </div>
        <Button variant="ghost" onClick={onRetry}>
          Retry detail
        </Button>
      </Alert>
    )
  }

  // ── Null state ──
  if (!detail) {
    return null
  }

  // ── Detail content ──
  return (
    <div className="space-y-6">
      <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-4">
        <DetailMetric
          label="Recorded rows"
          value={formatInteger(detail.messages.length)}
          hint={`${formatInteger(detail.message_count)} total messages reported · ${formatInteger(detailMessageMix.user)} user · ${formatInteger(detailMessageMix.assistant)} assistant`}
        />
        <DetailMetric
          label="Session spend"
          value={formatCurrencyWithProvenance(detail.total_cost, detail.cost_status, detail.cost_provenance)}
          hint={detail.messages.length > 0 ? `Peak row ${formatCurrencyWithProvenance(detailMessageStats.hottestMessageCost, detail.cost_status, detail.cost_provenance)} · ${formatCurrencyWithProvenance(safeDivide(detail.total_cost, detail.messages.length), detail.cost_status, detail.cost_provenance)} average per recorded row` : 'No message rows'}
        />
        <DetailMetric
          label="Token load"
          value={formatTokenCount(getTokenTotal(detail.total_tokens))}
          tooltipValue={`${formatInteger(getTokenTotal(detail.total_tokens))} tokens`}
          hint={detailMessageStats.heaviestTokenTotal > 0 ? `Heaviest row ${formatTokenCount(detailMessageStats.heaviestTokenTotal)} · ${formatTokenCount(detail.total_tokens.input)} input · ${formatTokenCount(detail.total_tokens.output)} output` : 'No token activity recorded'}
        />
        <DetailMetric
          label="Capture window"
          value={formatSessionWindow(detail.time_created, detail.time_updated)}
          hint={`Created ${formatDateTime(detail.time_created)} · updated ${formatDateTime(detail.time_updated)}`}
        />
      </div>

      <div className="grid min-w-0 gap-4 2xl:grid-cols-[minmax(0,1.55fr)_minmax(18rem,22rem)]">
        <div className="min-w-0 space-y-4">
          <Card className="border-border/70 bg-panel/45">
            <CardHeader className="gap-3 md:flex-row md:items-end md:justify-between">
              <CardDescription>Primary review surface</CardDescription>
              <div className="space-y-1.5">
                <CardTitle>Message timeline</CardTitle>
                <p className="text-sm text-muted-foreground">Scan role, time, spend, and total tokens first. Model, provider, agent, and ids stay present but quieter.</p>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border/60 bg-background/35 px-3 py-2 text-sm text-muted-foreground">
                <span className="font-mono text-foreground">{formatInteger(detailMessageMix.user)}</span>
                <span>user</span>
                <span className="text-border">·</span>
                <span className="font-mono text-foreground">{formatInteger(detailMessageMix.assistant)}</span>
                <span>assistant</span>
                {detailMessageMix.other > 0 ? (
                  <>
                    <span className="text-border">·</span>
                    <span className="font-mono text-foreground">{formatInteger(detailMessageMix.other)}</span>
                    <span>other</span>
                  </>
                ) : null}
                {detailMessageStats.hottestMessageCost > 0 ? (
                  <>
                    <span className="text-border">·</span>
                    <span>peak row {formatCurrencyWithProvenance(detailMessageStats.hottestMessageCost, detail.cost_status, detail.cost_provenance)}</span>
                  </>
                ) : null}
              </div>

              {detail.messages.length === 0 ? (
                <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-5 text-sm text-muted-foreground">
                  This session exists, but the detail endpoint returned no message rows.
                </div>
              ) : (
                <div className="divide-y divide-border/40 overflow-hidden rounded-xl border border-border/60">
                  {detail.messages.map((message, index) => (
                    <SessionMessageRow
                      key={message.id}
                      message={message}
                      previousMessage={index > 0 ? detail.messages[index - 1] : undefined}
                      isHighestCost={detailMessageStats.hottestMessageId === message.id}
                      isHighestTokens={detailMessageStats.heaviestTokenMessageId === message.id}
                    />
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        <div className="order-last min-w-0 space-y-4 2xl:order-none 2xl:pt-1">
          <Card className="border-border/60 bg-background/25 shadow-none">
            <CardHeader>
              <CardDescription>Secondary context</CardDescription>
              <CardTitle>Session facts</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <div className="flex items-center gap-2 rounded-xl border border-border/60 bg-background/35 px-3 py-2 text-xs leading-5 text-muted-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default font-mono text-foreground transition-opacity hover:opacity-80">
                        {formatTokenCount(getTokenTotal(detail.total_tokens))}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(getTokenTotal(detail.total_tokens))} tokens</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                <span>window token load</span>
                <span className="text-border">·</span>
                <span>{formatCurrencyWithProvenance(detail.total_cost, detail.cost_status, detail.cost_provenance)} spend</span>
              </div>

              <div className="rounded-xl border border-border/60 bg-background/35 px-3 py-3">
                <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Token mix</div>
                <TokenBreakdownList
                  className="mt-3 border-t border-border/50 pt-3"
                  hideZeroItems
                  tokens={detail.total_tokens}
                  variant="compact"
                />
              </div>

              <div className="grid gap-3 sm:grid-cols-2 2xl:grid-cols-1">
                <DetailFact label="Project" value={getSessionProjectLabel(detail)} />
                <DetailFact label="Source" value={sourceLabel} />
                <DetailFact label="Directory" value={detail.directory || 'No directory recorded'} subtle={!detail.directory} />
                <DetailFact label="Last update" value={formatDateTime(detail.time_updated)} />
                <DetailFact
                  label="Peak row"
                  value={detailMessageStats.hottestMessageCost > 0 ? formatCurrencyWithProvenance(detailMessageStats.hottestMessageCost, detail.cost_status, detail.cost_provenance) : 'No spend signal'}
                  subtle={detailMessageStats.hottestMessageCost <= 0}
                />
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
