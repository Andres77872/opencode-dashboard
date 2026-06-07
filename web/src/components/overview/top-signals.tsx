import type { ModelEntry, ProjectEntry, SourceID, ToolEntry } from '../../types/api'
import { DataTable, type DataTableColumn } from '../common/data-table'
import { Badge } from '../ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { formatCurrencyWithProvenance, formatInteger, formatTokenCount } from '../../lib/format'
import { getTokenTotal } from '../../lib/token-breakdown'

interface TopSignalsProps {
  models: ModelEntry[]
  projects: ProjectEntry[]
  tools: ToolEntry[]
  labelFor: (id?: SourceID) => string
}

function sourceCell(labelFor: (id?: SourceID) => string, id?: SourceID) {
  return <Badge tone="default">{labelFor(id)}</Badge>
}

/**
 * Top models, projects, and tools merged across every source. Each row is tagged
 * with its source; entries are ranked server-side by a cost-neutral metric.
 */
export function TopSignals({ models, projects, tools, labelFor }: TopSignalsProps) {
  const modelColumns: DataTableColumn<ModelEntry>[] = [
    { key: 'source', label: 'Source', render: (r) => sourceCell(labelFor, r.source_id) },
    { key: 'model', label: 'Model', render: (r) => <span className="font-mono text-sm">{r.model_id || '—'}</span> },
    { key: 'tokens', label: 'Tokens', render: (r) => <span className="font-mono">{formatTokenCount(getTokenTotal(r.tokens))}</span> },
    { key: 'cost', label: 'Cost', desktopOnly: true, render: (r) => <span className="font-mono">{formatCurrencyWithProvenance(r.cost, r.cost_status, r.cost_provenance)}</span> },
  ]

  const projectColumns: DataTableColumn<ProjectEntry>[] = [
    { key: 'source', label: 'Source', render: (r) => sourceCell(labelFor, r.source_id) },
    { key: 'project', label: 'Project', render: (r) => <span className="text-sm">{r.project_name || r.project_id || '—'}</span> },
    { key: 'tokens', label: 'Tokens', render: (r) => <span className="font-mono">{formatTokenCount(getTokenTotal(r.tokens))}</span> },
    { key: 'cost', label: 'Cost', desktopOnly: true, render: (r) => <span className="font-mono">{formatCurrencyWithProvenance(r.cost, r.cost_status, r.cost_provenance)}</span> },
  ]

  const toolColumns: DataTableColumn<ToolEntry>[] = [
    { key: 'source', label: 'Source', render: (r) => sourceCell(labelFor, r.source_id) },
    { key: 'tool', label: 'Tool', render: (r) => <span className="font-mono text-sm">{r.name}</span> },
    { key: 'runs', label: 'Runs', render: (r) => <span className="font-mono">{formatInteger(r.invocations)}</span> },
  ]

  return (
    <div className="grid gap-4 xl:grid-cols-3">
      <Card>
        <CardHeader>
          <CardDescription>Top models</CardDescription>
          <CardTitle>By token usage</CardTitle>
        </CardHeader>
        <CardContent>
          <DataTable rows={models} columns={modelColumns} sortState={null} rowKey={(r) => `${r.source_id}:${r.model_id}`} emptyState="No model data." />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription>Top projects</CardDescription>
          <CardTitle>By token usage</CardTitle>
        </CardHeader>
        <CardContent>
          <DataTable rows={projects} columns={projectColumns} sortState={null} rowKey={(r) => `${r.source_id}:${r.project_id}`} emptyState="No project data." />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardDescription>Top tools</CardDescription>
          <CardTitle>By invocations</CardTitle>
        </CardHeader>
        <CardContent>
          <DataTable rows={tools} columns={toolColumns} sortState={null} rowKey={(r) => `${r.source_id}:${r.name}`} emptyState="No tool data." />
        </CardContent>
      </Card>
    </div>
  )
}
