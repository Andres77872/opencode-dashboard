import { useState } from 'react'
import { Icon } from '../vael/icon'
import {
  formatDurationMs,
  humanizeAgentName,
  toolFailure,
  usageSummary,
  type Activity,
  type ActivityStatus,
  type SpecialistActivity,
  type ToolActivity,
} from '../../lib/assistant-activity'

function statusLabel(status: ActivityStatus): string {
  return status === 'running' ? 'Running' : status === 'complete' ? 'Complete' : 'Failed'
}

function formatJSON(value: unknown): string {
  if (value === undefined) return ''
  try {
    return JSON.stringify(value, null, 2) ?? ''
  } catch {
    return String(value)
  }
}

function StatusGlyph({ status }: { status: ActivityStatus }) {
  return (
    <span className="analytics-assistant-tool-state" aria-hidden="true">
      {status === 'running' ? <i /> : <Icon name={status === 'complete' ? 'check' : 'x'} size={11} />}
    </span>
  )
}

/** One analytics tool call, with its exact input and output on demand. */
function ToolCard({ tool, nested = false }: { tool: ToolActivity; nested?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const argumentsJSON = formatJSON(tool.arguments)
  const resultJSON = formatJSON(tool.result)
  const expandable = argumentsJSON !== '' || resultJSON !== ''
  const duration = formatDurationMs(tool.durationMs)
  const failure = tool.status === 'failed' ? toolFailure(tool.result) : undefined

  return (
    <div className={`analytics-assistant-tool-call ${tool.status}${nested ? ' nested' : ''}`}>
      <button
        type="button"
        className="analytics-assistant-tool-call-header"
        onClick={() => expandable && setExpanded((current) => !current)}
        aria-expanded={expandable ? expanded : undefined}
        disabled={!expandable}
        title={expandable ? 'Show tool input and output' : undefined}
      >
        <StatusGlyph status={tool.status} />
        <Icon name="wrench" size={12} />
        <span className="analytics-assistant-tool-name">{humanizeAgentName(tool.name)}</span>
        <small>
          {failure ? failure.code.replace(/_/g, ' ') : statusLabel(tool.status)}
          {duration && tool.status !== 'running' ? ` · ${duration}` : ''}
        </small>
        {expandable && (
          <span className="analytics-assistant-tool-chevron" aria-hidden="true">
            <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={13} />
          </span>
        )}
      </button>
      {failure && <p className="analytics-assistant-tool-error">{failure.message}</p>}
      {expanded && expandable && (
        <div className="analytics-assistant-tool-io">
          {argumentsJSON !== '' && (
            <div>
              <span className="analytics-assistant-tool-io-label">Input</span>
              <pre>{argumentsJSON}</pre>
            </div>
          )}
          {resultJSON !== '' && (
            <div>
              <span className="analytics-assistant-tool-io-label">Output</span>
              <pre>{resultJSON}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/** One delegated specialist run: its task, its finding, and the calls it made. */
function SpecialistCard({ run }: { run: SpecialistActivity }) {
  const [expanded, setExpanded] = useState(false)
  const duration = formatDurationMs(run.durationMs)
  const usage = usageSummary(run.usage)
  const detail = [
    run.rounds ? `${run.rounds} ${run.rounds === 1 ? 'round' : 'rounds'}` : '',
    run.children.length ? `${run.children.length} ${run.children.length === 1 ? 'call' : 'calls'}` : '',
    duration,
  ].filter(Boolean).join(' · ')

  return (
    <div className={`analytics-assistant-specialist ${run.status}`}>
      <button
        type="button"
        className="analytics-assistant-specialist-header"
        onClick={() => setExpanded((current) => !current)}
        aria-expanded={expanded}
        title={run.task || undefined}
      >
        <StatusGlyph status={run.status} />
        <Icon name="users" size={12} />
        <span className="analytics-assistant-tool-name">{run.title || humanizeAgentName(run.agent)}</span>
        <small>{run.status === 'running' ? 'Investigating' : detail || statusLabel(run.status)}</small>
        <span className="analytics-assistant-tool-chevron" aria-hidden="true">
          <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={13} />
        </span>
      </button>
      {run.error && <p className="analytics-assistant-tool-error">{run.error}</p>}
      {expanded && (
        <div className="analytics-assistant-specialist-body">
          {run.task && (
            <div className="analytics-assistant-specialist-task">
              <span className="analytics-assistant-tool-io-label">Task</span>
              <p>{run.task}</p>
            </div>
          )}
          {run.report && (
            <div className="analytics-assistant-specialist-report">
              <span className="analytics-assistant-tool-io-label">Finding</span>
              <p>{run.report}</p>
            </div>
          )}
          {run.children.length > 0 && (
            <div className="analytics-assistant-specialist-tools">
              {run.children.map((tool) => <ToolCard key={tool.id} tool={tool} nested />)}
            </div>
          )}
          {usage && <p className="analytics-assistant-specialist-usage">{usage}</p>}
        </div>
      )}
    </div>
  )
}

/** The evidence-gathering trail for one assistant answer. */
export function ActivityTrail({ activities }: { activities: readonly Activity[] }) {
  if (activities.length === 0) return null
  return (
    <div className="analytics-assistant-tool-activity" aria-label="Analytics evidence and specialist activity">
      {activities.map((activity) => (
        activity.kind === 'specialist'
          ? <SpecialistCard key={activity.id} run={activity} />
          : <ToolCard key={activity.id} tool={activity} />
      ))}
    </div>
  )
}
