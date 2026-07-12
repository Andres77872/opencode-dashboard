import { DataTable } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children, width }: { children?: ReactNode; width?: number }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, width }}>
      {children}
    </div>
  )
}

interface ProjectRow {
  project: string
  source: string
  tokens: string
  cost: string
  sessions: string
  share: number
}

const projectRows: ProjectRow[] = [
  { project: 'vael-api', source: 'opencode', tokens: '18.4M', cost: '$81.20', sessions: '468', share: 38.2 },
  { project: 'opencode-dashboard', source: 'claude_code', tokens: '12.9M', cost: '$56.70', sessions: '322', share: 26.8 },
  { project: 'argo-ui', source: 'opencode', tokens: '8.1M', cost: '$35.40', sessions: '214', share: 16.8 },
  { project: 'arz-embeddings', source: 'codex', tokens: '5.6M', cost: '$24.90', sessions: '168', share: 11.6 },
  { project: 'infra-tools', source: 'claude_code', tokens: '3.2M', cost: '$14.20', sessions: '112', share: 6.6 },
]

const projectColumns = [
  {
    key: 'project',
    header: 'Project',
    sortable: true,
    render: (r: ProjectRow) => (
      <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 3 }}>
        <span style={{ color: 'var(--fg-primary)', fontWeight: 500 }}>{r.project}</span>
        <span style={{ font: '400 10px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>{r.source}</span>
      </span>
    ),
  },
  { key: 'tokens', header: 'Tokens', numeric: true, sortable: true },
  { key: 'cost', header: 'Est. cost', numeric: true, sortable: true },
  { key: 'sessions', header: 'Sessions', numeric: true, sortable: true },
  {
    key: 'share',
    header: 'Share',
    numeric: true,
    width: 130,
    render: (r: ProjectRow) => (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
        <span style={{ width: 44, height: 4, borderRadius: 2, background: 'var(--ink-700)', overflow: 'hidden', display: 'inline-block' }}>
          <span style={{ display: 'block', width: `${r.share}%`, height: '100%', background: 'var(--accent)' }} />
        </span>
        <span>{r.share.toFixed(1)}%</span>
      </span>
    ),
  },
]

export const Projects = () => (
  <Canvas width={700}>
    <DataTable columns={projectColumns} rows={projectRows} sort={{ key: 'tokens', dir: 'desc' }} onSort={() => {}} />
  </Canvas>
)

interface ModelRow {
  model: string
  provider: string
  input: string
  output: string
  cost: string
}

const modelRows: ModelRow[] = [
  { model: 'claude-sonnet-5', provider: 'Anthropic', input: '19.8M', output: '4.8M', cost: '$118.60' },
  { model: 'gpt-5.6-codex', provider: 'OpenAI', input: '10.1M', output: '2.3M', cost: '$54.10' },
  { model: 'claude-haiku-4.5', provider: 'Anthropic', input: '6.0M', output: '1.1M', cost: '$23.30' },
  { model: 'minimax-m2', provider: 'MiniMax', input: '3.4M', output: '0.7M', cost: '$16.40' },
]

const modelColumns = [
  {
    key: 'model',
    header: 'Model',
    sortable: true,
    render: (r: ModelRow) => <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-primary)' }}>{r.model}</span>,
  },
  { key: 'provider', header: 'Provider', muted: true },
  { key: 'input', header: 'Input tokens', numeric: true, sortable: true },
  { key: 'output', header: 'Output tokens', numeric: true, sortable: true },
  { key: 'cost', header: 'Est. cost', numeric: true, sortable: true },
]

export const ModelsDense = () => (
  <Canvas width={660}>
    <DataTable columns={modelColumns} rows={modelRows} sort={{ key: 'cost', dir: 'desc' }} onSort={() => {}} dense />
  </Canvas>
)
