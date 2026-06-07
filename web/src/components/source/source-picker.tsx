import { useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { Check, ChevronDown, Database, FileText, Layers, Server } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Button } from '../ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { cn } from '../../lib/utils'
import { useDashboardContext } from '../layout/dashboard-context'
import type { SourceID, SourceInfo } from '../../types/api'

/**
 * Global data-source switcher. Shares the PeriodPicker's chrome (outline-button
 * trigger + popover panel) so the two global filters read as one family.
 *
 * It is route-aware: on the Overview — which aggregates every source — it renders
 * a read-only "All sources" state that lists what is being aggregated, instead of
 * a per-source selector. On every other view it switches the active source.
 *
 * Unlike a plain <select>, the panel stays informative when only one source is
 * configured (it surfaces that source's path/kind), and it lets you select an
 * unavailable source so SourceNotice can explain why it can't be queried.
 */

// Map of source kind → icon. Looked up (not called) at render so the icon keeps a
// stable component identity (see react-hooks/static-components).
const KIND_ICONS: Record<string, LucideIcon> = {
  sqlite: Database,
  jsonl: FileText,
}

function kindLabel(kind: string | undefined): string {
  switch (kind) {
    case 'sqlite':
      return 'SQLite database'
    case 'jsonl':
      return 'JSONL transcripts'
    default:
      return kind ?? 'source'
  }
}

function sourceSubtitle(source: SourceInfo): string {
  if (!source.available) {
    return source.diagnostics?.reason ?? 'Not available'
  }
  if (source.path) {
    return source.path_source ? `${source.path_source}: ${source.path}` : source.path
  }
  return kindLabel(source.kind)
}

function StatusDot({ available, className }: { available: boolean; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        'size-2 shrink-0 rounded-full ring-2',
        available ? 'bg-success ring-success/20' : 'bg-warning ring-warning/20',
        className,
      )}
    />
  )
}

