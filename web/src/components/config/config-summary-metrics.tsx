import type { ReactNode } from 'react'
import { Card, CardContent } from '../ui/card'
import type { ConfigStats } from '../../types/api'
import type { ConfigSummary } from '../../types/config'
import { formatInteger } from '../../lib/format'

interface ConfigSummaryMetricsProps {
  data: ConfigStats | null
  summary: ConfigSummary
}

export function ConfigSummaryMetrics({ data, summary }: ConfigSummaryMetricsProps) {
  const exists = data?.exists ?? false

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <StatCard
        label="Status"
        value={
          <span className={exists ? 'text-success' : 'text-warning'}>
            {exists ? 'Present' : 'Missing'}
          </span>
        }
        hint={exists ? 'Config file resolved and loaded' : 'No config file detected'}
      />
      <StatCard label="Sections" value={formatInteger(summary.sections.length)} hint="Top-level config groups" />
      <StatCard label="Values" value={formatInteger(summary.insights.leafValues)} hint="Leaf key-value pairs across all sections" />
      <StatCard
        label="Redacted"
        value={
          <span className={summary.insights.redactedValues > 0 ? 'text-warning' : 'text-muted-foreground'}>
            {formatInteger(summary.insights.redactedValues)}
          </span>
        }
        hint={summary.insights.redactedValues > 0 ? 'Sensitive fields masked' : 'No sensitive data to redact'}
      />
    </div>
  )
}

function StatCard({
  label,
  value,
  hint,
}: {
  label: string
  value: ReactNode
  hint: string
}) {
  return (
    <Card className="border-border/60 bg-card/60">
      <CardContent className="p-3 sm:p-4">
        <p className="text-[11px] uppercase tracking-[0.12em] text-muted-foreground">{label}</p>
        <p className="mt-1 font-mono text-lg font-semibold text-foreground">{value}</p>
        <p className="mt-0.5 text-xs text-muted-foreground/80">{hint}</p>
      </CardContent>
    </Card>
  )
}
