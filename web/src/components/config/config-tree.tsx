/* Tree view: recursive, format-agnostic rendering of the structured config.
   Expand state is seeded per-node from openDepth and reset wholesale by
   remounting via treeEpoch (expand/collapse-all are expected to reset
   individual toggles). Search-active force-expands. */
import { useState } from 'react'
import { Badge, Button, Card, Icon, Notice } from '../vael'
import { CopyButton, InlineCopyButton } from './copy-button'
import {
  ALL_SECTIONS,
  collectInsights,
  formatDisplayLabel,
  formatPrimitiveValue,
  isChipArray,
  isObject,
  isPrimitive,
  isRedactedValue,
  serializeConfigValue,
  summarizeValue,
  titleizeKey,
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
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
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
  copiedId,
  onCopy,
}: ConfigTreePaneProps) {
  const isAll = selectedKey === ALL_SECTIONS
  const title = isAll ? 'All sections' : titleizeKey(selectedKey)

  const copyValue = isAll
    ? serializeConfigValue(
        Object.fromEntries(projections.map((p) => [p.section.key, p.section.value])),
      )
    : serializeConfigValue(projections[0]?.section.value ?? null)

  const totalRedacted = projections.reduce((sum, p) => sum + p.insights.redactedValues, 0)
  const visible = projections.filter((p) => !searchActive || p.filteredValue !== null)
  const matchCount = visible.reduce((sum, p) => sum + (p.filteredInsights?.leafValues ?? 0), 0)

  const subtitle = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
      {!isAll && (
        <span style={{ font: '500 11px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>{selectedKey}</span>
      )}
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
      <CopyButton
        copyId="tree-copy"
        copiedId={copiedId}
        label={isAll ? 'Copy all' : 'Copy section'}
        value={copyValue}
        onCopy={onCopy}
      />
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
              copiedId={copiedId}
              onCopy={onCopy}
            />
          ))}
        </div>
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
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function ConfigNode({ label, value, depth, path, searchActive, openDepth, copiedId, onCopy }: ConfigNodeProps) {
  if (Array.isArray(value) || isObject(value)) {
    return (
      <CollectionValue
        label={label}
        value={value}
        depth={depth}
        path={path}
        searchActive={searchActive}
        openDepth={openDepth}
        copiedId={copiedId}
        onCopy={onCopy}
      />
    )
  }
  return <PrimitiveField label={label} value={value} path={path} copiedId={copiedId} onCopy={onCopy} />
}

