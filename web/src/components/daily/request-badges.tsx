/* Badges shared by the Daily message history and its detail drawer. */
import { Badge } from '../vael'
import { getProcessingModeMeta, REQUESTED_TIER_DISCLOSURE } from '../../lib/processing-mode'
import { usageStatusLabel } from '../../lib/request-accounting'
import type { MessageEntry } from '../../types/api'

/** The processing tier the local Codex client asked for. It is a request setting, not a confirmed outcome. */
export function RequestedTierBadge({ processingMode, serviceTier }: { processingMode?: string; serviceTier?: string }) {
  const meta = getProcessingModeMeta(processingMode, serviceTier)
  return (
    <span title={REQUESTED_TIER_DISCLOSURE}>
      <Badge tone={meta.tone} dot>{meta.label}</Badge>
    </span>
  )
}

function usageStatusTone(status: MessageEntry['usage_status']) {
  switch (status) {
    case 'recorded':
      return 'success' as const
    case 'recovered':
      return 'warning' as const
    case 'unavailable':
      return 'danger' as const
    default:
      return 'neutral' as const
  }
}

/** How much token and cost evidence a request left behind (recorded, recovered or unavailable). */
export function UsageStatusBadge({ status, reason }: { status: MessageEntry['usage_status']; reason?: MessageEntry['usage_unavailable_reason'] }) {
  return <Badge tone={usageStatusTone(status)}>{usageStatusLabel(status, reason)}</Badge>
}
