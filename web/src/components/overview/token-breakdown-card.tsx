import type { TokenStats } from '../../types/api'
import { formatPercentage, formatTokenCount } from '../../lib/format'
import { getTokenBreakdownItems, getTokenTotal } from '../../lib/token-breakdown'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Progress } from '../ui/progress'
import { Separator } from '../ui/separator'
import { cn } from '../../lib/utils'

interface TokenBreakdownCardProps {
  className?: string
  description?: string
  hideZeroItems?: boolean
  title?: string
  tokens: TokenStats
}

interface TokenBreakdownListProps {
  className?: string
  hideZeroItems?: boolean
  tokens: TokenStats
  variant?: 'stacked' | 'compact'
}

export function TokenBreakdownList({
  className,
  hideZeroItems = false,
  tokens,
  variant = 'stacked',
}: TokenBreakdownListProps) {
  const data = getTokenBreakdownItems(tokens)
  const total = getTokenTotal(tokens)
  const visibleData = hideZeroItems && total > 0 ? data.filter((item) => item.value > 0) : data

  if (variant === 'compact') {
    return (
      <div className={cn('flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px] text-muted-foreground', className)}>
        {visibleData.map((item) => (
          <span key={item.key} className="inline-flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className="size-1.5 rounded-full border border-white/12"
              style={{ backgroundColor: item.color }}
            />
            <span>{item.label}</span>
            <span className="font-mono text-foreground/85">{formatTokenCount(item.value)}</span>
          </span>
        ))}
      </div>
    )
  }

  return (
    <div className={cn('space-y-4', className)}>
      {visibleData.map((item, index) => {
        const share = total === 0 ? 0 : (item.value / total) * 100

        return (
          <div key={item.label} className="space-y-2">
            <div className="flex items-center justify-between gap-3 text-sm">
              <span className="flex items-center gap-2 text-muted-foreground">
                <span
                  aria-hidden="true"
                  className="size-2.5 rounded-full border border-white/12"
                  style={{ backgroundColor: item.color }}
                />
                {item.label}
              </span>
              <div className="text-right">
                <div className="font-mono text-foreground">{formatTokenCount(item.value)}</div>
                <div className="text-xs text-muted-foreground">{formatPercentage(share)}</div>
              </div>
            </div>
            <Progress value={Math.max(share, total > 0 ? 4 : 0)} indicatorStyle={{ backgroundColor: item.color }} />
            {index < visibleData.length - 1 ? <Separator className="mt-4" /> : null}
          </div>
        )
      })}
    </div>
  )
}

export function TokenBreakdownCard({
  className,
  description = 'Token breakdown',
  hideZeroItems = false,
  title,
  tokens,
}: TokenBreakdownCardProps) {
  const total = getTokenTotal(tokens)

  return (
    <Card className={cn('h-full', className)}>
      <CardHeader>
        <CardDescription>{description}</CardDescription>
        <CardTitle className="font-mono text-2xl">{formatTokenCount(total)}</CardTitle>
        {title ? <div className="text-sm font-medium text-foreground">{title}</div> : null}
      </CardHeader>
      <CardContent>
        <TokenBreakdownList hideZeroItems={hideZeroItems} tokens={tokens} />
      </CardContent>
    </Card>
  )
}
