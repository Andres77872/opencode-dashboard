import type { DayStats } from '../../types/api'
import { formatCompactInteger, formatCurrency, formatShortDate, formatShortWeekday, formatTokenCount } from '../../lib/format'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { cn } from '../../lib/utils'

interface DailyChartProps {
  days: DayStats[]
}

function getTotalTokens(day: DayStats) {
  return day.tokens.input + day.tokens.output + day.tokens.reasoning + day.tokens.cache.read + day.tokens.cache.write
}

function shouldShowLabel(index: number, total: number) {
  if (total <= 7) {
    return true
  }

  if (index === 0 || index === total - 1) {
    return true
  }

  return index % 5 === 0
}

export function DailyChart({ days }: DailyChartProps) {
  const maxCost = Math.max(...days.map((day) => day.cost), 0)
  const averageCost = days.length === 0 ? 0 : days.reduce((sum, day) => sum + day.cost, 0) / days.length
  const peakDay =
    days.find((day) => day.cost === maxCost) ??
    days[days.length - 1] ?? {
      date: '',
      sessions: 0,
      messages: 0,
      cost: 0,
      tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    }

  return (
    <Card className="border-border/70 bg-linear-to-b from-card to-panel">
      <CardHeader>
        <CardDescription>Spend trend</CardDescription>
        <CardTitle>Daily cost bars with session context</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Peak day</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatCurrency(maxCost)}</div>
            <div className="text-sm text-muted-foreground">{peakDay.date ? formatShortDate(peakDay.date) : 'No data'}</div>
          </div>
          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Average / day</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatCurrency(averageCost)}</div>
            <div className="text-sm text-muted-foreground">Zero-filled inactive days stay visible</div>
          </div>
          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Chart rule</div>
            <div className="mt-2 font-mono text-lg text-foreground">Cost-driven</div>
            <div className="text-sm text-muted-foreground">Hover any column for sessions, messages, and tokens</div>
          </div>
        </div>

        <div className="rounded-2xl border border-border/70 bg-background/40 p-4">
          <div className="flex h-64 items-end gap-2">
            {days.map((day, index) => {
              const height = maxCost > 0 ? Math.max((day.cost / maxCost) * 100, day.cost > 0 ? 8 : 2) : 2
              const active = day.cost > 0 || day.sessions > 0 || day.messages > 0 || getTotalTokens(day) > 0

              return (
                <div key={day.date} className="group flex min-w-0 flex-1 flex-col justify-end">
                  <div className="relative flex h-full items-end justify-center">
                    <div className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-3 hidden w-44 -translate-x-1/2 rounded-xl border border-border/70 bg-card/96 p-3 text-left shadow-2xl backdrop-blur group-hover:block group-focus-within:block">
                      <div className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
                        {formatShortDate(day.date)}
                      </div>
                      <div className="mt-2 space-y-1.5 text-sm">
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-muted-foreground">Cost</span>
                          <span className="font-mono text-foreground">{formatCurrency(day.cost)}</span>
                        </div>
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-muted-foreground">Sessions</span>
                          <span className="font-mono text-foreground">{formatCompactInteger(day.sessions)}</span>
                        </div>
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-muted-foreground">Messages</span>
                          <span className="font-mono text-foreground">{formatCompactInteger(day.messages)}</span>
                        </div>
                        <div className="flex items-center justify-between gap-3">
                          <span className="text-muted-foreground">Tokens</span>
                          <span className="font-mono text-foreground">{formatTokenCount(getTotalTokens(day))}</span>
                        </div>
                      </div>
                    </div>

                    <div
                      tabIndex={0}
                      aria-label={`${formatShortDate(day.date)} cost ${formatCurrency(day.cost)}, ${day.sessions} sessions, ${day.messages} messages, ${getTotalTokens(day)} tokens`}
                      className={cn(
                        'relative flex w-full items-end justify-center rounded-t-xl border border-b-0 px-1 outline-none transition-[filter,background-color] focus-visible:ring-2 focus-visible:ring-accent/70',
                        active
                          ? 'border-accent/35 bg-linear-to-t from-accent/90 via-accent/60 to-accent/20 group-hover:brightness-110 group-focus-within:brightness-110'
                          : 'border-border/70 bg-linear-to-t from-muted/80 to-muted/25',
                      )}
                      style={{ height: `${height}%` }}
                    >
                      <div className="absolute inset-x-[18%] top-1 h-3 rounded-full bg-white/18 blur-sm" />
                    </div>
                  </div>

                  <div className="mt-3 text-center text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                    {shouldShowLabel(index, days.length) ? formatShortWeekday(day.date) : '·'}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
