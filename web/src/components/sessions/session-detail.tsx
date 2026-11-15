/* Session detail — the full-page drill-down the Sessions view swaps to for
   ?session=<id>. Nothing here is fabricated: duration is time_updated −
   time_created, cache hit is derived from total_tokens, and costs always go
   through the *WithProvenance helpers. */
import { useMemo, useState } from 'react'
import type { CSSProperties } from 'react'
import {
  Badge,
  Button,
  Card,
  CompositionBar,
  ErrorState,
  Icon,
  Notice,
  SegmentedControl,
  StatCard,
  StatGrid,
  Tabs,
  VendorChip,
  ViewSkeleton,
  vendorMeta,
  type BadgeTone,
} from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { getSessionDetail } from '../../lib/api'
import { copyText } from '../../lib/clipboard'
import { useCachedResource } from '../../lib/use-cached-resource'
import { getRoleTone } from '../../lib/message-display'
import { getProcessingModeMeta, REQUESTED_TIER_DISCLOSURE } from '../../lib/processing-mode'
import { usageStatusLabel, usageUnavailableReasonDetail } from '../../lib/request-accounting'
import { getTokenBreakdownItems, getTokenTotal } from '../../lib/token-breakdown'
import {
  formatCostProvenance,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatDuration,
  formatInteger,
  formatShare,
  formatShortId,
  formatTokenCount,
  getCostLabel,
  isMissingCost,
  safeDivide,
} from '../../lib/format'
import type { SessionDetail, SessionMessage, SourceID } from '../../types/api'
import { getSessionLabel, getSessionProjectLabel } from './session-labels'

// A long-running agent session holds tens of thousands of requests. Rendering
// them all at once locks the tab, so the timeline grows a step at a time.
const TIMELINE_STEP = 200
// "Show all" is only offered while the remainder is small enough to render.
const TIMELINE_SHOW_ALL_LIMIT = 2000
// The raw payload of such a session runs to megabytes; the preview is capped
// and the copy button carries the whole document.
const RAW_PREVIEW_CHARS = 200_000

type DetailTab = 'timeline' | 'raw'
type TimelineOrder = 'oldest' | 'newest'
type CopyState = 'idle' | 'copied' | 'failed'

const META_ITEM: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 5, minWidth: 0 }

export interface SessionDetailScreenProps {
  id: string
  onBack: () => void
  /** Narrow the session list to one project (closes the detail). */
  onFilterProject: (projectId: string) => void
}

export function SessionDetailScreen({ id, onBack, onFilterProject }: SessionDetailScreenProps) {
  const {
    requestRefresh,
    selectedSourceId,
    selectedSourceInfo,
    sourceMetadata,
    sourceMetadataLoading,
    sourceAvailable,
    sourceStateError,
  } = useDashboardContext()
  const revision = sourceMetadata?.sources_revision ?? 0
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name

  // Not kept in the shared cache: one detail payload can be several megabytes.
  const { data: detail, loading, error } = useCachedResource<SessionDetail>({
    key: `session-detail|${revision}:${selectedSourceId}::${id}`,
    ready: !sourceMetadataLoading,
    blocked: sourceAvailable ? null : (sourceStateError?.message ?? 'Selected source is unavailable'),
    cache: false,
    load: (signal) => getSessionDetail(id, signal, selectedSourceId),
    fallbackError: 'Failed to load session detail',
  })

  const backButton = (
    <div style={{ marginBottom: 12 }}>
      <Button variant="ghost" size="sm" iconLeft="chevron-left" onClick={onBack}>All sessions</Button>
    </div>
  )

  if (!detail) {
    return (
      <div>
        {backButton}
        {loading ? (
          <ViewSkeleton stats={4} panels={[120, 320]} label="Loading session" />
        ) : (
          <Card>
            <ErrorState title="Session failed to load" message={error ?? undefined} onRetry={requestRefresh} />
          </Card>
        )}
      </div>
    )
  }

  return (
    <div>
      {backButton}
      {error && (
        <div style={{ marginBottom: 12 }}>
          <Notice
            tone="warning"
            title="Couldn't refresh — showing the last loaded data"
            action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}
          >
            {error}
          </Notice>
        </div>
      )}
      <SessionDetailBody detail={detail} fallbackSourceId={selectedSourceId} sourceLabel={sourceLabel} onFilterProject={onFilterProject} />
    </div>
  )
}

