/* Tree view: recursive, format-agnostic rendering of the structured config.
   Expand state is seeded per-node from openDepth and reset wholesale by
   remounting via treeEpoch (expand/collapse-all are expected to reset
   individual toggles). Search-active force-expands. */
import { createContext, useContext, useState } from 'react'
import type { FocusEvent } from 'react'
import { Badge, Button, Card, Icon, Notice } from '../vael'
import { CopyButton, CopyChip, InlineCopyButton } from './copy-button'
import { useMediaQuery } from '../../lib/use-media-query'
import {
  ALL_SECTIONS,
  collectInsights,
  formatPrimitiveValue,
  isChipArray,
  isObject,
  isPrimitive,
  isRedactedValue,
  serializeConfigValue,
  summarizeValue,
} from '../../lib/config-utils'
import { formatInteger } from '../../lib/format'
import type {
  ConfigJsonObject,
  ConfigJsonPrimitive,
  ConfigJsonValue,
  ConfigSectionProjection,
} from '../../types/config'

export interface ConfigTreePaneProps {
  /** Projections to render: one entry for a selected section, all for ALL_SECTIONS. */
  projections: ConfigSectionProjection[]
  selectedKey: string
  searchActive: boolean
  /** Nodes with depth < openDepth start expanded. */
  openDepth: number
  /** Bumping remounts the subtree, re-seeding expand state from openDepth. */
  treeEpoch: number
  onExpandAll: () => void
  onCollapseAll: () => void
  onClearFilter: () => void
}

/* Row copy buttons stay out of the way until a row is hovered or holds focus.
   A touch screen has no hover to reveal them, so there they are always shown. */
const AlwaysShowCopyContext = createContext(false)

function useRowReveal() {
  const alwaysShow = useContext(AlwaysShowCopyContext)
  const [hover, setHover] = useState(false)
  const [focusWithin, setFocusWithin] = useState(false)
  return {
    hover,
    revealed: alwaysShow || hover || focusWithin,
    rowProps: {
      onMouseEnter: () => setHover(true),
      onMouseLeave: () => setHover(false),
      onFocus: () => setFocusWithin(true),
      onBlur: (event: FocusEvent<HTMLElement>) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocusWithin(false)
      },
    },
  }
}

