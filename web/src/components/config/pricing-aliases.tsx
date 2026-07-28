/*
  Pricing alias management for the selected source.

  An alias maps an observed model identifier onto a bundled pricing catalog
  model. Targets are not restricted to the selected source: a CLI frequently
  reports models another vendor prices (Claude Code driven through a proxy that
  serves GPT models, say), and only that vendor's catalog holds the right rates.
  A user alias also outranks native pricing, because name matching is a guess
  and the user knows what a proxied model actually is.
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Badge,
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  Notice,
  SearchInput,
  SegmentedControl,
  Skeleton,
  vendorMeta,
} from '../vael'
import type { BadgeTone, Column, NoticeTone } from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { PricingTargetPicker } from './pricing-target-picker'
import { deletePricingAlias, getPricingAliases, upsertPricingAlias } from '../../lib/api'
import { formatInteger } from '../../lib/format'
import {
  filterPricingAliasRows,
  formatPricingProvider,
  getRepriceStatusDisplay,
  mergePricingAliasDrafts,
  mergePricingAliasRows,
  pricingRowNeedsAttention,
  samePricingTarget,
} from '../../lib/pricing-aliases'
import type { PricingAliasRow, PricingTargetRef } from '../../lib/pricing-aliases'
import { buildPricingTargetGroups, countPricingTargets } from '../../lib/pricing-target-catalog'
import type { PricingAliasesResponse, PricingRepriceStatus, SourceID } from '../../types/api'

type PendingAction = 'save' | 'remove'
type RowScope = 'attention' | 'all'

interface RowFeedback {
  tone: NoticeTone
  title: string
  message: string
}

const SCOPE_OPTIONS = [
  { value: 'attention', label: 'Needs pricing' },
  { value: 'all', label: 'All models' },
]

function readOnlyTargetReason(row: PricingAliasRow, targetCount: number, writable: boolean): string {
  if (!writable) return 'Alias persistence is unavailable, so targets cannot be changed.'
  if (targetCount === 0) return 'No priced catalog target is available in any source.'
  if (row.resolution_kind === 'unavailable') return 'This source’s own pricing catalog could not be read, so an alias cannot be applied.'
  return 'This pricing state cannot be overridden.'
}

function shortSnapshotID(value: string): string {
  if (value.length <= 48) return value
  return `${value.slice(0, 30)}…${value.slice(-12)}`
}

function resolutionLabel(row: PricingAliasRow): string {
  switch (row.resolution_kind) {
    case 'unpriced':
      return 'Unpriced model'
    case 'unavailable':
      return 'Pricing unavailable'
    case 'exact':
      return 'Priced natively'
    case 'native_alias':
      return 'Priced via bundled alias'
    case 'fallback':
      return 'Priced approximately'
    default:
      return 'Unknown model'
  }
}

function resolutionTone(row: PricingAliasRow): BadgeTone {
  switch (row.resolution_kind) {
    case 'unavailable':
      return 'danger'
    case 'exact':
    case 'native_alias':
      return 'success'
    case 'fallback':
      return 'accent'
    default:
      return 'warning'
  }
}

function aliasStateBadge(row: PricingAliasRow): { tone: BadgeTone; label: string } {
  switch (row.alias?.state) {
    case 'active':
      return { tone: 'success', label: row.alias.overrides_native ? 'Alias overriding native' : 'Alias active' }
    case 'target_missing':
      return { tone: 'warning', label: 'Alias target missing' }
    case 'not_detected':
      return { tone: 'neutral', label: 'Alias saved · not detected' }
    default:
      return { tone: 'warning', label: 'Alias not applied' }
  }
}

function mutationFeedback(action: PendingAction, status: PricingRepriceStatus, refreshError?: string): RowFeedback {
  const reprice = getRepriceStatusDisplay(status)
  const saved = action === 'remove' ? 'Alias removed' : 'Alias saved'
  if (refreshError) {
    return {
      tone: 'warning',
      title: `${saved} · view not refreshed`,
      message: `${refreshError} ${reprice.message}`,
    }
  }
  return {
    tone: reprice.tone,
    title: `${saved} · ${reprice.title}`,
    message: reprice.message,
  }
}

export function PricingAliases() {
  const { refreshNonce, requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [data, setData] = useState<PricingAliasesResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reloadNonce, setReloadNonce] = useState(0)
  const [filter, setFilter] = useState('')
  const [scope, setScope] = useState<RowScope>('attention')
  const [drafts, setDrafts] = useState<Record<string, PricingTargetRef>>({})
  const [pendingByKey, setPendingByKey] = useState<Record<string, PendingAction>>({})
  const [feedbackByKey, setFeedbackByKey] = useState<Record<string, RowFeedback>>({})
  const [globalFeedback, setGlobalFeedback] = useState<RowFeedback | null>(null)
  const latestFetchRequestRef = useRef(0)
  const activeFetchControllerRef = useRef<AbortController | null>(null)
  const mutationControllersRef = useRef<Set<AbortController>>(new Set())
  const selectedSourceRef = useRef(selectedSourceId)
  selectedSourceRef.current = selectedSourceId

  useEffect(() => {
    const mutationControllers = mutationControllersRef.current
    return () => {
      activeFetchControllerRef.current?.abort()
      for (const controller of mutationControllers) controller.abort()
    }
  }, [])

  useEffect(() => {
    activeFetchControllerRef.current?.abort()
    for (const controller of mutationControllersRef.current) controller.abort()
    mutationControllersRef.current.clear()
    setData(null)
    setError(null)
    setFilter('')
    setScope('attention')
    setDrafts({})
    setPendingByKey({})
    setFeedbackByKey({})
    setGlobalFeedback(null)
  }, [selectedSourceId])

  useEffect(() => {
    const controller = new AbortController()
    const requestId = latestFetchRequestRef.current + 1
    latestFetchRequestRef.current = requestId
    activeFetchControllerRef.current?.abort()
    activeFetchControllerRef.current = controller

    async function load() {
      setLoading(true)
      setError(null)
      try {
        const next = await getPricingAliases(selectedSourceId, controller.signal)
        if (controller.signal.aborted || requestId !== latestFetchRequestRef.current) return
        setData(next)
        // Keep unsaved target selections: a periodic refresh must not silently
        // discard choices the user has not saved yet.
        setDrafts((current) => mergePricingAliasDrafts(current, next.aliases))
      } catch (caught) {
        if (controller.signal.aborted || requestId !== latestFetchRequestRef.current) return
        setError(caught instanceof Error ? caught.message : 'Failed to load pricing aliases')
      } finally {
        if (!controller.signal.aborted && requestId === latestFetchRequestRef.current) {
          setLoading(false)
        }
      }
    }

    void load()
    return () => controller.abort()
  }, [refreshNonce, reloadNonce, selectedSourceId])

  const currentData = data?.source_id === selectedSourceId ? data : null
  const rows = useMemo(
    () => currentData
      ? mergePricingAliasRows(currentData.observed_models, currentData.aliases)
      : [],
    [currentData],
  )
  const scopedRows = useMemo(
    () => scope === 'all' ? rows : rows.filter(pricingRowNeedsAttention),
    [rows, scope],
  )
  const visibleRows = useMemo(() => filterPricingAliasRows(scopedRows, filter), [filter, scopedRows])
  const targetGroups = useMemo(
    () => buildPricingTargetGroups(currentData?.catalogs ?? [], selectedSourceId),
    [currentData?.catalogs, selectedSourceId],
  )
  const targetCount = useMemo(() => countPricingTargets(targetGroups), [targetGroups])
  const attentionCount = useMemo(() => rows.filter(pricingRowNeedsAttention).length, [rows])
  const crossSourceCount = useMemo(
    () => currentData?.aliases.filter((alias) => alias.target_source_id !== alias.source_id).length ?? 0,
    [currentData],
  )
  const anyMutationPending = rows.some((row) => Boolean(pendingByKey[row.key]))
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name
  const writable = currentData?.writable ?? false
  const rowIsEditable = (row: PricingAliasRow) => writable && row.editable && targetCount > 0
  const draftFor = (row: PricingAliasRow): PricingTargetRef | null => drafts[row.key] ?? row.target

  const updateDraft = (rowKey: string, target: PricingTargetRef) => {
    setGlobalFeedback(null)
    setDrafts((current) => ({ ...current, [rowKey]: target }))
    setFeedbackByKey((current) => {
      if (!(rowKey in current)) return current
      const next = { ...current }
      delete next[rowKey]
      return next
    })
  }

  const runMutation = async (row: PricingAliasRow, action: PendingAction) => {
    const target = draftFor(row)
    if (action === 'save' && !target) return

    activeFetchControllerRef.current?.abort()
    latestFetchRequestRef.current += 1
    setLoading(false)
    const controller = new AbortController()
    mutationControllersRef.current.add(controller)
    setPendingByKey((current) => ({ ...current, [row.key]: action }))
    setFeedbackByKey((current) => {
      if (!(row.key in current)) return current
      const next = { ...current }
      delete next[row.key]
      return next
    })

    try {
      const response = action === 'remove'
        ? await deletePricingAlias({
            source_id: row.source_id,
            provider_id: row.provider_id,
            model_id: row.model_id,
          }, controller.signal)
        : await upsertPricingAlias({
            source_id: row.source_id,
            provider_id: row.provider_id,
            model_id: row.model_id,
            target_source_id: target!.source_id,
            target_model_id: target!.model_id,
          }, controller.signal)

      if (controller.signal.aborted) return
      activeFetchControllerRef.current?.abort()
      latestFetchRequestRef.current += 1
      if (selectedSourceRef.current === row.source_id) {
        const feedback = mutationFeedback(action, response.reprice, response.refresh_error)
        setData(response)
        // Only this row's draft is settled by its own mutation; other rows keep
        // whatever the user selected but has not saved.
        setDrafts((current) => mergePricingAliasDrafts(current, response.aliases, [row.key]))
        setGlobalFeedback(action === 'remove' && !row.detected ? feedback : null)
        setFeedbackByKey((current) => ({
          ...current,
          [row.key]: feedback,
        }))
      }
      requestRefresh()
    } catch (caught) {
      if (controller.signal.aborted || selectedSourceRef.current !== row.source_id) return
      const feedback: RowFeedback = {
        tone: 'danger',
        title: action === 'remove' ? 'Alias removal failed' : 'Alias update failed',
        message: caught instanceof Error ? caught.message : 'The pricing alias request failed.',
      }
      setFeedbackByKey((current) => ({
        ...current,
        [row.key]: feedback,
      }))
    } finally {
      mutationControllersRef.current.delete(controller)
      if (!controller.signal.aborted) {
        setPendingByKey((current) => {
          const next = { ...current }
          delete next[row.key]
          return next
        })
      }
    }
  }

  const columns: Column<PricingAliasRow>[] = [
    {
      key: 'model_id',
      header: 'Observed model',
      width: 250,
      render: (row) => (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
          <span style={{ font: '500 12.5px/1.4 var(--font-mono)', color: 'var(--fg-primary)' }}>
            {row.model_id}
          </span>
          <span style={{ color: row.provider_id === '' ? 'var(--fg-faint)' : 'var(--fg-muted)', font: '400 11.5px/1.3 var(--font-ui)' }}>
            {formatPricingProvider(row.provider_id)}
          </span>
        </span>
      ),
    },
    {
      key: 'activity',
      header: 'Activity',
      width: 130,
      render: (row) => row.detected ? (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 3, font: '500 12px/1.2 var(--font-mono)', fontVariantNumeric: 'tabular-nums' }}>
          <span style={{ color: 'var(--fg-secondary)' }}>{formatInteger(row.sessions)} sessions</span>
          <span style={{ color: 'var(--fg-muted)' }}>{formatInteger(row.messages)} messages</span>
        </span>
      ) : (
        <span style={{ color: 'var(--fg-faint)', font: '400 12px/1.3 var(--font-ui)' }}>No current detection</span>
      ),
    },
    {
      key: 'status',
      header: 'Pricing status',
      width: 230,
      render: (row) => {
        const badge = row.alias ? aliasStateBadge(row) : { tone: resolutionTone(row), label: resolutionLabel(row) }
        const reason = row.alias
          ? row.alias.state_reason
          : [row.resolution_reason, row.resolution_note].filter(Boolean).join(' · ')
        return (
          <span style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 5, minWidth: 0 }}>
            <Badge tone={badge.tone} dot={row.alias?.active}>{badge.label}</Badge>
            {reason && (
              // The full sentence lives in the tooltip so rows stay one line tall.
              <span
                title={reason}
                style={{
                  display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden',
                  color: 'var(--fg-muted)', font: '400 11.5px/1.35 var(--font-ui)', whiteSpace: 'normal',
                }}
              >
                {reason}
              </span>
            )}
          </span>
        )
      },
    },
    {
      key: 'target',
      header: 'Pricing target',
      width: 300,
      render: (row) => {
        const target = draftFor(row)
        const canEdit = rowIsEditable(row)
        if (!canEdit) {
          return (
            <span style={{ display: 'flex', flexDirection: 'column', gap: 4, maxWidth: 280, color: 'var(--fg-muted)', font: '400 12px/1.4 var(--font-ui)', whiteSpace: 'normal' }}>
              {readOnlyTargetReason(row, targetCount, writable)}
            </span>
          )
        }
        const crossSource = Boolean(target && target.source_id !== row.source_id)
        return (
          <span style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 6 }}>
            <PricingTargetPicker
              groups={targetGroups}
              value={target}
              selectedSourceId={selectedSourceId as SourceID}
              onChange={(next) => updateDraft(row.key, next)}
            />
            {crossSource && (
              <Badge tone="accent">Cross-source · {vendorMeta(target!.source_id).name}</Badge>
            )}
          </span>
        )
      },
    },
    {
      key: 'actions',
      header: 'Actions',
      width: 300,
      wrap: true,
      render: (row) => {
        const pending = pendingByKey[row.key]
        const target = draftFor(row)
        const editable = rowIsEditable(row)
        const changed = !samePricingTarget(target, row.target)
        const disableForOtherRow = anyMutationPending && !pending
        const feedback = feedbackByKey[row.key]

        return (
          <span style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 8, padding: '7px 0', minWidth: 250 }}>
            <span style={{ display: 'flex', alignItems: 'center', gap: 7, flexWrap: 'wrap' }}>
              {editable && (
                <Button
                  size="sm"
                  variant="primary"
                  disabled={!changed || !target || Boolean(pending) || disableForOtherRow}
                  onClick={() => void runMutation(row, 'save')}
                >
                  {pending === 'save' ? (row.alias ? 'Updating…' : 'Saving…') : (row.alias ? 'Update' : 'Save')}
                </Button>
              )}
              {row.alias && (
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={!writable || Boolean(pending) || disableForOtherRow}
                  onClick={() => void runMutation(row, 'remove')}
                >
                  {pending === 'remove' ? 'Removing…' : 'Remove'}
                </Button>
              )}
              {!editable && !row.alias && <Badge>Read-only</Badge>}
            </span>
            {feedback && (
              <span style={{ display: 'block', width: 280, maxWidth: '100%', whiteSpace: 'normal' }}>
                <Notice tone={feedback.tone} title={feedback.title}>{feedback.message}</Notice>
              </span>
            )}
          </span>
        )
      },
    },
  ]

  const headerAction = currentData ? (
    <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8, flexWrap: 'wrap' }}>
      {loading && <Badge tone="accent" dot>Refreshing</Badge>}
      {currentData.catalog.snapshot_id && (
        <span title={currentData.catalog.snapshot_id}>
          <Badge tone="accent">Snapshot {shortSnapshotID(currentData.catalog.snapshot_id)}</Badge>
        </span>
      )}
      {currentData.supported && (
        <>
          <Badge tone={attentionCount > 0 ? 'warning' : 'neutral'}>
            {formatInteger(attentionCount)} {attentionCount === 1 ? 'needs' : 'need'} pricing
          </Badge>
          <Badge>{formatInteger(currentData.aliases.length)} {currentData.aliases.length === 1 ? 'alias' : 'aliases'}</Badge>
          <Badge>{formatInteger(targetCount)} targets · {formatInteger(targetGroups.length)} catalogs</Badge>
        </>
      )}
      {currentData.supported && rows.length > 0 && (
        <SearchInput
          value={filter}
          onChange={setFilter}
          placeholder="Filter model, provider, or target…"
          label="Filter pricing aliases by model, provider, or target"
          width={260}
        />
      )}
    </span>
  ) : loading ? <Skeleton width={260} height={20} /> : null

  return (
    <Card
      title="Pricing aliases"
      subtitle={(
        <span style={{ display: 'block', maxWidth: 760, whiteSpace: 'normal' }}>
          Map an observed model identifier to a bundled pricing model from any source. Aliases affect pricing only; raw model and provider grouping remains unchanged.
        </span>
      )}
      action={headerAction}
      pad={16}
    >
      {!currentData && loading ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Skeleton width="100%" height={52} radius="var(--radius-lg)" />
          <Skeleton width="100%" height={190} radius="var(--radius-xl)" />
        </div>
      ) : !currentData && error ? (
        <ErrorState
          title="Pricing aliases failed to load"
          message={error}
          onRetry={() => setReloadNonce((value) => value + 1)}
        />
      ) : !currentData ? (
        <EmptyState icon="git-branch" title="No pricing alias data" description={`No pricing catalog response is available for ${sourceLabel}.`} />
      ) : !currentData.supported ? (
        <EmptyState
          icon="dollar"
          title="Local pricing aliases are not used for this source"
          description={`${sourceLabel} reports cost without a local target catalog, so this section is read-only. Pricing aliases are available only for sources whose costs are computed from bundled per-model rates.`}
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {globalFeedback && (
            <Notice tone={globalFeedback.tone} title={globalFeedback.title}>{globalFeedback.message}</Notice>
          )}
          {error && (
            <Notice tone="warning" title="Pricing aliases could not be refreshed">
              {error} The last successfully loaded alias list remains visible.
            </Notice>
          )}
          {!writable && (
            <Notice tone="warning" title="Pricing aliases are read-only">
              The dashboard settings database is unavailable, so saved aliases can be inspected but not changed.
            </Notice>
          )}
          {currentData.observation_error && (
            <Notice tone="warning" title="Detected models could not be read">
              {currentData.observation_error} Saved aliases are still listed, but newly detected models may be missing.
            </Notice>
          )}
          {crossSourceCount > 0 && (
            <Notice tone="info" title="Some models are priced from another source">
              {crossSourceCount === 1
                ? 'One alias borrows'
                : `${formatInteger(crossSourceCount)} aliases borrow`} another catalog’s rates.
              Only input, cached input, cache write and output carry across catalogs, so processing-tier and long-context
              rates do not apply and those costs are reported as approximate.
            </Notice>
          )}
          {currentData.catalog.note && (
            <Notice tone="info" title="Catalog note">{currentData.catalog.note}</Notice>
          )}
          {targetCount === 0 && (
            <Notice tone="warning" title="No priced alias targets">
              No source has a catalog model with positive input and output rates, so aliases cannot be created or updated.
            </Notice>
          )}

          {rows.length === 0 ? (
            <EmptyState
              icon="check"
              title="No observed models or aliases"
              description="This source has not reported any models yet, and there are no saved manual aliases for it."
            />
          ) : (
            <>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
                <SegmentedControl
                  value={scope}
                  options={SCOPE_OPTIONS}
                  onChange={(value) => setScope(value as RowScope)}
                />
                <span style={{ color: 'var(--fg-faint)', font: '400 12px/1.4 var(--font-ui)' }}>
                  {scope === 'attention'
                    ? 'Models without usable pricing, plus aliases that are not applying.'
                    : 'Every observed model, including ones that already price correctly.'}
                </span>
              </div>
              {visibleRows.length === 0 ? (
                <EmptyState
                  icon={filter.trim() ? 'search' : 'check'}
                  title={filter.trim() ? 'No pricing rows match this filter' : 'Every observed model has usable pricing'}
                  description={filter.trim()
                    ? `No observed model, provider, or target contains “${filter.trim()}”.`
                    : 'Nothing needs a pricing alias right now. Switch to All models to re-point one anyway.'}
                  action={filter.trim()
                    ? <Button size="sm" variant="secondary" onClick={() => setFilter('')}>Clear filter</Button>
                    : <Button size="sm" variant="secondary" onClick={() => setScope('all')}>Show all models</Button>}
                />
              ) : (
                <DataTable
                  columns={columns}
                  rows={visibleRows}
                  rowKey={(row) => row.key}
                />
              )}
            </>
          )}
        </div>
      )}
    </Card>
  )
}