function SessionDetailBody({
  detail,
  fallbackSourceId,
  sourceLabel,
  onFilterProject,
}: {
  detail: SessionDetail
  fallbackSourceId: SourceID
  sourceLabel: string
  onFilterProject: (projectId: string) => void
}) {
  const [tab, setTab] = useState<DetailTab>('timeline')
  const sourceId = detail.source_id ?? fallbackSourceId
  const title = getSessionLabel(detail)
  const projectLabel = getSessionProjectLabel(detail)
  const messages = detail.messages

  const stats = useMemo(() => {
    const tokens = detail.total_tokens
    const roleCounts = new Map<string, number>()
    for (const message of detail.messages) {
      const role = message.role || 'unknown'
      roleCounts.set(role, (roleCounts.get(role) ?? 0) + 1)
    }
    const roles = [...roleCounts.entries()].sort((a, b) => b[1] - a[1])
    return {
      totalTokens: getTokenTotal(tokens),
      // Share of prompt tokens served from cache: read ÷ (fresh input + read).
      cacheHit: formatShare(tokens.cache.read, tokens.input + tokens.cache.read),
      breakdown: getTokenBreakdownItems(tokens),
      assistantCount: roleCounts.get('assistant') ?? 0,
      roles,
    }
  }, [detail])

  const costMissing = isMissingCost(detail.cost_status, detail.cost_provenance)
  const costNote = formatCostProvenance(detail.cost_status, detail.cost_provenance)
  // Only assistant messages carry spend; averaging over user and system rows
  // too would understate what a request costs.
  const costHint = costMissing || stats.assistantCount === 0
    ? 'this session'
    : `${formatCurrencyWithProvenance(safeDivide(detail.total_cost, stats.assistantCount), detail.cost_status, detail.cost_provenance)} / assistant message`
  const messagesHint = detail.message_count !== messages.length
    ? `${formatInteger(messages.length)} rows returned`
    : stats.roles.map(([role, count]) => `${formatInteger(count)} ${role}`).join(' · ') || 'no message rows'

  return (
    <>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12, marginBottom: 16 }}>
        <VendorChip id={sourceId} label={false} size={34} />
        <div style={{ minWidth: 0, flex: 1 }}>
          <h2
            title={title}
            style={{
              margin: 0,
              font: '700 18px/1.3 var(--font-ui)',
              color: 'var(--fg-primary)',
              overflowWrap: 'anywhere',
              display: '-webkit-box',
              WebkitLineClamp: 2,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}
          >
            {title}
          </h2>
          <div style={{ display: 'flex', alignItems: 'center', columnGap: 14, rowGap: 6, marginTop: 8, flexWrap: 'wrap', font: '400 12px/1.2 var(--font-mono)', color: 'var(--fg-muted)' }}>
            <span title={detail.id} style={{ color: 'var(--fg-faint)' }}>id {formatShortId(detail.id)}</span>
            {detail.project_id ? (
              <button
                type="button"
                onClick={() => onFilterProject(detail.project_id)}
                title={`Show all ${projectLabel} sessions`}
                style={{ ...META_ITEM, padding: 0, background: 'transparent', border: 'none', color: 'var(--blue-300)', font: 'inherit', cursor: 'pointer' }}
              >
                <Icon name="folder" size={13} />
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '28ch' }}>{projectLabel}</span>
              </button>
            ) : (
              <span style={META_ITEM}><Icon name="folder" size={13} />{projectLabel}</span>
            )}
            <span>{sourceLabel}</span>
            <span style={META_ITEM} title="Started"><Icon name="clock" size={13} />{formatDateTime(detail.time_created)}</span>
            <span title="Time between the first and last activity">{formatDuration(detail.time_created, detail.time_updated)}</span>
          </div>
          {detail.directory && (
            <div title={detail.directory} style={{ marginTop: 6, font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {detail.directory}
            </div>
          )}
        </div>
      </div>

      {/* KPI */}
      <div style={{ marginBottom: 12 }}>
        <StatGrid>
          <StatCard
            accent
            label="Tokens"
            value={formatTokenCount(stats.totalTokens)}
            title={`${formatInteger(stats.totalTokens)} tokens`}
            hint={`${formatTokenCount(detail.total_tokens.input)} in · ${formatTokenCount(detail.total_tokens.output)} out`}
          />
          <StatCard
            label={getCostLabel(detail.cost_status, detail.cost_provenance)}
            value={formatCurrencyWithProvenance(detail.total_cost, detail.cost_status, detail.cost_provenance)}
            title={costMissing ? (costNote ?? undefined) : [formatCurrencyExact(detail.total_cost), costNote].filter(Boolean).join(' — ')}
            hint={costHint}
          />
          <StatCard label="Messages" value={formatInteger(detail.message_count)} hint={messagesHint} />
          <StatCard label="Cache hit" value={stats.cacheHit} title="Cache-read tokens ÷ (input + cache-read tokens)" hint="of input read from cache" />
        </StatGrid>
      </div>

      {/* Token composition */}
      <Card title="Token composition" subtitle="How this session's tokens split by type" style={{ marginBottom: 12 }}>
        <CompositionBar items={stats.breakdown} format={formatTokenCount} ariaLabel="Token composition" />
      </Card>

      {/* Timeline / raw JSON */}
      <Card flush>
        <div style={{ padding: '0 16px' }}>
          <Tabs
            ariaLabel="Session detail sections"
            tabs={[
              { value: 'timeline', label: 'Timeline', count: formatInteger(messages.length) },
              { value: 'raw', label: 'Raw JSON' },
            ]}
            value={tab}
            onChange={setTab}
          />
        </div>
        {tab === 'timeline' ? <Timeline messages={messages} sourceId={sourceId} /> : <RawJson detail={detail} />}
      </Card>
    </>
  )
}

