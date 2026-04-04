import { Skeleton } from './skeleton'
import { cn } from '../../lib/utils'

interface ChartSkeletonProps {
  className?: string
  barCount?: number
}

export function ChartSkeleton({ className, barCount = 7 }: ChartSkeletonProps) {
  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className="flex items-end gap-1.5 px-2" style={{ minHeight: '200px' }}>
        {Array.from({ length: barCount }).map((_, i) => {
          const heights = [75, 45, 90, 55, 80, 35, 65]
          return (
            <Skeleton
              key={i}
              className="flex-1 rounded-t-md"
              style={{ height: `${heights[i % heights.length]}%` }}
            />
          )
        })}
      </div>
      <div className="flex gap-1.5 px-2">
        {Array.from({ length: barCount }).map((_, i) => (
          <Skeleton key={i} className="h-3 flex-1" />
        ))}
      </div>
      <div className="flex justify-center gap-4 pt-2">
        <Skeleton className="h-3 w-16" />
        <Skeleton className="h-3 w-16" />
        <Skeleton className="h-3 w-16" />
      </div>
    </div>
  )
}

export function DonutSkeleton({ className }: { className?: string }) {
  return (
    <div className={cn('flex items-center justify-center', className)} style={{ minHeight: '200px' }}>
      <Skeleton className="size-40 rounded-full" />
    </div>
  )
}