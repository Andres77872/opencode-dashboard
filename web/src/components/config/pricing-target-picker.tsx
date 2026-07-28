/*
  Target picker for a pricing alias.

  Targets span every source's bundled catalog, so a flat dropdown of ~45 long
  option labels is unusable — and rendering one inside the alias DataTable clips
  it against that table's horizontal scroller. This picker therefore opens a
  portalled panel with a search field, groups models by source, and lays the
  per-million rates out as aligned columns so models can be compared by scanning
  down a column rather than reading across a sentence.
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { Icon, Popover, vendorMeta } from '../vael'
import {
  filterPricingTargetGroups,
  findPricingTarget,
  flattenPricingTargetGroups,
  formatPricingTargetRates,
  pricingTargetOptionKey,
} from '../../lib/pricing-target-catalog'
import type { PricingTargetGroup, PricingTargetOption } from '../../lib/pricing-target-catalog'
import type { PricingTargetRef } from '../../lib/pricing-aliases'
import type { SourceID } from '../../types/api'

const PANEL_WIDTH = 460

export interface PricingTargetPickerProps {
  groups: PricingTargetGroup[]
  value: PricingTargetRef | null
  selectedSourceId: SourceID
  onChange: (target: PricingTargetRef) => void
  disabled?: boolean
}

function SourceChip({ sourceId, label }: { sourceId: SourceID; label: string }) {
  const vendor = vendorMeta(sourceId)
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding: '1px 6px 1px 3px',
        borderRadius: 'var(--radius-sm)',
        background: 'var(--ink-750)',
        color: 'var(--fg-muted)',
        font: '600 10.5px/1.6 var(--font-ui)',
        letterSpacing: '0.04em',
        textTransform: 'uppercase',
        whiteSpace: 'nowrap',
      }}
    >
      <span style={{ width: 6, height: 6, borderRadius: 2, background: vendor.color }} />
      {label}
    </span>
  )
}

function RateColumns({ option }: { option: PricingTargetOption }) {
  const rates = formatPricingTargetRates(option.rate, option.currency)
  const cells: Array<[string, string]> = [
    ['in', rates.input],
    ['cached', rates.cached],
    ['out', rates.output],
  ]
  return (
    <span
      title={`Cache write ${rates.cacheWrite} per 1M`}
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(3, 62px)',
        gap: 6,
        font: '500 11.5px/1.3 var(--font-mono)',
        fontVariantNumeric: 'tabular-nums',
        textAlign: 'right',
      }}
    >
      {cells.map(([label, value]) => (
        <span key={label} style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end' }}>
          <span style={{ color: 'var(--fg-secondary)' }}>{value}</span>
          <span style={{ color: 'var(--fg-faint)', font: '500 9.5px/1.4 var(--font-ui)', letterSpacing: '0.05em' }}>{label}</span>
        </span>
      ))}
    </span>
  )
}

export function PricingTargetPicker({ groups, value, selectedSourceId, onChange, disabled }: PricingTargetPickerProps) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const searchRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const visibleGroups = useMemo(() => filterPricingTargetGroups(groups, query), [groups, query])
  const flat = useMemo(() => flattenPricingTargetGroups(visibleGroups), [visibleGroups])
  const selected = useMemo(() => findPricingTarget(groups, value), [groups, value])

  // Clamped during render rather than corrected by an effect: a narrowed list
  // can be shorter than the previous cursor, and fixing that in an effect would
  // paint one frame with the cursor out of range.
  const cursor = activeIndex < flat.length ? activeIndex : 0

  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [cursor, visibleGroups])

  const commit = (option: PricingTargetOption, close: () => void) => {
    onChange({ source_id: option.source_id, model_id: option.model_id })
    close()
  }

  const triggerLabel = selected
    ? (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 7, minWidth: 0 }}>
          {selected.source_id !== selectedSourceId && (
            <SourceChip sourceId={selected.source_id} label={vendorMeta(selected.source_id).short} />
          )}
          <span style={{ font: '500 12px/1.3 var(--font-mono)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {selected.model_id}
          </span>
        </span>
      )
    : <span style={{ color: 'var(--fg-muted)' }}>Choose a pricing target</span>

  if (disabled) {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 7, color: 'var(--fg-faint)', font: '400 12px/1.4 var(--font-ui)' }}>
        {triggerLabel}
      </span>
    )
  }

  return (
    <Popover
      portal
      width={PANEL_WIDTH}
      closeOnClick={false}
      onOpenChange={(open) => {
        if (!open) return
        setQuery('')
        setActiveIndex(Math.max(0, flat.findIndex((option) => selected && pricingTargetOptionKey(option) === pricingTargetOptionKey(selected))))
        requestAnimationFrame(() => searchRef.current?.focus())
      }}
      trigger={(open, toggle) => (
        <button
          type="button"
          onClick={toggle}
          aria-haspopup="listbox"
          aria-expanded={open}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 8,
            width: 260,
            minHeight: 32,
            padding: '5px 9px',
            background: open ? 'var(--ink-700)' : 'var(--ink-750)',
            border: `1px solid ${open ? 'var(--border-accent)' : 'var(--border-default)'}`,
            borderRadius: 'var(--radius-md)',
            cursor: 'pointer',
            font: '500 13px/1.3 var(--font-ui)',
            color: 'var(--fg-primary)',
            textAlign: 'left',
          }}
        >
          {triggerLabel}
          <Icon name="chevron-down" size={14} color="var(--fg-muted)" style={{ flexShrink: 0, transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
        </button>
      )}
    >
      {(close) => (
          <div
            style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0 }}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                event.preventDefault()
                if (flat.length === 0) return
                const step = event.key === 'ArrowDown' ? 1 : -1
                setActiveIndex((cursor + step + flat.length) % flat.length)
                return
              }
              if (event.key === 'Home' || event.key === 'End') {
                event.preventDefault()
                setActiveIndex(event.key === 'Home' ? 0 : Math.max(0, flat.length - 1))
                return
              }
              if (event.key === 'Enter') {
                event.preventDefault()
                const option = flat[cursor]
                if (option) commit(option, close)
              }
            }}
          >
            <input
              ref={searchRef}
              type="text"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setActiveIndex(0)
              }}
              placeholder="Search models across all sources…"
              aria-label="Search pricing targets across all sources"
              style={{
                width: '100%',
                height: 32,
                padding: '0 10px',
                background: 'var(--ink-800)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-md)',
                color: 'var(--fg-primary)',
                font: '400 13px/1 var(--font-ui)',
                outline: 'none',
              }}
            />
            <div ref={listRef} role="listbox" aria-label="Pricing targets" style={{ display: 'flex', flexDirection: 'column', gap: 2, maxHeight: 320, overflowY: 'auto' }}>
              {visibleGroups.length === 0 ? (
                <span style={{ padding: '14px 9px', color: 'var(--fg-muted)', font: '400 12.5px/1.4 var(--font-ui)' }}>
                  No pricing model matches “{query.trim()}”.
                </span>
              ) : (
                visibleGroups.map((group) => (
                  <div key={group.source_id} style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <span
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 7,
                        padding: '8px 9px 4px',
                        color: 'var(--fg-muted)',
                        font: '600 10.5px/1 var(--font-ui)',
                        letterSpacing: '0.07em',
                        textTransform: 'uppercase',
                      }}
                    >
                      <span style={{ width: 6, height: 6, borderRadius: 2, background: vendorMeta(group.source_id).color }} />
                      {group.source_label}
                      {group.is_current_source && <span style={{ color: 'var(--fg-faint)', textTransform: 'none', letterSpacing: 0 }}>this source</span>}
                    </span>
                    {group.options.map((option) => {
                      const key = pricingTargetOptionKey(option)
                      const index = flat.findIndex((candidate) => pricingTargetOptionKey(candidate) === key)
                      const isActive = index === cursor
                      const isSelected = Boolean(selected && pricingTargetOptionKey(selected) === key)
                      return (
                        <div
                          key={key}
                          id={`pricing-target-${key.replace(/\s+/g, '-')}`}
                          role="option"
                          aria-selected={isSelected}
                          data-active={isActive}
                          onMouseEnter={() => setActiveIndex(index)}
                          onClick={() => commit(option, close)}
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            gap: 10,
                            padding: '7px 9px',
                            borderRadius: 'var(--radius-sm)',
                            background: isSelected ? 'var(--accent-soft)' : isActive ? 'var(--ink-650)' : 'transparent',
                            cursor: 'pointer',
                          }}
                        >
                          <span style={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}>
                            <span style={{ font: '500 12.5px/1.3 var(--font-mono)', color: isSelected ? 'var(--blue-300)' : 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                              {option.model_id}
                            </span>
                            {option.display_name !== option.model_id && (
                              <span style={{ color: 'var(--fg-faint)', font: '400 11px/1.3 var(--font-ui)' }}>{option.display_name}</span>
                            )}
                          </span>
                          <span style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                            <RateColumns option={option} />
                            {isSelected && <Icon name="check" size={14} color="var(--accent)" />}
                          </span>
                        </div>
                      )
                    })}
                  </div>
                ))
              )}
            </div>
            <span style={{ display: 'block', padding: '2px 9px 4px', color: 'var(--fg-faint)', font: '400 11px/1.45 var(--font-ui)' }}>
              Rates are per 1M tokens. A target from another source borrows that catalog’s input, cached, cache-write and output rates.
            </span>
          </div>
      )}
    </Popover>
  )
}
