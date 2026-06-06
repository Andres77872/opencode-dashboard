import { Search, X } from 'lucide-react'
import type { ConfigStats } from '../../types/api'
import { Badge } from '../ui/badge'
import { Input } from '../ui/input'
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
      {/* Search + actions row */}
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <label htmlFor="config-search" className="sr-only">
            Filter config keys and values
          </label>
          <Input
            id="config-search"
            type="search"
            value={searchValue}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder="Filter keys and values…"
            className="pl-8"
          />
          {searchValue ? (
            <button
              type="button"
              onClick={onClearFilter}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
              aria-label="Clear search filter"
            >
              <X className="size-4" />
            </button>
          ) : null}
        </div>

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
            value={JSON.stringify(data.content, null, 2)}
            onCopy={onCopy}
          />
        ) : null}
      </div>

      {/* Context line */}
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {data?.source_id ? <Badge className="px-2 py-0.5 text-[9px]">{data.source_id}</Badge> : null}
        {data?.redacted ? <Badge tone="warning" className="px-2 py-0.5 text-[9px]">redacted</Badge> : null}
        {data?.path ? (
          <span className="font-mono truncate" title={data.path}>
            {data.path}
          </span>
        ) : null}
        {searchQuery && totalSectionCount > 0 ? (
          <span>
            · {visibleSectionCount}/{totalSectionCount} sections match
          </span>
        ) : null}
      </div>
    </div>
  )
}
