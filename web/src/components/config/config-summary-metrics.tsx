import type { ConfigStats } from '../../types/api'
import type { ConfigSummary } from '../../types/config'
import { formatInteger } from '../../lib/format'

interface ConfigSummaryMetricsProps {
  data: ConfigStats | null
  summary: ConfigSummary
}

export function ConfigSummaryMetrics({ data, summary }: ConfigSummaryMetricsProps) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
      <span className={data?.exists ? 'text-success' : 'text-warning'}>
        {data?.exists ? '● Present' : '● Missing'}
      </span>
      <span>·</span>
      <span>{formatInteger(summary.sections.length)} sections</span>
      <span>·</span>
      <span>{formatInteger(summary.insights.leafValues)} values</span>
      {summary.insights.redactedValues > 0 ? (
        <>
          <span>·</span>
          <span className="text-warning">{formatInteger(summary.insights.redactedValues)} redacted</span>
        </>
      ) : null}
    </div>
  )
}
