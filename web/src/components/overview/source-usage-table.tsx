import type { SourceOverview } from '../../types/api'
import { DataTable, type DataTableColumn } from '../common/data-table'
import { Badge } from '../ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { formatCurrencyWithProvenance, formatInteger, formatPercentage, formatTokenCount } from '../../lib/format'
import { getTokenTotal } from '../../lib/token-breakdown'

interface SourceUsageTableProps {
  sources: SourceOverview[]
}

const columns: DataTableColumn<SourceOverview>[] = [
  {
    key: 'source',
    label: 'Source',
    render: (row) => <Badge tone="accent">{row.label ?? row.source_id}</Badge>,
  },
  {
    key: 'sessions',
    label: 'Sessions',
    render: (row) => <span className="font-mono">{formatInteger(row.overview.sessions)}</span>,
  },
  {
    key: 'messages',
    label: 'Messages',
    render: (row) => (
      <span className="font-mono">
        {formatInteger(row.overview.messages)}
        <span className="ml-2 text-xs text-muted-foreground">{formatPercentage(row.message_share * 100)}</span>
      </span>
    ),
  },
  {
    key: 'tokens',
    label: 'Tokens',
    desktopOnly: true,
    render: (row) => (
      <span className="font-mono">
        {formatTokenCount(getTokenTotal(row.overview.tokens))}
        <span className="ml-2 text-xs text-muted-foreground">{formatPercentage(row.token_share * 100)}</span>
      </span>
    ),
  },
  {
    key: 'cost',
    label: 'Cost',
    render: (row) => (
      <span className="font-mono">
        {formatCurrencyWithProvenance(row.overview.cost, row.overview.cost_status, row.overview.cost_provenance)}
      </span>
    ),
  },
  {
    key: 'msgs_per_session',
    label: 'Msgs / session',
    desktopOnly: true,
    render: (row) => <span className="font-mono">{row.messages_per_session.toFixed(1)}</span>,
  },
]

/**
 * Per-source usage breakdown for the Overview. Cost is each source's own value
 * with its own provenance marker — never combined across sources.
 */
export function SourceUsageTable({ sources }: SourceUsageTableProps) {
  return (
    <Card>
      <CardHeader>
        <CardDescription>Usage by source</CardDescription>
        <CardTitle>Sessions, messages, tokens, and cost per source</CardTitle>
      </CardHeader>
      <CardContent>
        <DataTable
          rows={sources}
          columns={columns}
          sortState={null}
          rowKey={(row) => row.source_id}
          emptyState="No sources available for this range."
        />
      </CardContent>
    </Card>
  )
}
