import type {
  PricingAlias,
  PricingObservedModel,
  PricingRepriceStatus,
  SourceID,
  TokenStats,
} from '../types/api'

/** A chosen pricing target, which may live in a different source's catalog. */
export interface PricingTargetRef {
  source_id: SourceID
  model_id: string
}

export interface PricingAliasRow {
  key: string
  source_id: SourceID
  provider_id: string
  model_id: string
  detected: boolean
  sessions: number
  messages: number
  tokens: TokenStats | null
  resolution_kind: PricingObservedModel['resolution_kind'] | null
  resolution_reason: string | null
  resolution_note: string | null
  /** Whether the model already prices without a user alias. */
  resolved: boolean
  aliasable: boolean
  target: PricingTargetRef | null
  alias: PricingAlias | null
  /** Whether a target may be chosen; only an unreadable catalog makes a row read-only. */
  editable: boolean
}

export interface RepriceStatusDisplay {
  tone: 'info' | 'warning' | 'success'
  title: string
  message: string
}

export function pricingAliasRowKey(sourceId: SourceID, providerId: string, modelId: string): string {
  return JSON.stringify([sourceId, providerId, modelId])
}

export function samePricingTarget(left: PricingTargetRef | null, right: PricingTargetRef | null): boolean {
  if (!left || !right) return left === right
  return left.source_id === right.source_id && left.model_id === right.model_id
}

export function mergePricingAliasRows(
  observedModels: PricingObservedModel[],
  aliases: PricingAlias[],
): PricingAliasRow[] {
  const rows = new Map<string, PricingAliasRow>()

  for (const observed of observedModels) {
    const key = pricingAliasRowKey(observed.source_id, observed.provider_id, observed.model_id)
    rows.set(key, {
      key,
      source_id: observed.source_id,
      provider_id: observed.provider_id,
      model_id: observed.model_id,
      detected: true,
      sessions: observed.sessions,
      messages: observed.messages,
      tokens: observed.tokens,
      resolution_kind: observed.resolution_kind,
      resolution_reason: observed.resolution_reason,
      resolution_note: observed.resolution_note ?? null,
      resolved: observed.resolved,
      aliasable: observed.aliasable,
      target: null,
      alias: null,
      editable: observed.aliasable,
    })
  }

  for (const alias of aliases) {
    const key = pricingAliasRowKey(alias.source_id, alias.provider_id, alias.model_id)
    const detected = rows.get(key)
    if (detected) {
      rows.set(key, {
        ...detected,
        target: { source_id: alias.target_source_id, model_id: alias.target_model_id },
        alias,
        editable: alias.editable,
      })
      continue
    }

    rows.set(key, {
      key,
      source_id: alias.source_id,
      provider_id: alias.provider_id,
      model_id: alias.model_id,
      detected: alias.detected,
      sessions: alias.sessions,
      messages: alias.messages,
      tokens: alias.detected ? alias.tokens : null,
      resolution_kind: null,
      resolution_reason: null,
      resolution_note: null,
      resolved: false,
      aliasable: alias.editable,
      target: { source_id: alias.target_source_id, model_id: alias.target_model_id },
      alias,
      editable: alias.editable,
    })
  }

  return Array.from(rows.values()).sort(comparePricingAliasRows)
}

function comparePricingAliasRows(left: PricingAliasRow, right: PricingAliasRow): number {
  // Rows that still cost nothing lead: they are the reason to open this view.
  const leftNeeds = pricingRowNeedsAttention(left)
  if (leftNeeds !== pricingRowNeedsAttention(right)) return leftNeeds ? -1 : 1
  if (left.detected !== right.detected) return left.detected ? -1 : 1
  if (left.detected && left.messages !== right.messages) return right.messages - left.messages

  const providerOrder = left.provider_id.localeCompare(right.provider_id)
  if (providerOrder !== 0) return providerOrder
  return left.model_id.localeCompare(right.model_id)
}

/**
 * Whether a row is unfinished business: an observed model with no usable
 * pricing, or a saved alias that is not currently supplying it.
 */
export function pricingRowNeedsAttention(row: PricingAliasRow): boolean {
  if (row.alias) return !row.alias.active
  return row.detected && !row.resolved
}

export function filterPricingAliasRows(rows: PricingAliasRow[], query: string): PricingAliasRow[] {
  const normalized = query.trim().toLocaleLowerCase()
  if (!normalized) return rows

  return rows.filter((row) =>
    [
      row.model_id,
      row.provider_id,
      formatPricingProvider(row.provider_id),
      row.target?.model_id ?? '',
      row.target?.source_id ?? '',
    ].some((value) => value.toLocaleLowerCase().includes(normalized)),
  )
}

export function formatPricingProvider(providerId: string): string {
  return providerId === '' ? 'Unknown provider' : providerId
}

/**
 * Rebuild target drafts after a refresh or a single-row mutation.
 *
 * Saved targets are authoritative for the rows they cover, but a user may have
 * selected targets for several rows before saving any of them: replacing the
 * whole draft map would silently discard that work. Unsaved selections are kept
 * unless the server now reports the same target for that row.
 */
export function mergePricingAliasDrafts(
  currentDrafts: Record<string, PricingTargetRef>,
  aliases: PricingAlias[],
  settledKeys: string[] = [],
): Record<string, PricingTargetRef> {
  const next: Record<string, PricingTargetRef> = {}
  const settled = new Set(settledKeys)

  for (const [key, target] of Object.entries(currentDrafts)) {
    if (!settled.has(key) && target.model_id !== '') next[key] = target
  }
  for (const alias of aliases) {
    next[pricingAliasRowKey(alias.source_id, alias.provider_id, alias.model_id)] = {
      source_id: alias.target_source_id,
      model_id: alias.target_model_id,
    }
  }
  return next
}

export function getRepriceStatusDisplay(status: PricingRepriceStatus): RepriceStatusDisplay {
  switch (status) {
    case 'started':
      return {
        tone: 'success',
        title: 'Historical repricing started',
        message: 'Cached historical usage is being repriced with the updated aliases.',
      }
    case 'queued':
      return {
        tone: 'info',
        title: 'Historical repricing queued',
        message: 'Another pricing update is running; this alias change will be repriced next.',
      }
    case 'disabled':
      return {
        tone: 'warning',
        title: 'Historical repricing disabled',
        message: 'The alias change is saved, but historical cache repricing is disabled.',
      }
    case 'unavailable':
      return {
        tone: 'warning',
        title: 'Historical repricing unavailable',
        message: 'The alias change is saved, but this runtime cannot reprice historical cached usage automatically.',
      }
  }
}
