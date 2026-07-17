import type { BadgeTone } from '../components/vael/atoms.tsx'
import { getTokenTotal } from './token-breakdown.ts'
import type { CostProvenance, CostStatus, DimensionDayStats, ProcessingMode } from '../types/api.ts'

export const REQUESTED_TIER_DISCLOSURE =
  'Local Codex request setting, not a server-confirmed processing outcome. USD estimates use official API per-token rates for the requested mode: Fast uses Priority rates, Flex uses Flex rates, and Standard uses Standard rates. Tier unknown remains unknown and falls back to Standard rates. These estimates are not actual billed spend.'

export interface ProcessingModeMeta {
  mode: ProcessingMode
  label: string
  shortLabel: string
  color: string
  tone: BadgeTone
}

export interface ProcessingModeUsage {
  mode: ProcessingMode
  messages: number
  tokens: number
  cost: number
  costStatus?: CostStatus
  costProvenance?: CostProvenance
}

export const PROCESSING_MODE_ORDER: ProcessingMode[] = ['fast', 'standard', 'flex', 'unknown']

const PROCESSING_MODE_META: Record<ProcessingMode, Omit<ProcessingModeMeta, 'mode'>> = {
  fast: {
    label: 'Fast requested',
    shortLabel: 'Fast',
    color: 'var(--cat-1)',
    tone: 'accent',
  },
  standard: {
    label: 'Standard requested',
    shortLabel: 'Standard',
    color: 'var(--cat-2)',
    tone: 'success',
  },
  flex: {
    label: 'Flex requested',
    shortLabel: 'Flex',
    color: 'var(--cat-4)',
    tone: 'warning',
  },
  unknown: {
    label: 'Tier unknown',
    shortLabel: 'Unknown',
    color: 'var(--fg-faint)',
    tone: 'neutral',
  },
}

function normalizeModeValue(value: string | undefined): ProcessingMode | null {
  switch (value?.trim().toLowerCase()) {
    case 'fast':
    case 'priority':
      return 'fast'
    case 'standard':
    case 'default':
      return 'standard'
    case 'flex':
      return 'flex'
    case 'unknown':
      return 'unknown'
    default:
      return null
  }
}

/**
 * Resolve a normalized display mode without implying the actual processing
 * outcome. The normalized API value wins;
 * raw service_tier is a compatibility fallback for older cached responses.
 */
export function resolveProcessingMode(
  processingMode?: string,
  serviceTier?: string,
): ProcessingMode {
  return normalizeModeValue(processingMode) ?? normalizeModeValue(serviceTier) ?? 'unknown'
}

export function getProcessingModeMeta(
  processingMode?: string,
  serviceTier?: string,
): ProcessingModeMeta {
  const mode = resolveProcessingMode(processingMode, serviceTier)
  return { mode, ...PROCESSING_MODE_META[mode] }
}

/** USD API catalog used for the locally requested mode. */
export function getProcessingModePricingLabel(
  processingMode?: string,
  serviceTier?: string,
): string {
  switch (resolveProcessingMode(processingMode, serviceTier)) {
    case 'fast':
      return 'Priority API estimate'
    case 'flex':
      return 'Flex API estimate'
    default:
      return 'Standard API estimate'
  }
}

/** Short request-level caveat that keeps requested and actually served tiers distinct. */
export function getProcessingModePricingDisclosure(
  processingMode?: string,
  serviceTier?: string,
): string {
  switch (resolveProcessingMode(processingMode, serviceTier)) {
    case 'fast':
      return 'Requested Fast → Priority API rates · served tier not recorded · not actual billed spend'
    case 'flex':
      return 'Requested Flex → Flex API rates · served tier not recorded · not actual billed spend'
    case 'standard':
      return 'Requested Standard → Standard API rates · served tier not recorded · not actual billed spend'
    default:
      return 'Tier unknown remains unknown → Standard API fallback · served tier not recorded · not actual billed spend'
  }
}

function mergeOptionalMetadata(left: string | undefined, right: string | undefined): string | undefined {
  if (!left) return right
  if (!right) return left
  return left === right ? left : undefined
}

function mergeUsageCostMetadata(total: ProcessingModeUsage, row: DimensionDayStats): void {
  const nextStatus = row.cost_status ?? row.cost_provenance?.status
  if (!nextStatus) return

  const currentStatus = total.costStatus ?? total.costProvenance?.status
  if (!currentStatus) {
    total.costStatus = nextStatus
    total.costProvenance = row.cost_provenance
      ? { ...row.cost_provenance, status: nextStatus }
      : { status: nextStatus }
    return
  }

  const status: CostStatus = currentStatus === nextStatus ? currentStatus : 'mixed'
  const current = total.costProvenance
  const next = row.cost_provenance
  total.costStatus = status
  total.costProvenance = {
    status,
    currency: mergeOptionalMetadata(current?.currency, next?.currency),
    pricing_snapshot_id: mergeOptionalMetadata(current?.pricing_snapshot_id, next?.pricing_snapshot_id),
    pricing_source: mergeOptionalMetadata(current?.pricing_source, next?.pricing_source),
    missing_count: (current?.missing_count ?? 0) + (next?.missing_count ?? 0),
    computed_count: (current?.computed_count ?? 0) + (next?.computed_count ?? 0),
    reported_count: (current?.reported_count ?? 0) + (next?.reported_count ?? 0),
    note: mergeOptionalMetadata(current?.note, next?.note),
  }
}

/** Sum assistant-request dimension rows into stable Fast/Standard/Flex/Unknown totals. */
export function aggregateProcessingModeUsage(rows: DimensionDayStats[] | undefined): ProcessingModeUsage[] {
  const totals = new Map<ProcessingMode, ProcessingModeUsage>(
    PROCESSING_MODE_ORDER.map((mode) => [mode, { mode, messages: 0, tokens: 0, cost: 0 }]),
  )

  for (const row of rows ?? []) {
    const mode = resolveProcessingMode(row.dimension_key)
    const total = totals.get(mode)!
    total.messages += row.messages
    total.tokens += getTokenTotal(row.tokens)
    total.cost += row.cost
    mergeUsageCostMetadata(total, row)
  }

  return PROCESSING_MODE_ORDER.map((mode) => totals.get(mode)!)
}