// ── Timeline ──────────────────────────────────────────────────

function Timeline({ messages, sourceId }: { messages: SessionMessage[]; sourceId: SourceID }) {
  const [order, setOrder] = useState<TimelineOrder>('oldest')
  const [visibleCount, setVisibleCount] = useState(TIMELINE_STEP)

  if (messages.length === 0) {
    return (
      <div style={{ padding: 16 }}>
        <Notice tone="info" title="No message rows">This session exists, but no message rows were recorded for it.</Notice>
      </div>
    )
  }

  const shownCount = Math.min(visibleCount, messages.length)
  const remaining = messages.length - shownCount
  // Rows are addressed by their position in the recorded order, which stays a
  // stable key whichever way the list is read (message ids can repeat).
  const positions = Array.from({ length: shownCount }, (_, i) => (order === 'oldest' ? i : messages.length - 1 - i))

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, rowGap: 8, flexWrap: 'wrap', padding: '12px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
        <span style={{ font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>
          Tokens and cost per message
        </span>
        <SegmentedControl<TimelineOrder>
          ariaLabel="Message order"
          size="sm"
          options={[
            { value: 'oldest', label: 'Oldest first' },
            { value: 'newest', label: 'Newest first' },
          ]}
          value={order}
          onChange={(next) => {
            setOrder(next)
            setVisibleCount(TIMELINE_STEP)
          }}
        />
      </div>
      <ol style={{ listStyle: 'none', margin: 0, padding: '0 16px' }}>
        {positions.map((position, i) => (
          <MessageRow key={position} message={messages[position]} sourceId={sourceId} last={i === positions.length - 1} />
        ))}
      </ol>
      {remaining > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, rowGap: 8, flexWrap: 'wrap', padding: '10px 16px', borderTop: '1px solid var(--border-subtle)' }}>
          <span aria-live="polite" style={{ font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
            Showing {formatInteger(shownCount)} of {formatInteger(messages.length)} messages
          </span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Button size="sm" onClick={() => setVisibleCount((count) => count + TIMELINE_STEP)}>
              Show {formatInteger(Math.min(TIMELINE_STEP, remaining))} more
            </Button>
            {remaining > TIMELINE_STEP && remaining <= TIMELINE_SHOW_ALL_LIMIT && (
              <Button size="sm" variant="ghost" onClick={() => setVisibleCount(messages.length)}>Show all</Button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function getUsageTone(status: SessionMessage['usage_status']): BadgeTone {
  return status === 'unavailable' ? 'danger' : 'warning'
}

function MessageRow({ message, sourceId, last }: { message: SessionMessage; sourceId: SourceID; last: boolean }) {
  const tokens = message.tokens ? getTokenTotal(message.tokens) : 0
  const meta = [message.model_id, message.provider_id, message.agent].filter(Boolean).join(' · ')
  const requestedMode = getProcessingModeMeta(message.processing_mode, message.service_tier)
  const usageUnavailable = message.usage_status === 'unavailable'
  // User and system rows carry no usage of their own: a dash, not "$0.00".
  const hasUsage = message.cost != null || message.tokens != null
  const muted: CSSProperties = { color: 'var(--fg-faint)' }

  return (
    <li style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 12, padding: '10px 0', borderBottom: last ? 'none' : '1px solid var(--border-subtle)' }}>
      <div style={{ minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, rowGap: 6, flexWrap: 'wrap' }}>
          <Badge tone={getRoleTone(message.role)}>{message.role || 'unknown'}</Badge>
          {message.is_subagent && <Badge>subagent</Badge>}
          {sourceId === 'codex' && message.role === 'assistant' && (
            <span title={REQUESTED_TIER_DISCLOSURE}>
              <Badge tone={requestedMode.tone} dot>{requestedMode.label}</Badge>
            </span>
          )}
          {/* "Recorded" is the normal case; only the exceptions are called out. */}
          {message.usage_status && message.usage_status !== 'recorded' && (
            <span title={usageUnavailable ? usageUnavailableReasonDetail(message.usage_unavailable_reason) : undefined}>
              <Badge tone={getUsageTone(message.usage_status)}>{usageStatusLabel(message.usage_status, message.usage_unavailable_reason)}</Badge>
            </span>
          )}
          {message.request_trace === 'inferred' && <Badge>Request inferred</Badge>}
          <span style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }} title={message.id}>
            {formatDateTime(message.time_created)}
          </span>
        </div>
        {meta && (
          <div title={meta} style={{ marginTop: 5, font: '400 11px/1.4 var(--font-mono)', color: 'var(--fg-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {meta}
          </div>
        )}
      </div>
      <div style={{ textAlign: 'right', whiteSpace: 'nowrap', display: 'flex', flexDirection: 'column', gap: 5, alignItems: 'flex-end', fontVariantNumeric: 'tabular-nums' }}>
        {usageUnavailable ? (
          <span title="Tokens and cost are unknown for this request, not zero." style={{ font: '500 12px/1 var(--font-mono)', ...muted }}>Unavailable</span>
        ) : hasUsage ? (
          <>
            <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)' }} title={`${formatInteger(tokens)} tokens`}>
              {formatTokenCount(tokens)}
            </span>
            <span
              style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-muted)' }}
              title={isMissingCost(message.cost_status, message.cost_provenance) ? undefined : formatCurrencyExact(message.cost ?? 0)}
            >
              {formatCurrencyWithProvenance(message.cost ?? 0, message.cost_status, message.cost_provenance)}
            </span>
          </>
        ) : (
          <span aria-label="No usage" style={{ font: '500 12px/1 var(--font-mono)', ...muted }}>—</span>
        )}
      </div>
    </li>
  )
}

// ── Raw JSON ──────────────────────────────────────────────────

function RawJson({ detail }: { detail: SessionDetail }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  // Serialized only while this tab is open: for a long session it is megabytes.
  const raw = useMemo(() => JSON.stringify(detail, null, 2), [detail])
  const truncated = raw.length > RAW_PREVIEW_CHARS

  const copy = async () => {
    setCopyState((await copyText(raw)) ? 'copied' : 'failed')
    window.setTimeout(() => setCopyState('idle'), 2000)
  }

  return (
    <div style={{ padding: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, rowGap: 8, flexWrap: 'wrap', marginBottom: 10 }}>
        <span style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
          {truncated
            ? `Showing the first ${formatInteger(RAW_PREVIEW_CHARS)} of ${formatInteger(raw.length)} characters — copy for the full document.`
            : `${formatInteger(raw.length)} characters, as returned by the API.`}
        </span>
        <Button size="sm" iconLeft={copyState === 'copied' ? 'check' : 'copy'} onClick={() => void copy()}>
          {copyState === 'copied' ? 'Copied' : copyState === 'failed' ? 'Copy failed' : 'Copy JSON'}
        </Button>
      </div>
      <pre
        tabIndex={0}
        aria-label="Session detail JSON"
        style={{ margin: 0, font: '400 12px/1.7 var(--font-mono)', color: 'var(--fg-secondary)', background: 'var(--ink-850)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: 14, overflow: 'auto', maxHeight: 520 }}
      >
        {truncated ? `${raw.slice(0, RAW_PREVIEW_CHARS)}\n…` : raw}
      </pre>
    </div>
  )
}
