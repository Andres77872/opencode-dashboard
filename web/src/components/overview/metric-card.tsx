import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'

interface MetricCardProps {
  label: string
  value: string
  hint: string
}

export function MetricCard({ label, value, hint }: MetricCardProps) {
  return (
    <Card className="border-border/70 bg-linear-to-b from-card to-panel">
      <CardHeader>
        <CardDescription className="text-xs uppercase tracking-[0.18em]">{label}</CardDescription>
        <CardTitle className="font-mono text-3xl font-semibold text-foreground">{value}</CardTitle>
      </CardHeader>
      <CardContent className="pt-4">
        <p className="text-sm text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  )
}
