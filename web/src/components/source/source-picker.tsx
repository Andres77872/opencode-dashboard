import { Badge } from '../ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../ui/select'
import { useDashboardContext } from '../layout/dashboard-context'
import { isSourceID, type SourceInfo } from '../../types/api'

function getSourceSubtitle(source: SourceInfo) {
  if (!source.available) {
    return source.diagnostics?.reason ?? 'Unavailable'
  }
  if (source.path) {
    return source.path_source ? `${source.path_source}: ${source.path}` : source.path
  }
  return source.kind
}

export function SourcePicker() {
  const {
    selectedSourceId,
    selectedSourceInfo,
    setSelectedSourceId,
    sourceMetadataLoading,
    sources,
  } = useDashboardContext()

  const options = selectedSourceInfo && !sources.some((source) => source.id === selectedSourceInfo.id)
    ? [...sources, selectedSourceInfo]
    : sources
  const availableCount = options.filter((source) => source.available).length
  const singleAvailableSource = availableCount <= 1
  const selectDisabled = sourceMetadataLoading || options.length === 0 || (singleAvailableSource && selectedSourceInfo?.available === true)

  return (
    <div className="flex items-center gap-2">
      <span className="hidden text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground sm:inline">
        Source
      </span>
      <Select
        value={selectedSourceId}
        disabled={selectDisabled}
        onValueChange={(value) => {
          if (isSourceID(value)) {
            setSelectedSourceId(value)
          }
        }}
      >
        <SelectTrigger
          size="sm"
          className="w-[11rem] justify-between bg-panel/45"
          aria-label="Select data source"
        >
          <SelectValue placeholder={sourceMetadataLoading ? 'Loading sources…' : 'Select source'}>
            {selectedSourceInfo?.label ?? selectedSourceId}
          </SelectValue>
        </SelectTrigger>
        <SelectContent align="end" className="min-w-[18rem]">
          {options.map((source) => (
            <SelectItem key={source.id} value={source.id} disabled={!source.available}>
              <span className="flex min-w-0 flex-col gap-1 py-1">
                <span className="flex items-center gap-2">
                  <span className="font-medium">{source.label}</span>
                  {source.default ? <Badge className="px-1.5 py-0 text-[9px]">default</Badge> : null}
                  {!source.available ? <Badge tone="warning" className="px-1.5 py-0 text-[9px]">unavailable</Badge> : null}
                </span>
                <span className="max-w-[15rem] truncate text-xs text-muted-foreground">
                  {getSourceSubtitle(source)}
                </span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {singleAvailableSource ? (
        <Badge className="hidden px-2 py-0.5 text-[9px] md:inline-flex">
          single source
        </Badge>
      ) : null}
    </div>
  )
}
