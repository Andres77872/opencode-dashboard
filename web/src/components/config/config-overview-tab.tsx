import { ChevronRight } from 'lucide-react'
import { summarizeValue, titleizeKey } from '../../lib/config-utils'
import { formatInteger } from '../../lib/format'
import { cn } from '../../lib/utils'
import type { ConfigSectionProjection } from '../../types/config'
import { Badge } from '../ui/badge'

interface ConfigOverviewTabProps {
  sectionProjections: ConfigSectionProjection[]
  searchQuery: string
  onOpenSection: (sectionKey: string) => void
}

export function ConfigOverviewTab({ sectionProjections, searchQuery, onOpenSection }: ConfigOverviewTabProps) {
  return (
    <div className="divide-y divide-border/30 overflow-hidden rounded-xl border border-border/60 bg-card/50">
      {sectionProjections.map((projection) => {
        const matchesFilter = projection.filteredValue !== null

        return (
          <button
            key={projection.section.key}
            type="button"
            className={cn(
              'group flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/20',
              searchQuery && !matchesFilter ? 'opacity-40' : '',
            )}
            onClick={() => onOpenSection(projection.section.key)}
          >
            {/* Section name */}
            <span className="text-sm font-medium text-foreground">{titleizeKey(projection.section.key)}</span>

            {/* Size summary */}
            <span className="text-xs text-muted-foreground">{summarizeValue(projection.section.value)}</span>

            {/* Leaf values count — dot-separated */}
            <span aria-hidden="true" className="text-muted-foreground/40">·</span>
            <span className="text-xs text-muted-foreground">{formatInteger(projection.insights.leafValues)} values</span>

            {/* Redacted badge */}
            {projection.insights.redactedValues > 0 ? (
              <Badge tone="warning" className="px-1.5 py-px text-[10px]">
                {formatInteger(projection.insights.redactedValues)} redacted
              </Badge>
            ) : null}

            {/* Filter match indicator */}
            {searchQuery ? (
              <span className={cn('text-xs', matchesFilter ? 'text-accent' : 'text-muted-foreground')}>
                {matchesFilter
                  ? `${formatInteger(projection.filteredInsights?.leafValues ?? 0)} matches`
                  : 'No matches'}
              </span>
            ) : null}

            {/* Chevron */}
            <span className="ml-auto shrink-0 text-muted-foreground/50 transition-colors group-hover:text-muted-foreground">
              <ChevronRight className="size-4" />
            </span>
          </button>
        )
      })}

      {sectionProjections.length === 0 ? (
        <div className="px-4 py-8 text-center text-sm text-muted-foreground">No sections available.</div>
      ) : null}
    </div>
  )
}