export function SourcePicker() {
  const {
    selectedSourceId,
    selectedSourceInfo,
    setSelectedSourceId,
    sourceMetadataLoading,
    sources,
  } = useDashboardContext()

  const aggregate = useLocation().pathname.startsWith('/overview')
  const [open, setOpen] = useState(false)
  const rowRefs = useRef<Partial<Record<SourceID, HTMLButtonElement | null>>>({})

  // Keep the selected source visible even if it is a pending/unregistered one
  // that the backend did not return in the list.
  const options = useMemo(() => {
    if (selectedSourceInfo && !sources.some((source) => source.id === selectedSourceInfo.id)) {
      return [...sources, selectedSourceInfo]
    }
    return sources
  }, [selectedSourceInfo, sources])

  // The Overview legend lists exactly what the backend aggregates (`sources`); the
  // per-source picker also keeps a selected-but-unregistered source visible.
  const listItems = aggregate ? sources : options
  const availableCount = listItems.filter((source) => source.available).length
  const singleSource = options.length <= 1
  const loadingEmpty = sourceMetadataLoading && options.length === 0

  // Roving-tabindex anchor: focus the selected row first, else the top row.
  const rovingId =
    !aggregate && options.some((source) => source.id === selectedSourceId)
      ? selectedSourceId
      : options[0]?.id

  const selectRow = (sourceId: SourceID) => {
    if (sourceId !== selectedSourceId) {
      setSelectedSourceId(sourceId)
    }
    setOpen(false)
  }

  const handleRowKeyDown = (event: React.KeyboardEvent, index: number) => {
    let nextIndex: number | null = null
    switch (event.key) {
      case 'ArrowDown':
        nextIndex = (index + 1) % options.length
        break
      case 'ArrowUp':
        nextIndex = (index - 1 + options.length) % options.length
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = options.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    const nextId = options[nextIndex]?.id
    if (nextId) {
      rowRefs.current[nextId]?.focus()
    }
  }

  const TriggerIcon = aggregate ? Layers : (KIND_ICONS[selectedSourceInfo?.kind ?? ''] ?? Server)
  const triggerLabel = loadingEmpty
    ? 'Loading sources…'
    : aggregate
      ? 'All sources'
      : (selectedSourceInfo?.label ?? selectedSourceId)
  const triggerUnavailable = !aggregate && selectedSourceInfo?.available === false

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          disabled={loadingEmpty}
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-label={aggregate ? 'Data source: all sources' : `Data source: ${triggerLabel}`}
          className="min-w-[11rem] justify-between gap-2 font-normal"
        >
          <span className="flex min-w-0 items-center gap-2">
            <TriggerIcon
              className={cn('size-4 shrink-0', triggerUnavailable ? 'text-warning' : 'opacity-70')}
            />
            <span className="truncate">{triggerLabel}</span>
          </span>
          <ChevronDown className="size-4 shrink-0 opacity-60" />
        </Button>
      </PopoverTrigger>

      <PopoverContent
        align="end"
        sideOffset={6}
        className="w-80 max-w-[calc(100vw-2rem)] p-0"
      >
        <div className="flex items-center justify-between gap-2 border-b border-border/60 px-3 py-2">
          <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            {aggregate ? 'Overview · all sources' : 'Data source'}
          </span>
          {availableCount > 0 ? (
            <span className="text-[11px] text-muted-foreground">
              {availableCount} available
            </span>
          ) : null}
        </div>

        <div
          role={aggregate ? 'list' : 'listbox'}
          aria-label="Data sources"
          className="max-h-[18rem] overflow-y-auto p-1"
        >
          {listItems.map((source, index) => {
            const selected = !aggregate && source.id === selectedSourceId
            const RowIcon = KIND_ICONS[source.kind] ?? Server

            const inner = (
              <>
                <StatusDot available={source.available} className="mt-1.5" />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex items-center gap-2">
                    <span className="truncate font-medium text-foreground">{source.label}</span>
                    {source.default ? (
                      <span className="rounded-full border border-border/70 px-1.5 py-px text-[9px] font-medium uppercase tracking-[0.12em] text-muted-foreground">
                        default
                      </span>
                    ) : null}
                    {!source.available ? (
                      <span className="rounded-full border border-warning/35 bg-warning/10 px-1.5 py-px text-[9px] font-medium uppercase tracking-[0.12em] text-warning">
                        unavailable
                      </span>
                    ) : null}
                  </span>
                  <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <RowIcon className="size-3 shrink-0 opacity-70" />
                    <span className="truncate">{sourceSubtitle(source)}</span>
                  </span>
                </span>
                {selected ? <Check className="mt-0.5 size-4 shrink-0 text-accent" /> : null}
              </>
            )

            if (aggregate) {
              return (
                <div
                  key={source.id}
                  role="listitem"
                  className="flex items-start gap-2.5 rounded-md px-2 py-2 text-sm"
                >
                  {inner}
                </div>
              )
            }

            return (
              <button
                key={source.id}
                ref={(node) => {
                  rowRefs.current[source.id] = node
                }}
                type="button"
                role="option"
                aria-selected={selected}
                tabIndex={source.id === rovingId ? 0 : -1}
                onClick={() => selectRow(source.id)}
                onKeyDown={(event) => handleRowKeyDown(event, index)}
                className={cn(
                  'flex w-full items-start gap-2.5 rounded-md px-2 py-2 text-left text-sm transition-colors',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  selected ? 'bg-muted/60' : 'hover:bg-muted/40',
                )}
              >
                {inner}
              </button>
            )
          })}
        </div>

        <p className="border-t border-border/60 px-3 py-2 text-xs leading-5 text-muted-foreground">
          {aggregate
            ? 'The Overview aggregates every source. Costs are shown per source and never combined. Open another view to filter by a single source.'
            : singleSource
              ? 'Only one data source is configured. Start with --claude-home or --codex-home to add Claude Code or Codex.'
              : 'Applies to every view except the Overview, which always aggregates all sources.'}
        </p>
      </PopoverContent>
    </Popover>
  )
}
