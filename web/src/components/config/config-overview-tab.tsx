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
    <div className="rounded-lg border border-border/60 bg-card/50 divide-y divide-border/30">
      {sectionProjections.map((projection) => {
        const matchesFilter = projection.filteredValue !== null

        return (
          <button
            key={projection.section.key}
            type="button"
            className={cn(
              'flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/20',
              searchQuery && !matchesFilter ? 'opacity-40' : '',
            )}
            onClick={() => onOpenSection(projection.section.key)}
          >
            {/* Section name */}
            <span className="text-sm font-medium text-foreground">{titleizeKey(projection.section.key)}</span>

            {/* Key/items count */}
            <span className="text-xs text-muted-foreground">{summarizeValue(projection.section.value)}</span>

            {/* Leaf values */}
            <span className="text-xs text-muted-foreground">
              · {formatInteger(projection.insights.leafValues)} values
            </span>

            {/* Redacted badge if any */}
            {projection.insights.redactedValues > 0 ? (
              <Badge tone="warning" className="text-[10px] py-0.5 px-1.5">
                {formatInteger(projection.insights.redactedValues)} redacted
              </Badge>
            ) : null}

            {/* Filter match status */}
            {searchQuery ? (
              <span className={cn('text-xs', matchesFilter ? 'text-accent' : 'text-muted-foreground')}>
                {matchesFilter
                  ? `${formatInteger(projection.filteredInsights?.leafValues ?? 0)} matches`
                  : 'No matches'}
              </span>
            ) : null}

            {/* Open arrow */}
            <span className="ml-auto text-muted-foreground">
              <svg className="h-4 w-4" viewBox="0 0 16 16" fill="currentColor">
                <path d="M6 4l4 4-4 4" />
              </svg>
            </span>
          </button>
        )
      })}

      {sectionProjections.length === 0 ? (
        <div className="px-4 py-6 text-center text-sm text-muted-foreground">No sections available.</div>
      ) : null}
    </div>
  )
}
