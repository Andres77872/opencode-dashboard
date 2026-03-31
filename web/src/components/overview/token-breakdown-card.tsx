import type { TokenStats } from '../../types/api'
import { formatPercentage, formatTokenCount } from '../../lib/format'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Separator } from '../ui/separator'

interface TokenBreakdownCardProps {
  tokens: TokenStats
}

export function TokenBreakdownCard({ tokens }: TokenBreakdownCardProps) {
  const data = [
    { label: 'Input', value: tokens.input },
    { label: 'Output', value: tokens.output },
    { label: 'Reasoning', value: tokens.reasoning },
    { label: 'Cache read', value: tokens.cache.read },
    { label: 'Cache write', value: tokens.cache.write },
  ]
  const total = data.reduce((sum, item) => sum + item.value, 0)

  return (
    <Card className="h-full">
      <CardHeader>
        <CardDescription>Token breakdown</CardDescription>
        <CardTitle className="font-mono text-2xl">{formatTokenCount(total)}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {data.map((item, index) => {
          const share = total === 0 ? 0 : (item.value / total) * 100

          return (
            <div key={item.label} className="space-y-2">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="text-muted-foreground">{item.label}</span>
                <div className="text-right">
                  <div className="font-mono text-foreground">{formatTokenCount(item.value)}</div>
                  <div className="text-xs text-muted-foreground">{formatPercentage(share)}</div>
                </div>
              </div>
              <div className="h-2 rounded-full bg-muted/70">
                <div
                  className="h-full rounded-full bg-accent transition-[width]"
                  style={{ width: `${Math.max(share, total > 0 ? 4 : 0)}%` }}
                />
              </div>
              {index < data.length - 1 ? <Separator className="mt-4" /> : null}
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
}
