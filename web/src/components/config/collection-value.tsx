import { Fragment, type ReactNode, useState } from 'react'
import {
  formatDisplayLabel,
  formatPrimitiveValue,
  isPrimitive,
  isRedactedValue,
  serializeConfigValue,
  summarizeValue,
} from '../../lib/config-utils'
import { cn } from '../../lib/utils'
import type { ConfigJsonObject, ConfigJsonPrimitive, ConfigJsonValue } from '../../types/config'
import { CopyButton } from './copy-button'
import { PrimitiveField } from './primitive-field'

interface CollectionValueProps {
  label: string
  value: ConfigJsonObject | ConfigJsonValue[]
  depth: number
  path: string
  searchActive: boolean
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
  renderNode: (args: { label: string; value: ConfigJsonValue; depth: number; path: string }) => ReactNode
}

export function CollectionValue({
  label,
  value,
  depth,
  path,
  searchActive,
  copiedId,
  onCopy,
  renderNode,
}: CollectionValueProps) {
  const [open, setOpen] = useState(depth === 0)
  const expanded = searchActive ? true : open
  const displayLabel = formatDisplayLabel(label)
  const isArrayValue = Array.isArray(value)
  const primitiveItems = isArrayValue ? value.every((item) => isPrimitive(item)) : false

  const primitiveEntries = isArrayValue
    ? []
    : Object.entries(value).filter((entry): entry is [string, ConfigJsonPrimitive] => isPrimitive(entry[1]))

  const nestedEntries = isArrayValue
    ? value.map((item, index) => [`[${index}]`, item] as const)
    : Object.entries(value).filter((entry) => !isPrimitive(entry[1]))

  const summary = summarizeValue(value)

  return (
    <div className="group/collection">
      {/* Collapsible header row */}
      <button
        type="button"
        className="group/header flex w-full items-center gap-1.5 rounded px-1 py-1 text-left hover:bg-muted/20"
        onClick={() => setOpen((current) => !current)}
      >
        <svg
          className={cn(
            'h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform duration-150',
            expanded ? 'rotate-90' : '',
          )}
          viewBox="0 0 16 16"
          fill="currentColor"
        >
          <path d="M6 4l4 4-4 4" />
        </svg>

        <span className="text-sm font-medium text-foreground">{displayLabel}</span>

        <span className="text-xs text-muted-foreground">{summary}</span>

        <span className="ml-auto shrink-0 opacity-0 transition-opacity group-hover/header:opacity-100">
          <CopyButton
            copyId={`${path}-json`}
            copiedId={copiedId}
            label="Copy"
            value={serializeConfigValue(value)}
            onCopy={onCopy}
          />
        </span>
      </button>

      {/* Indented children */}
      {expanded ? (
        <div className="ml-2 border-l border-border/40 pl-3">
          {isArrayValue && value.length === 0 ? (
            <div className="px-1 py-1.5 text-sm italic text-muted-foreground">Empty collection</div>
          ) : null}

          {!isArrayValue && primitiveEntries.length === 0 && nestedEntries.length === 0 ? (
            <div className="px-1 py-1.5 text-sm italic text-muted-foreground">Empty object</div>
          ) : null}

          {/* Primitive key-value fields */}
          {!isArrayValue && primitiveEntries.length > 0 ? (
            <div>
              {primitiveEntries.map(([key, item]) => (
                <PrimitiveField key={key} label={key} value={item} path={`${path}.${key}`} copiedId={copiedId} onCopy={onCopy} />
              ))}
            </div>
          ) : null}

          {/* Primitive array items as inline chips */}
          {isArrayValue && primitiveItems && value.length > 0 ? (
            <div className="flex flex-wrap gap-1 px-1 py-1.5">
              {value.map((item, index) => {
                const textValue = formatPrimitiveValue(item as ConfigJsonPrimitive)
                const redacted = isRedactedValue(item)

                return (
                  <button
                    key={`${path}-${index}`}
                    type="button"
                    className={cn(
                      'inline-flex max-w-full items-center rounded border px-2 py-0.5 font-mono text-xs transition-colors',
                      redacted
                        ? 'border-warning/30 bg-warning/8 text-warning'
                        : 'border-border/50 text-foreground hover:bg-muted/30',
                    )}
                    onClick={() => onCopy(`${path}[${index}]`, textValue)}
                    title="Click to copy"
                  >
                    <span className="truncate">{textValue}</span>
                    {copiedId === `${path}[${index}]` ? (
                      <span className="ml-1 text-[10px] text-muted-foreground">✓</span>
                    ) : null}
                  </button>
                )
              })}
            </div>
          ) : null}

          {/* Nested collections */}
          {nestedEntries.length > 0 ? (
            <div>
              {nestedEntries.map(([key, item]) => (
                <Fragment key={`${path}.${key}`}>
                  {renderNode({
                    label: key,
                    value: item,
                    depth: depth + 1,
                    path: key.startsWith('[') ? `${path}${key}` : `${path}.${key}`,
                  })}
                </Fragment>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
