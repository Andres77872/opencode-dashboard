import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { Skeleton } from '../ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

interface MetricCardProps {
  label: string
  value: string
  hint: string
  /** Optional full value to show in a tooltip on hover (useful for compressed numbers like token counts) */
  tooltipValue?: string
  /** Show skeleton loading state when data is being fetched */
  loading?: boolean
}

export function MetricCard({ label, value, hint, tooltipValue, loading }: MetricCardProps) {
  if (loading) {
    return (
      <Card className="gap-3 py-4">
        <CardHeader className="pb-2">
          <Skeleton className="h-3.5 w-24" />
          <Skeleton className="mt-2 h-7 w-20" />
        </CardHeader>
        <CardContent className="pt-0">
          <Skeleton className="h-3.5 w-full" />
        </CardContent>
      </Card>
    )
  }

  const valueElement = tooltipValue ? (
    <Tooltip>
      <TooltipTrigger asChild>
        <CardTitle className="font-mono text-2xl font-semibold text-foreground cursor-default transition-opacity hover:opacity-80">
          {value}
        </CardTitle>
      </TooltipTrigger>
      <TooltipContent side="top" className="font-mono">
        <p>{tooltipValue}</p>
      </TooltipContent>
    </Tooltip>
  ) : (
    <CardTitle className="font-mono text-2xl font-semibold text-foreground">{value}</CardTitle>
  )

  return (
    <Card className="gap-3 py-4 transition-colors hover:border-accent/40">
      <CardHeader className="pb-2">
        <p className="text-[11px] uppercase tracking-[0.14em] text-muted-foreground">{label}</p>
        {valueElement}
      </CardHeader>
      <CardContent className="pt-0">
        <p className="text-xs text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  )
}
