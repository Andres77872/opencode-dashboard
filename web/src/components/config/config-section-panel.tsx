import { collectInsights, serializeConfigValue, titleizeKey } from '../../lib/config-utils'
import { formatInteger } from '../../lib/format'
import type { ConfigJsonValue, ConfigSection } from '../../types/config'
import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { ConfigNode } from './config-node'
import { CopyButton } from './copy-button'

interface ConfigSectionPanelProps {
  section: ConfigSection
  visibleValue: ConfigJsonValue
  searchActive: boolean
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

export function ConfigSectionPanel({ section, visibleValue, searchActive, copiedId, onCopy }: ConfigSectionPanelProps) {
  const totalInsights = collectInsights(section.value)
  const visibleInsights = collectInsights(visibleValue)

  return (
    <div className="rounded-lg border border-border/60 bg-card/50">
      {/* Compact header */}
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/40 px-4 py-2.5">
        <div className="flex items-center gap-2">
          <h3 className="text-base font-semibold text-foreground">{titleizeKey(section.key)}</h3>
          <Badge className="font-mono text-[10px]">{section.key}</Badge>
          {totalInsights.redactedValues > 0 ? (
            <Badge tone="warning" className="text-[10px]">
              {formatInteger(totalInsights.redactedValues)} redacted
            </Badge>
          ) : null}
          {searchActive ? (
            <span className="text-xs text-muted-foreground">{formatInteger(visibleInsights.leafValues)} matches</span>
          ) : null}
        </div>

        <div className="flex items-center gap-1">
          <CopyButton
            copyId={`${section.key}-section`}
            copiedId={copiedId}
            label="Copy section"
            value={serializeConfigValue(section.value)}
            onCopy={onCopy}
          />
          {searchActive ? (
            <CopyButton
              copyId={`${section.key}-filtered`}
              copiedId={copiedId}
              label="Copy matches"
              value={serializeConfigValue(visibleValue)}
              onCopy={onCopy}
            />
          ) : null}
        </div>
      </div>

      {/* Tree content */}
      <div className="px-3 py-2">
        {searchActive ? (
          <Alert tone="info" className="mb-2 text-xs">
            Showing only keys and values matching the current filter inside <span className="font-medium text-foreground">{section.key}</span>.
          </Alert>
        ) : null}

        <ConfigNode
          label={section.key}
          value={visibleValue}
          depth={0}
          path={section.key}
          searchActive={searchActive}
          copiedId={copiedId}
          onCopy={onCopy}
        />
      </div>
    </div>
  )
}
