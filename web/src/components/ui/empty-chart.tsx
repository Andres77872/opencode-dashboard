import { BarChart3 } from 'lucide-react'
import { cn } from '../../lib/utils'

interface EmptyChartProps {
  className?: string
  message?: string
  icon?: React.ComponentType<{ className?: string }>
}

export function EmptyChart({
  className,
  message = 'No data available',
  icon: Icon = BarChart3,
}: EmptyChartProps) {
  return (
    <div className={cn('flex min-h-[200px] flex-col items-center justify-center gap-3 text-muted-foreground', className)}>
      <Icon className="size-10 opacity-30" />
      <p className="text-sm">{message}</p>
    </div>
  )
}