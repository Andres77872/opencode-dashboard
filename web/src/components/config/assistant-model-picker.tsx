/*
  Picker for the assistant's active provider and model.

  A provider can list hundreds of models, so this is a searchable panel grouped
  by provider instead of a native select beside a separate search box: the
  filter can never hide the current choice, and each row shows the context
  window that decides whether a model can be used at all.
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { Badge, Icon, Popover } from '../vael'
import { formatContextLimit, providerModelValue } from './assistant-provider-form'
import type { AssistantProvider, AssistantProviderModel } from '../../types/assistant'

const PANEL_WIDTH = 480

interface PickerOption {
  key: string
  provider: AssistantProvider
  model: AssistantProviderModel
}

export interface AssistantModelPickerProps {
  providers: AssistantProvider[]
  /** The active pair, if any. It may name a provider or model that no longer exists. */
  selection: { provider_id: string; model_id: string } | null
  onSelect: (providerId: string, modelId: string) => void
  /** Called for the "No model" row, which turns the assistant off. Omit to hide the row. */
  onClear?: () => void
  disabled?: boolean
  busy?: boolean
  /** Accessible name of the trigger. */
  label: string
}

export function AssistantModelPicker({ providers, selection, onSelect, onClear, disabled, busy, label }: AssistantModelPickerProps) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const searchRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const groups = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return providers
      .map((provider) => ({
        provider,
        options: provider.models
          .filter((model) => !needle || `${provider.name} ${model.id}`.toLowerCase().includes(needle))
          // Usable models first; the rest stay listed, disabled, so it is
          // clear why a model the endpoint reports cannot be chosen.
          .sort((left, right) => Number(right.selectable) - Number(left.selectable))
          .map((model): PickerOption => ({ key: providerModelValue(provider.id, model.id), provider, model })),
      }))
      .filter((group) => group.options.length > 0)
  }, [providers, query])

  const selectable = useMemo(() => groups.flatMap((group) => group.options.filter((option) => option.model.selectable)), [groups])
  const selectedKey = selection?.provider_id ? providerModelValue(selection.provider_id, selection.model_id) : null
  const selectedProvider = providers.find((provider) => provider.id === selection?.provider_id) ?? null
  const selectedModel = selectedProvider?.models.find((model) => model.id === selection?.model_id) ?? null
  const totalSelectable = useMemo(
    () => providers.reduce((sum, provider) => sum + provider.models.filter((model) => model.selectable).length, 0),
    [providers],
  )

  // Clamped during render: a narrowed list can be shorter than the cursor.
  const cursor = activeIndex < selectable.length ? activeIndex : 0

  useEffect(() => {
    // Optional call: not every DOM implements it (jsdom does not).
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView?.({ block: 'nearest' })
  }, [cursor, groups])

  const commit = (option: PickerOption, close: () => void) => {
    if (option.key !== selectedKey) onSelect(option.provider.id, option.model.id)
    close()
  }

  const inert = disabled || busy || totalSelectable === 0
  const triggerContent = !selection?.provider_id ? (
    <span style={{ color: 'var(--fg-muted)' }}>{totalSelectable === 0 ? 'No model available yet' : 'Choose a model'}</span>
  ) : (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
      <span style={{ font: '600 12px/1.3 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'nowrap', flexShrink: 0 }}>
        {selectedProvider?.name ?? selection.provider_id}
      </span>
      <span style={{ font: '500 12.5px/1.3 var(--font-mono)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {selection.model_id}
      </span>
      {!selectedModel && <Badge tone="warning">Unavailable</Badge>}
    </span>
  )

  return (
    <Popover
      portal
      block
      width={PANEL_WIDTH}
      closeOnClick={false}
      onOpenChange={(open) => {
        if (!open) return
        setQuery('')
        setActiveIndex(Math.max(0, selectable.findIndex((option) => option.key === selectedKey)))
        requestAnimationFrame(() => searchRef.current?.focus())
      }}
      trigger={(open, toggle) => (
        <button
          type="button"
          onClick={inert ? undefined : toggle}
          disabled={disabled || totalSelectable === 0}
          aria-label={label}
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-busy={busy || undefined}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 10,
            width: '100%',
            minHeight: 36,
            padding: '6px 11px',
            background: open ? 'var(--ink-700)' : 'var(--ink-850)',
            border: `1px solid ${open ? 'var(--border-accent)' : 'var(--border-default)'}`,
            borderRadius: 'var(--radius-md)',
            cursor: disabled || totalSelectable === 0 ? 'not-allowed' : busy ? 'progress' : 'pointer',
            opacity: disabled || totalSelectable === 0 ? 0.6 : 1,
            font: '500 13px/1.3 var(--font-ui)',
            color: 'var(--fg-primary)',
            textAlign: 'left',
          }}
        >
          {triggerContent}
          <Icon
            name={busy ? 'refresh' : 'chevron-down'}
            size={14}
            color="var(--fg-muted)"
            style={{ flexShrink: 0, ...(busy ? { animation: 'vael-spin 0.9s linear infinite' } : { transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }) }}
          />
        </button>
      )}
    >
      {(close) => (
        <div
          style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0 }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault()
              if (selectable.length === 0) return
              const step = event.key === 'ArrowDown' ? 1 : -1
              setActiveIndex((cursor + step + selectable.length) % selectable.length)
              return
            }
            if (event.key === 'Home' || event.key === 'End') {
              event.preventDefault()
              setActiveIndex(event.key === 'Home' ? 0 : Math.max(0, selectable.length - 1))
              return
            }
            if (event.key === 'Enter') {
              event.preventDefault()
              const option = selectable[cursor]
              if (option) commit(option, close)
            }
          }}
        >
          <input
            ref={searchRef}
            type="text"
            className="vael-input"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value)
              setActiveIndex(0)
            }}
            placeholder="Search providers and models…"
            aria-label="Search providers and models"
          />
          <div ref={listRef} role="listbox" aria-label={label} style={{ display: 'flex', flexDirection: 'column', gap: 2, maxHeight: 320, overflowY: 'auto' }}>
            {onClear && selectedKey && query.trim() === '' && (
              <button
                type="button"
                onClick={() => {
                  onClear()
                  close()
                }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 10,
                  padding: '7px 9px',
                  border: 'none',
                  borderRadius: 'var(--radius-sm)',
                  background: 'transparent',
                  color: 'var(--fg-secondary)',
                  font: '500 12.5px/1.3 var(--font-ui)',
                  textAlign: 'left',
                  cursor: 'pointer',
                }}
              >
                No model
                <span style={{ color: 'var(--fg-muted)', font: '400 11.5px/1.3 var(--font-ui)' }}>Turns the assistant off</span>
              </button>
            )}
            {groups.length === 0 ? (
              <span style={{ padding: '14px 9px', color: 'var(--fg-muted)', font: '400 12.5px/1.4 var(--font-ui)' }}>
                No model matches “{query.trim()}”.
              </span>
            ) : (
              groups.map((group) => (
                <div key={group.provider.id} role="group" aria-label={group.provider.name} style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
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
                    {group.provider.name}
                    <span style={{ color: 'var(--fg-faint)', textTransform: 'none', letterSpacing: 0, fontWeight: 500 }}>{group.provider.destination_label}</span>
                  </span>
                  {group.options.map((option) => {
                    const index = selectable.indexOf(option)
                    const isActive = index >= 0 && index === cursor
                    const isSelected = option.key === selectedKey
                    const usable = option.model.selectable
                    return (
                      <div
                        key={option.key}
                        role="option"
                        aria-selected={isSelected}
                        aria-disabled={!usable || undefined}
                        data-active={isActive}
                        title={usable ? undefined : 'Set this model’s context window in the provider below before it can be used.'}
                        onMouseEnter={usable ? () => setActiveIndex(index) : undefined}
                        onClick={usable ? () => commit(option, close) : undefined}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: 10,
                          padding: '7px 9px',
                          borderRadius: 'var(--radius-sm)',
                          background: isSelected ? 'var(--accent-soft)' : isActive ? 'var(--ink-650)' : 'transparent',
                          cursor: usable ? 'pointer' : 'not-allowed',
                          opacity: usable ? 1 : 0.55,
                        }}
                      >
                        <span style={{ font: '500 12.5px/1.3 var(--font-mono)', color: isSelected ? 'var(--blue-300)' : 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0 }}>
                          {option.model.id}
                        </span>
                        <span style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0, font: '500 11.5px/1.3 var(--font-mono)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
                          {option.model.context_limit ? `${formatContextLimit(option.model.context_limit)} context` : 'context required'}
                          {usable && !option.model.verified && <Badge>Unverified</Badge>}
                          {isSelected && <Icon name="check" size={14} color="var(--accent)" />}
                        </span>
                      </div>
                    )
                  })}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </Popover>
  )
}
