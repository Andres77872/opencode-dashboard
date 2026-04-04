import { Label, Pie, PieChart } from 'recharts'
import type { TokenStats } from '../../types/api'
import { formatPercentage, formatTokenCount } from '../../lib/format'
import { getTokenBreakdownItems, getTokenTotal } from '../../lib/token-breakdown'
import { tokenBreakdownChartConfig } from '../../lib/chart-config'
import { transformTokensToSlices } from '../../lib/chart-transform'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from '../ui/chart'
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
      {visibleData.map((item) => {
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
  const formattedTotal = formatTokenCount(total)
  const slices = transformTokensToSlices(tokens)

  const visibleSlices = hideZeroItems ? slices.filter((s) => s.value > 0) : slices

  return (
    <Card className={cn('h-full border-border/70 bg-linear-to-b from-card to-panel', className)}>
      <CardHeader>
        <CardDescription>{description}</CardDescription>
        <CardTitle className="font-mono text-2xl">{formattedTotal}</CardTitle>
        {title ? <div className="text-sm font-medium text-foreground">{title}</div> : null}
      </CardHeader>
      <CardContent>
        <ChartContainer config={tokenBreakdownChartConfig} className="mx-auto min-h-[200px] w-full">
          <PieChart accessibilityLayer>
            <ChartTooltip
              content={
                <ChartTooltipContent
                  nameKey="name"
                  formatter={(value) =>
                    typeof value === 'number' ? formatTokenCount(value) : String(value)
                  }
                />
              }
            />
            <Pie
              data={visibleSlices}
              dataKey="value"
              nameKey="name"
              innerRadius="60%"
              outerRadius="80%"
              strokeWidth={2}
              stroke="var(--color-card)"
              paddingAngle={2}
            >
              <Label
                content={({ viewBox }) => {
                  if (viewBox && 'cx' in viewBox && 'cy' in viewBox) {
                    return (
                      <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                        <tspan
                          x={viewBox.cx}
                          y={viewBox.cy}
                          className="fill-foreground text-2xl font-bold"
                        >
                          {formattedTotal}
                        </tspan>
                        <tspan
                          x={viewBox.cx}
                          y={(viewBox.cy || 0) + 20}
                          className="fill-muted-foreground text-xs"
                        >
                          total tokens
                        </tspan>
                      </text>
                    )
                  }
                }}
              />
            </Pie>
            <ChartLegend content={<ChartLegendContent nameKey="name" />} />
          </PieChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
