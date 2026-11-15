/* Request detail — the drill-down behind a row of the Daily message history.
   Loads one message for the selected source and shows what it cost, the
   tokens it used and what was said. */
import { useState } from 'react'
import { Badge, Card, CompositionBar, Drawer, ErrorState, Notice, Skeleton } from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { getMessageDetail } from '../../lib/api'
import { useCachedResource } from '../../lib/use-cached-resource'
import {
  formatCostProvenance,
  formatCount,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatInteger,
  formatShortId,
  formatTokenCount,
  getCostLabel,
  isMissingCost,
} from '../../lib/format'
import { getDetailLoadingCopy, getDetailTitle, getRoleTone } from '../../lib/message-display'
import { getProcessingModePricingDisclosure, getProcessingModePricingLabel } from '../../lib/processing-mode'
import { usageUnavailableReasonDetail } from '../../lib/request-accounting'
import { getTokenBreakdownItems, getTokenTotal } from '../../lib/token-breakdown'
import type { MessageDetail, MessagePart, SourceID, ToolPart, TruncationInfo } from '../../types/api'
import { RequestedTierBadge, UsageStatusBadge } from './request-badges'

const TOOL_PREVIEW_CHARS = 400

export function RequestDetailDrawer({ messageId, onClose }: { messageId: string | null; onClose: () => void }) {
  const { selectedSourceId, selectedSourceInfo, sourceMetadata, sourceMetadataLoading } = useDashboardContext()
  const [retry, setRetry] = useState(0)
  const revision = sourceMetadata?.sources_revision ?? 0

  const { data: detail, loading, error } = useCachedResource<MessageDetail>({
    key: `daily-request-detail|${revision}:${selectedSourceId}::${messageId ?? ''}`,
    ready: !sourceMetadataLoading,
    enabled: messageId !== null,
    // Bumped by Retry: the same message, asked for again.
    depsKey: String(retry),
    load: (signal) => getMessageDetail(messageId ?? '', signal, selectedSourceId),
    fallbackError: 'Failed to load request detail',
  })

  const pending = loading && !detail

  return (
    <Drawer
      open={messageId !== null}
      onClose={onClose}
      width={680}
      title={getDetailTitle(selectedSourceId, pending)}
      subtitle={detail ? <span title={detail.id}>{detail.id}</span> : undefined}
    >
      {detail ? (
        <RequestDetail detail={detail} sourceId={detail.source_id ?? selectedSourceId} sourceLabel={selectedSourceInfo?.label ?? selectedSourceId} />
      ) : error ? (
        <ErrorState title="Request detail failed to load" message={error} onRetry={() => setRetry((n) => n + 1)} />
      ) : (
        <div role="status" aria-label={getDetailLoadingCopy(selectedSourceId)} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Skeleton width="60%" height={20} />
          <Skeleton width="100%" height={80} />
          <Skeleton width="100%" height={120} />
        </div>
      )}
    </Drawer>
  )
}