export function ConfigTreePane({
  projections,
  selectedKey,
  searchActive,
  openDepth,
  treeEpoch,
  onExpandAll,
  onCollapseAll,
  onClearFilter,
}: ConfigTreePaneProps) {
  const touch = useMediaQuery('(hover: none)')
  const isAll = selectedKey === ALL_SECTIONS
  const title = isAll ? 'All sections' : <span style={{ fontFamily: 'var(--font-mono)' }}>{selectedKey}</span>

  // Serialized on demand: building the JSON for every render of a large config
  // was work nobody asked for until the button is pressed.
  const copyValue = () => isAll
    ? serializeConfigValue(
        Object.fromEntries(projections.map((p) => [p.section.key, p.section.value])),
      )
    : serializeConfigValue(projections[0]?.section.value ?? null)

  const totalRedacted = projections.reduce((sum, p) => sum + p.insights.redactedValues, 0)
  const visible = projections.filter((p) => !searchActive || p.filteredValue !== null)
  const matchCount = visible.reduce((sum, p) => sum + (p.filteredInsights?.leafValues ?? 0), 0)

  const subtitle = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
      <span>{isAll ? 'Every top-level key in the file' : 'One top-level key'}</span>
      {totalRedacted > 0 && <Badge tone="warning">{formatInteger(totalRedacted)} redacted</Badge>}
      {searchActive && (
        <span style={{ color: matchCount > 0 ? 'var(--accent)' : 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
          {formatInteger(matchCount)} match{matchCount === 1 ? '' : 'es'}
        </span>
      )}
    </span>
  )

  const action = (
    <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
      <Button variant="ghost" size="sm" onClick={onExpandAll}>Expand all</Button>
      <Button variant="ghost" size="sm" onClick={onCollapseAll}>Collapse all</Button>
      <CopyButton label={isAll ? 'Copy all' : 'Copy section'} value={copyValue} />
    </div>
  )

  return (
    <Card title={title} subtitle={subtitle} action={action}>
      {visible.length === 0 ? (
        <>
          <Notice tone="warning" title="No matches">
            No keys or values{isAll ? '' : ` in ${selectedKey}`} match the active filter.
          </Notice>
          <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
            <Button variant="secondary" size="sm" onClick={onClearFilter}>Clear filter</Button>
          </div>
        </>
      ) : (
        <AlwaysShowCopyContext.Provider value={touch}>
          <div key={treeEpoch}>
            {visible.map((projection) => (
              <ConfigNode
                key={projection.section.key}
                label={projection.section.key}
                value={searchActive ? projection.filteredValue ?? projection.section.value : projection.section.value}
                depth={0}
                path={projection.section.key}
                searchActive={searchActive}
                openDepth={openDepth}
              />
            ))}
          </div>
        </AlwaysShowCopyContext.Provider>
      )}
    </Card>
  )
}

/* ---------- recursive nodes ---------- */

interface ConfigNodeProps {
  label: string
  value: ConfigJsonValue
  depth: number
  path: string
  searchActive: boolean
  openDepth: number
}

function ConfigNode({ label, value, depth, path, searchActive, openDepth }: ConfigNodeProps) {
  if (Array.isArray(value) || isObject(value)) {
    return (
      <CollectionValue
        label={label}
        value={value}
        depth={depth}
        path={path}
        searchActive={searchActive}
        openDepth={openDepth}
      />
    )
  }
  return <PrimitiveField label={label} value={value} />
}

interface CollectionValueProps {
  label: string
  value: ConfigJsonObject | ConfigJsonValue[]
  depth: number
  path: string
  searchActive: boolean
  openDepth: number
}

function CollectionValue({ label, value, depth, path, searchActive, openDepth }: CollectionValueProps) {
  const [open, setOpen] = useState(() => depth < openDepth)
  const { hover, revealed, rowProps } = useRowReveal()
  const expanded = searchActive ? true : open
  // Keys are shown exactly as written: the filter matches the real key, and a
  // prettified "Small Model" would never be found by typing what is on screen.
  const displayLabel = label
  const isArrayValue = Array.isArray(value)
  const chips = isArrayValue && isChipArray(value)
  const redactedCount = collectInsights(value).redactedValues

  const primitiveEntries = isArrayValue
    ? []
    : Object.entries(value).filter((entry): entry is [string, ConfigJsonPrimitive] => isPrimitive(entry[1]))

  const nestedEntries: Array<readonly [string, ConfigJsonValue]> = isArrayValue
    ? chips
      ? []
      : value.map((item, index) => [`[${index}]`, item] as const)
    : Object.entries(value).filter((entry) => !isPrimitive(entry[1]))

  return (
    <div>
      {/* The toggle and the copy button are siblings: a button inside a button
          is invalid markup and unreachable from the keyboard. */}
      <div
        {...rowProps}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          background: hover ? 'var(--ink-750)' : 'transparent',
          borderRadius: 'var(--radius-sm)',
        }}
      >
        <button
          type="button"
          onClick={() => setOpen((c) => !c)}
          aria-expanded={expanded}
          // Search force-expands every match, so the toggle has nothing to do.
          disabled={searchActive}
          title={searchActive ? 'Expanded while a filter is active' : undefined}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            flex: 1,
            minWidth: 0,
            minHeight: 30,
            padding: '4px 4px',
            textAlign: 'left',
            border: 'none',
            background: 'transparent',
            cursor: searchActive ? 'default' : 'pointer',
            borderRadius: 'var(--radius-sm)',
          }}
        >
          <Icon
            name="chevron-right"
            size={14}
            color="var(--fg-muted)"
            style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform var(--dur-fast)', flexShrink: 0 }}
          />
          <span style={{ font: '600 13px/1.3 var(--font-mono)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{displayLabel}</span>
          <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', flexShrink: 0 }}>{summarizeValue(value)}</span>
          {redactedCount > 0 && (
            <span
              role="img"
              aria-label={`${redactedCount} redacted ${redactedCount === 1 ? 'value' : 'values'}`}
              title={`${redactedCount} redacted ${redactedCount === 1 ? 'value' : 'values'}`}
              style={{ width: 6, height: 6, borderRadius: 3, background: 'var(--warning)', flexShrink: 0 }}
            />
          )}
        </button>
        <InlineCopyButton
          label={`Copy ${displayLabel} as JSON`}
          value={() => serializeConfigValue(value)}
          visible={revealed}
        />
      </div>

      {expanded && (
        <div style={{ marginLeft: 8, paddingLeft: 12, borderLeft: '1px solid var(--border-subtle)' }}>
          {isArrayValue && value.length === 0 && <EmptyLeaf label="Empty collection" />}
          {!isArrayValue && primitiveEntries.length === 0 && nestedEntries.length === 0 && <EmptyLeaf label="Empty object" />}

          {!isArrayValue && primitiveEntries.length > 0 && (
            <div>
              {primitiveEntries.map(([key, item]) => (
                <PrimitiveField key={key} label={key} value={item} />
              ))}
            </div>
          )}

          {chips && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, padding: '6px 4px' }}>
              {value.map((item, index) => (
                <CopyChip
                  key={`${path}-${index}`}
                  text={formatPrimitiveValue(item as ConfigJsonPrimitive)}
                  redacted={isRedactedValue(item)}
                />
              ))}
            </div>
          )}

          {nestedEntries.length > 0 && (
            <div>
              {nestedEntries.map(([key, item]) => (
                <ConfigNode
                  key={`${path}.${key}`}
                  label={key}
                  value={item}
                  depth={depth + 1}
                  path={key.startsWith('[') ? `${path}${key}` : `${path}.${key}`}
                  searchActive={searchActive}
                  openDepth={openDepth}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function EmptyLeaf({ label }: { label: string }) {
  return (
    <div style={{ padding: '6px 4px', font: '400 13px/1 var(--font-ui)', fontStyle: 'italic', color: 'var(--fg-muted)' }}>
      {label}
    </div>
  )
}

interface PrimitiveFieldProps {
  label: string
  value: ConfigJsonPrimitive
}

function PrimitiveField({ label, value }: PrimitiveFieldProps) {
  const { hover, revealed, rowProps } = useRowReveal()
  const redacted = isRedactedValue(value)
  const displayLabel = label
  const formattedValue = formatPrimitiveValue(value)
  const valueColor = primitiveValueColor(value, redacted)

  return (
    <div
      {...rowProps}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        minHeight: 30,
        padding: '2px 0 2px 4px',
        borderBottom: '1px solid var(--border-subtle)',
        background: hover ? 'var(--ink-750)' : 'transparent',
        borderRadius: 'var(--radius-sm)',
      }}
    >
      <span style={{ flex: '0 1 auto', maxWidth: '45%', font: '400 13px/1.4 var(--font-mono)', color: 'var(--fg-secondary)', overflowWrap: 'anywhere' }}>{displayLabel}</span>
      <span
        style={{
          minWidth: 0,
          flex: 1,
          font: '400 13px/1.4 var(--font-mono)',
          color: valueColor,
          overflowWrap: 'anywhere',
        }}
      >
        <span
          style={{
            background: redacted ? 'var(--warning-soft)' : 'transparent',
            borderRadius: redacted ? 2 : 0,
            padding: redacted ? '0 4px' : 0,
          }}
        >
          {formattedValue}
        </span>
      </span>
      <InlineCopyButton label={`Copy ${displayLabel} value`} value={formattedValue} visible={revealed} />
    </div>
  )
}

/* Syntax-ish colors: keys fg-secondary, strings cat-4, numbers cat-1,
   booleans cat-5, null fg-faint, redacted warning. */
function primitiveValueColor(value: ConfigJsonPrimitive, redacted: boolean): string {
  if (redacted) return 'var(--warning)'
  if (value === null) return 'var(--fg-faint)'
  if (typeof value === 'boolean') return 'var(--cat-5)'
  if (typeof value === 'number') return 'var(--cat-1)'
  return 'var(--cat-4)'
}
