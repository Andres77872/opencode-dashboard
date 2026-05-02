import { Card, CardContent, CardHeader } from '../ui/card'
import { Skeleton } from '../ui/skeleton'

export type SkeletonSection = 'kpi-grid' | 'chart' | 'table' | 'chips'

interface DataPageSkeletonProps {
  sections: SkeletonSection[]
  kpiCount?: number
  tableRows?: number
}

/**
 * Parametric skeleton component that renders the requested sections.
 *
 * - `kpi-grid`: 2×2 grid of skeleton metric cards
 * - `chart`: chart-shaped placeholder
 * - `table`: header row + data rows
 * - `chips`: row of pill-shaped skeleton elements
 */
export function DataPageSkeleton({ sections, kpiCount = 4, tableRows = 6 }: DataPageSkeletonProps) {
  return (
    <div className="space-y-6">
      {sections.map((section) => {
        switch (section) {
          case 'kpi-grid':
            return (
              <div key="kpi-grid" className="grid gap-4 md:grid-cols-2 2xl:grid-cols-4">
                {Array.from({ length: kpiCount }).map((_, index) => (
                  <Card key={index}>
                    <CardHeader>
                      <Skeleton className="h-3 w-24" />
                      <Skeleton className="mt-3 h-9 w-28" />
                    </CardHeader>
                    <CardContent>
                      <Skeleton className="h-4 w-36" />
                    </CardContent>
                  </Card>
                ))}
              </div>
            )

          case 'chart':
            return (
              <Card key="chart">
                <CardHeader>
                  <Skeleton className="h-3 w-24" />
                  <Skeleton className="mt-3 h-8 w-44" />
                </CardHeader>
                <CardContent>
                  <Skeleton className="h-64 w-full rounded-2xl" />
                </CardContent>
              </Card>
            )

          case 'table':
            return (
              <Card key="table">
                <CardHeader>
                  <Skeleton className="h-3 w-28" />
                  <Skeleton className="mt-3 h-8 w-56" />
                </CardHeader>
                <CardContent className="space-y-3">
                  <Skeleton className="h-4 w-full rounded-xl" />
                  {Array.from({ length: tableRows }).map((_, index) => (
                    <Skeleton key={index} className="h-20 w-full rounded-2xl" />
                  ))}
                </CardContent>
              </Card>
            )

          case 'chips':
            return (
              <Card key="chips">
                <CardHeader>
                  <Skeleton className="h-3 w-28" />
                  <Skeleton className="mt-3 h-8 w-56" />
                </CardHeader>
                <CardContent>
                  <div className="flex flex-wrap gap-2">
                    {Array.from({ length: 3 }).map((_, index) => (
                      <Skeleton key={index} className="h-7 w-28 rounded-full" />
                    ))}
                  </div>
                </CardContent>
              </Card>
            )

          default:
            return null
        }
      })}
    </div>
  )
}