interface CollectionValueProps {
  label: string
  value: ConfigJsonObject | ConfigJsonValue[]
  depth: number
  path: string
  searchActive: boolean
  openDepth: number
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function CollectionValue({ label, value, depth, path, searchActive, openDepth, copiedId, onCopy }: CollectionValueProps) {
  const [open, setOpen] = useState(() => depth < openDepth)
  const [hover, setHover] = useState(false)
  const expanded = searchActive ? true : open
  const displayLabel = formatDisplayLabel(label)
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
      <button
        type="button"
        onClick={() => setOpen((c) => !c)}
        onMouseEnter={() => setHover(true)}
        onMouseLeave={() => setHover(false)}
        aria-expanded={expanded}
        aria-label={`Toggle ${displayLabel} section`}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          width: '100%',
          minHeight: 30,
          padding: '4px 4px',
          textAlign: 'left',
          border: 'none',
          background: hover ? 'var(--ink-750)' : 'transparent',
          cursor: 'pointer',
          borderRadius: 'var(--radius-sm)',
        }}
      >
        <Icon
          name="chevron-right"
          size={14}
          color="var(--fg-muted)"
          style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform var(--dur-fast)', flexShrink: 0 }}
        />
        <span style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)' }}>{displayLabel}</span>
        <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{summarizeValue(value)}</span>
        {redactedCount > 0 && (
          <span aria-label={`${redactedCount} redacted values`} style={{ width: 6, height: 6, borderRadius: 3, background: 'var(--warning)', flexShrink: 0 }} />
        )}
        <span style={{ marginLeft: 'auto', flexShrink: 0 }}>
          <InlineCopyButton
            copyId={`${path}-json`}
            copiedId={copiedId}
            value={serializeConfigValue(value)}
            onCopy={onCopy}
            style={{ opacity: hover || copiedId === `${path}-json` ? 1 : 0, transition: 'opacity var(--dur-fast)' }}
          />
        </span>
      </button>

      {expanded && (
        <div style={{ marginLeft: 8, paddingLeft: 12, borderLeft: '1px solid var(--border-subtle)' }}>
          {isArrayValue && value.length === 0 && <EmptyLeaf label="Empty collection" />}
          {!isArrayValue && primitiveEntries.length === 0 && nestedEntries.length === 0 && <EmptyLeaf label="Empty object" />}

          {!isArrayValue && primitiveEntries.length > 0 && (
            <div>
              {primitiveEntries.map(([key, item]) => (
                <PrimitiveField key={key} label={key} value={item} path={`${path}.${key}`} copiedId={copiedId} onCopy={onCopy} />
              ))}
            </div>
          )}

          {chips && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, padding: '6px 4px' }}>
              {value.map((item, index) => {
                const textValue = formatPrimitiveValue(item as ConfigJsonPrimitive)
                const redacted = isRedactedValue(item)
                const chipCopied = copiedId === `${path}[${index}]`
                return (
                  <button
                    key={`${path}-${index}`}
                    type="button"
                    onClick={() => onCopy(`${path}[${index}]`, textValue)}
                    title="Click to copy"
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 4,
                      maxWidth: '100%',
                      padding: '2px 8px',
                      font: '400 12px/1.4 var(--font-mono)',
                      color: redacted ? 'var(--warning)' : 'var(--fg-secondary)',
                      background: redacted ? 'var(--warning-soft)' : 'var(--ink-750)',
                      border: `1px solid ${redacted ? 'var(--border-subtle)' : 'var(--border-default)'}`,
                      borderRadius: 'var(--radius-sm)',
                      cursor: 'pointer',
                    }}
                  >
                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{textValue}</span>
                    {chipCopied && <Icon name="check" size={12} color="var(--success)" />}
                  </button>
                )
              })}
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
                  copiedId={copiedId}
                  onCopy={onCopy}
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
  path: string
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function PrimitiveField({ label, value, path, copiedId, onCopy }: PrimitiveFieldProps) {
  const [hover, setHover] = useState(false)
  const redacted = isRedactedValue(value)
  const displayLabel = formatDisplayLabel(label)
  const formattedValue = formatPrimitiveValue(value)
  const valueColor = primitiveValueColor(value, redacted)

  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        minHeight: 30,
        padding: '4px 4px',
        borderBottom: '1px solid var(--border-subtle)',
        background: hover ? 'var(--ink-750)' : 'transparent',
        borderRadius: 'var(--radius-sm)',
      }}
    >
      <span style={{ flexShrink: 0, font: '400 13px/1.4 var(--font-mono)', color: 'var(--fg-secondary)' }}>{displayLabel}</span>
      <span
        style={{
          minWidth: 0,
          flex: 1,
          font: '400 13px/1.4 var(--font-mono)',
          color: valueColor,
          overflowWrap: 'anywhere',
          background: redacted ? 'var(--warning-soft)' : 'transparent',
          borderRadius: redacted ? 2 : 0,
          padding: redacted ? '0 4px' : 0,
        }}
      >
        {formattedValue}
      </span>
      <InlineCopyButton
        copyId={path}
        copiedId={copiedId}
        value={formattedValue}
        onCopy={onCopy}
        style={{ opacity: hover || copiedId === path ? 1 : 0, transition: 'opacity var(--dur-fast)' }}
      />
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
