import type { ConfigStats } from '../../types/api'
import { Button } from '../ui/button'
import { CopyButton } from './copy-button'

interface ConfigWorkspaceHeaderProps {
  data: ConfigStats | null
  searchValue: string
  searchQuery: string
  visibleSectionCount: number
  totalSectionCount: number
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
  onSearchChange: (value: string) => void
  onClearFilter: () => void
}

export function ConfigWorkspaceHeader({
  data,
  searchValue,
  searchQuery,
  visibleSectionCount,
  totalSectionCount,
  copiedId,
  onCopy,
  onSearchChange,
  onClearFilter,
}: ConfigWorkspaceHeaderProps) {
  return (
    <div className="space-y-2">
      {/* Search row */}
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <input
            id="config-search"
            type="search"
            value={searchValue}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder="Filter keys and values…"
            className="h-9 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
        </div>

        {searchValue ? (
          <Button type="button" variant="ghost" size="xs" onClick={onClearFilter}>
            Clear
          </Button>
        ) : null}

        <CopyButton
          copyId="config-path"
          copiedId={copiedId}
          label="Copy path"
          value={data?.path ?? 'Unavailable'}
          onCopy={onCopy}
        />
        {data?.content ? (
          <CopyButton
            copyId="config-raw"
            copiedId={copiedId}
            label="Copy JSON"
            value={data.content}
            onCopy={onCopy}
          />
        ) : null}
      </div>

      {/* Subtle context line */}
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {data?.path ? (
          <span className="font-mono truncate">{data.path}</span>
        ) : null}
        {searchQuery ? (
          <span>
            · Filtering {visibleSectionCount}/{totalSectionCount} sections
          </span>
        ) : null}
      </div>
    </div>
  )
}
