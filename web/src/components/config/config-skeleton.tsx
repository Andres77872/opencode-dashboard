import { Card, CardContent, CardHeader } from '../ui/card'
import { Skeleton } from '../ui/skeleton'

export function ConfigSkeleton() {
  return (
    <div className="space-y-4">
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index}>
            <CardHeader>
              <Skeleton className="h-3 w-24" />
              <Skeleton className="mt-3 h-9 w-32" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-4 w-44" />
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <Skeleton className="h-3 w-40" />
          <Skeleton className="mt-3 h-8 w-72" />
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 xl:grid-cols-[1.2fr_0.8fr]">
            <div className="space-y-3">
              <Skeleton className="h-3 w-28" />
              <div className="flex gap-2">
                <Skeleton className="h-10 flex-1 rounded-md" />
                <Skeleton className="h-10 w-28 rounded-md" />
              </div>
              <Skeleton className="h-4 w-full max-w-xl" />
            </div>

            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
              <Skeleton className="h-24 w-full rounded-lg" />
              <Skeleton className="h-24 w-full rounded-lg" />
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="space-y-3">
        <Skeleton className="h-10 w-full max-w-3xl rounded-lg" />
        <Card>
          <CardHeader>
            <Skeleton className="h-3 w-28" />
            <Skeleton className="mt-3 h-8 w-52" />
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="grid gap-3 md:grid-cols-3">
              {Array.from({ length: 3 }).map((_, index) => (
                <Skeleton key={index} className="h-24 w-full rounded-lg" />
              ))}
            </div>
            <Skeleton className="h-56 w-full rounded-3xl" />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