function RequestDetail({ detail, sourceId, sourceLabel }: { detail: MessageDetail; sourceId: SourceID; sourceLabel: string }) {
  const tokenTotal = detail.tokens ? getTokenTotal(detail.tokens) : 0
  const isCodexAssistant = sourceId === 'codex' && detail.role === 'assistant'
  const usageUnavailable = detail.usage_status === 'unavailable'
  const cost = detail.cost ?? 0
  const provenanceHint = formatCostProvenance(detail.cost_status, detail.cost_provenance)
  const tierDisclosure = isCodexAssistant ? getProcessingModePricingDisclosure(detail.processing_mode, detail.service_tier) : null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
        <Badge tone={getRoleTone(detail.role)}>{detail.role || 'unknown'}</Badge>
        <Badge>{detail.session_title || 'Untitled session'}</Badge>
        <Badge>{sourceLabel}</Badge>
        {detail.is_subagent && <Badge tone="accent">subagent</Badge>}
        {isCodexAssistant && <RequestedTierBadge processingMode={detail.processing_mode} serviceTier={detail.service_tier} />}
        {detail.usage_status && <UsageStatusBadge status={detail.usage_status} reason={detail.usage_unavailable_reason} />}
        {detail.request_trace && <Badge>{detail.request_trace === 'observed' ? 'Request observed' : 'Request inferred'}</Badge>}
      </div>

      {usageUnavailable && (
        <Notice tone="warning" title="Usage unavailable">
          {usageUnavailableReasonDetail(detail.usage_unavailable_reason)} Tokens and the estimated cost are unknown, not zero.
        </Notice>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', gap: 10 }}>
        <DetailMetric
          label={isCodexAssistant ? getProcessingModePricingLabel(detail.processing_mode, detail.service_tier) : getCostLabel(detail.cost_status, detail.cost_provenance)}
          value={usageUnavailable ? 'Unavailable' : formatCurrencyWithProvenance(cost, detail.cost_status, detail.cost_provenance)}
          hint={usageUnavailable ? 'No usage was recorded' : (tierDisclosure ?? provenanceHint ?? detail.model_id ?? 'Model not recorded')}
          title={usageUnavailable || isMissingCost(detail.cost_status, detail.cost_provenance) ? undefined : formatCurrencyExact(cost)}
        />
        <DetailMetric
          label="Tokens"
          value={usageUnavailable ? 'Unavailable' : formatTokenCount(tokenTotal)}
          hint={
            usageUnavailable
              ? 'Unknown, not zero'
              : detail.tokens
                ? `${formatTokenCount(detail.tokens.input)} in · ${formatTokenCount(detail.tokens.output)} out`
                : 'No token usage recorded'
          }
          title={usageUnavailable ? undefined : `${formatInteger(tokenTotal)} tokens`}
        />
        <DetailMetric
          label="Recorded"
          value={formatDateTime(detail.time_created)}
          hint={`Session ${formatShortId(detail.session_id)}`}
          title={detail.session_id}
        />
      </div>

      {(detail.model_id || detail.provider_id || detail.agent) && (
        <div style={{ font: '400 12px/1.5 var(--font-mono)', color: 'var(--fg-muted)', overflowWrap: 'anywhere' }}>
          {[detail.model_id, detail.provider_id, detail.agent].filter(Boolean).join(' · ')}
        </div>
      )}

      {detail.tokens && tokenTotal > 0 && (
        <Card title="Token mix" pad={14}>
          <CompositionBar items={getTokenBreakdownItems(detail.tokens)} format={formatTokenCount} ariaLabel="Token mix" />
        </Card>
      )}

      <ContentSection title="Message text" parts={detail.content.text_parts} emptyCopy="No text was recorded for this message." />
      {detail.content.reasoning_parts.length > 0 && (
        <ContentSection title="Reasoning" parts={detail.content.reasoning_parts} emptyCopy="No reasoning was recorded." accent />
      )}
      <ToolSection parts={detail.content.tool_parts} />
    </div>
  )
}

function DetailMetric({ label, value, hint, title }: { label: string; value: string; hint: string; title?: string }) {
  return (
    <div title={title} style={{ minWidth: 0, background: 'var(--ink-800)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '10px 12px' }}>
      <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)' }}>{label}</div>
      <div style={{ font: '600 15px/1.2 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums', marginTop: 6, overflowWrap: 'anywhere' }}>{value}</div>
      <div style={{ font: '400 11px/1.4 var(--font-ui)', color: 'var(--fg-muted)', marginTop: 5, overflowWrap: 'anywhere' }}>{hint}</div>
    </div>
  )
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${formatInteger(bytes)} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** Says when the content on screen is not the whole recorded content. */
function ContentFlags({ truncation, redacted }: { truncation?: TruncationInfo; redacted?: boolean }) {
  const truncated = truncation?.truncated === true
  if (!truncated && !redacted) return null
  const sizes =
    truncated && truncation?.display_bytes != null && truncation.original_bytes != null
      ? `Showing ${formatBytes(truncation.display_bytes)} of ${formatBytes(truncation.original_bytes)}`
      : undefined
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 6, marginTop: 8 }}>
      {truncated && (
        <span title={sizes}>
          <Badge tone="warning">Truncated</Badge>
        </span>
      )}
      {redacted && <Badge>Redacted</Badge>}
      {sizes && <span style={{ font: '400 11px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>{sizes}</span>}
    </div>
  )
}

function ContentSection({ title, parts, emptyCopy, accent = false }: { title: string; parts: MessagePart[]; emptyCopy: string; accent?: boolean }) {
  return (
    <Card title={title} subtitle={parts.length > 0 ? formatCount(parts.length, 'part') : undefined} pad={14}>
      {parts.length === 0 ? (
        <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>{emptyCopy}</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {parts.map((part, i) => (
            <div
              key={i}
              style={{
                background: 'var(--ink-850)',
                border: `1px solid ${accent ? 'var(--border-accent)' : 'var(--border-subtle)'}`,
                borderRadius: 'var(--radius-md)',
                padding: '10px 12px',
              }}
            >
              <div style={{ font: '400 12px/1.6 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{part.text}</div>
              <ContentFlags truncation={part.truncation} redacted={part.redacted} />
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}

function toolStatusTone(status: string | undefined) {
  switch (status) {
    case 'completed':
      return 'success' as const
    case 'error':
      return 'danger' as const
    case 'running':
      return 'accent' as const
    case 'pending':
      return 'warning' as const
    default:
      return 'neutral' as const
  }
}

function preview(text: string) {
  return text.length > TOOL_PREVIEW_CHARS ? `${text.slice(0, TOOL_PREVIEW_CHARS)}…` : text
}

function toolResultPreview(part: ToolPart): string | null {
  const { state } = part
  if (state.status === 'error' && state.error) return preview(state.error)
  if (state.status === 'completed' && state.output) return preview(state.output)
  return null
}

function toolInputPreview(part: ToolPart): string | null {
  const input = part.state.input
  if (!input || Object.keys(input).length === 0) return null
  try {
    return preview(JSON.stringify(input))
  } catch {
    return null
  }
}

function ToolSection({ parts }: { parts: ToolPart[] }) {
  if (parts.length === 0) return null

  return (
    <Card title="Tool calls" subtitle={formatCount(parts.length, 'call')} pad={14}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {parts.map((part, i) => {
          const result = toolResultPreview(part)
          const input = toolInputPreview(part)
          const failed = part.state.status === 'error'
          return (
            <div key={`${part.call_id}:${i}`} style={{ background: 'var(--ink-850)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '10px 12px' }}>
              <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
                <span style={{ font: '600 12px/1.3 var(--font-mono)', color: 'var(--fg-primary)', overflowWrap: 'anywhere' }}>{part.tool || 'unknown tool'}</span>
                <Badge tone={toolStatusTone(part.state.status)}>{part.state.status || 'unknown'}</Badge>
              </div>
              {part.state.title && (
                <div style={{ marginTop: 6, font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-secondary)', overflowWrap: 'anywhere' }}>{part.state.title}</div>
              )}
              {input && <ToolText label="Input" text={input} />}
              {result && <ToolText label={failed ? 'Error' : 'Output'} text={result} danger={failed} />}
              <ContentFlags truncation={part.state.truncation} redacted={part.state.redacted} />
            </div>
          )
        })}
      </div>
    </Card>
  )
}

function ToolText({ label, text, danger = false }: { label: string; text: string; danger?: boolean }) {
  return (
    <div style={{ marginTop: 8 }}>
      <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 4 }}>{label}</div>
      <div style={{ font: '400 12px/1.5 var(--font-mono)', color: danger ? 'var(--danger)' : 'var(--fg-secondary)', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{text}</div>
    </div>
  )
}
