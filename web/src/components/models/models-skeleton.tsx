import { Card, CardContent, CardHeader } from '../ui/card'
import { Skeleton } from '../ui/skeleton'

export function ModelsSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index}>
            <CardHeader>
              <Skeleton className="h-3 w-24" />
              <Skeleton className="mt-3 h-9 w-32" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-4 w-40" />
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <Skeleton className="h-3 w-28" />
          <Skeleton className="mt-3 h-8 w-52" />
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="hidden grid-cols-[minmax(12rem,1.5fr)_9rem_5.5rem_5.5rem_7rem_7rem_7rem_7rem] gap-3 rounded-xl border border-border/60 bg-panel/60 px-4 py-3 lg:grid">
            {Array.from({ length: 8 }).map((_, index) => (
              <Skeleton key={index} className="h-4 w-full" />
            ))}
          </div>

          {Array.from({ length: 7 }).map((_, index) => (
            <Skeleton key={index} className="h-20 w-full rounded-2xl" />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
