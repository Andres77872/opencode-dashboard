import { Card, CardContent } from '../ui/card'
import { Skeleton } from '../ui/skeleton'

export function ConfigSkeleton() {
  return (
    <div className="space-y-6">
      {/* Summary stat cards */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index} className="border-border/60 bg-card/60">
            <CardContent className="p-3 sm:p-4">
              <Skeleton className="h-3 w-14" />
              <Skeleton className="mt-2 h-6 w-16" />
              <Skeleton className="mt-1 h-3 w-24" />
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Search bar skeleton */}
      <Skeleton className="h-9 w-full rounded-md" />

      {/* Tabs skeleton */}
      <div className="space-y-3">
        <Skeleton className="h-9 w-96 rounded-lg" />

        <Card className="border-border/60 bg-card/50">
          <CardContent className="p-4">
            <div className="divide-y divide-border/20">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 py-2.5">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3 w-16" />
                  <Skeleton className="ml-auto h-4 w-4" />
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
