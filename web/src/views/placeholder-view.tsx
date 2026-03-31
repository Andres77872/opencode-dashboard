import { Badge } from '../components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'

interface PlaceholderViewProps {
  title: string
  description: string
}

export function PlaceholderView({ title, description }: PlaceholderViewProps) {
  return (
    <section className="space-y-6">
      <div className="space-y-2">
        <Badge>Planned slice</Badge>
        <h2 className="text-2xl font-semibold tracking-tight text-foreground">{title}</h2>
        <p className="max-w-3xl text-sm text-muted-foreground">{description}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Intentionally deferred</CardTitle>
          <CardDescription>
            This route exists to prove the shell and IA, but its real data table/chart lands in a later web slice.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-sm leading-6 text-muted-foreground">
            This route is present to prove the shell and information architecture. Real data work is intentionally deferred until the next slice.
          </p>
        </CardContent>
      </Card>
    </section>
  )
}
