/* Config — redacted source/config snapshot (Vael JSON viewer). Periodless: the
   config endpoint takes no period, so we lean on usePeriodResource with a fixed
   period arg + cachePeriods:false (re-fetches on source change / refresh). No
   fabricated data — everything is derived from the redacted payload. */
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Card,
  StatCard,
  SectionTitle,
  Badge,
  Button,
  Tabs,
  Skeleton,
  EmptyState,
  ErrorState,
  Notice,
  Icon,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodResource } from '../lib/use-period-resource'
import { getConfig } from '../lib/api'
import {
  buildConfigSummary,
  buildSectionProjections,
  collectInsights,
  formatDisplayLabel,
  formatPrimitiveValue,
  isObject,
  isPrimitive,
  isRedactedValue,
  normalizeSearchQuery,
  serializeConfigValue,
  summarizeValue,
  titleizeKey,
} from '../lib/config-utils'
import { formatInteger } from '../lib/format'
import type { ConfigStats, SourceID } from '../types/api'
import type { ConfigJsonObject, ConfigJsonPrimitive, ConfigJsonValue, ConfigSection, ConfigSectionProjection } from '../types/config'

/**
 * Backward-compat shim: old API returned `content` as a JSON-encoded string,
 * new API returns it as an object. Normalize to an object when possible.
 */
function resolveContent(raw: ConfigStats): ConfigStats {
  if (raw.content && typeof raw.content === 'string') {
    try {
      return { ...raw, content: JSON.parse(raw.content) }
    } catch {
      return raw
    }
  }
  return raw
}

const COPY_RESET_MS = 1600

export function ConfigView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [searchValue, setSearchValue] = useState('')
  const [activeTab, setActiveTab] = useState('overview')
  const [hasChosenTab, setHasChosenTab] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const copyResetRef = useRef<number | null>(null)

  // Periodless: fetcher ignores the period arg; cachePeriods:false always re-fetches.
  const { data: rawData, loading, error } = usePeriodResource<ConfigStats>(
    (_p: string, signal?: AbortSignal, sourceId?: SourceID) => getConfig(signal, sourceId),
    '7d',
    { cachePeriods: false },
  )

  const sourceLabel =
    selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  const data = useMemo(() => (rawData ? resolveContent(rawData) : null), [rawData])

  useEffect(() => {
    return () => {
      if (copyResetRef.current !== null) window.clearTimeout(copyResetRef.current)
    }
  }, [])

  const searchQuery = useMemo(() => normalizeSearchQuery(searchValue), [searchValue])
  const summary = useMemo(() => buildConfigSummary(data), [data])
  const sectionProjections = useMemo(
    () => buildSectionProjections(summary, searchQuery),
    [searchQuery, summary],
  )

  const sectionKeys = useMemo(() => summary?.sections.map((s) => s.key) ?? [], [summary])

  const effectiveActiveTab = useMemo(() => {
    if (!summary) return activeTab
    const available = new Set(['overview', 'raw', ...sectionKeys])
    if (!hasChosenTab && summary.sections.length > 0) return summary.sections[0].key
    if (!available.has(activeTab)) return summary.sections[0]?.key ?? 'overview'
    return activeTab
  }, [activeTab, hasChosenTab, summary, sectionKeys])

  const handleActiveTabChange = (value: string) => {
    setHasChosenTab(true)
    setActiveTab(value)
  }

  const visibleSectionCount = sectionProjections.filter((p) => p.filteredValue !== null).length
  const totalSectionCount = summary?.sections.length ?? 0

  const handleCopy = async (copyId: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedId(copyId)
      if (copyResetRef.current !== null) window.clearTimeout(copyResetRef.current)
      copyResetRef.current = window.setTimeout(() => setCopiedId(null), COPY_RESET_MS)
    } catch {
      setCopiedId(null)
    }
  }

  const contentValue =
    typeof data?.content === 'string' ? data.content : JSON.stringify(data?.content, null, 2)

  // --- Loading ---
  if (loading && !data) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12 }}>
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', padding: 16 }}>
              <Skeleton width={80} height={11} />
              <Skeleton width={110} height={26} style={{ marginTop: 12 }} />
            </div>
          ))}
        </div>
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 56 }} />
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 360 }} />
      </div>
    )
  }

  // --- Hard error (no data at all) ---
  if (error && !data) {
    return (
      <Card>
        <ErrorState title="Config failed to load" message={error} onRetry={requestRefresh} />
      </Card>
    )
  }

  if (!summary || !data) {
    return (
      <Card>
        <EmptyState icon="settings" title="No config available" description={`No ${sourceLabel} config snapshot to inspect.`} />
      </Card>
    )
  }

  const tabItems = [
    { value: 'overview', label: 'Overview' },
    ...summary.sections.map((s) => ({ value: s.key, label: titleizeKey(s.key) })),
    { value: 'raw', label: 'Raw JSON' },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && (
        <Notice tone="warning" title="Config partially loaded">{error}</Notice>
      )}

      {/* Summary metrics */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12 }}>
        <StatCard
          label="Status"
          value={data.exists ? 'Present' : 'Missing'}
          hint={data.exists ? 'Config resolved & loaded' : 'No config file detected'}
        />
        <StatCard label="Values" value={formatInteger(summary.insights.leafValues)} hint="Leaf key-value pairs" />
        <StatCard label="Collections" value={formatInteger(summary.insights.collections)} hint="Objects & arrays" />
        <StatCard
          label="Redacted"
          value={formatInteger(summary.insights.redactedValues)}
          hint={summary.insights.redactedValues > 0 ? 'Sensitive fields masked' : 'No sensitive data'}
        />
      </div>

      {/* Workspace header: path + redaction badge + copy + search */}
      <WorkspaceHeader
        data={data}
        sourceLabel={sourceLabel}
        searchValue={searchValue}
        searchQuery={searchQuery}
        visibleSectionCount={visibleSectionCount}
        totalSectionCount={totalSectionCount}
        copiedId={copiedId}
        contentValue={contentValue}
        onCopy={handleCopy}
        onSearchChange={setSearchValue}
        onClearFilter={() => setSearchValue('')}
      />

      {data.redacted && (
        <Notice tone="warning" title="Redacted snapshot">
          Sensitive values in this {sourceLabel} snapshot were redacted before display.
        </Notice>
      )}

      {/* State branches */}
      {!data.exists ? (
        <Card>
          <EmptyState
            icon="file-text"
            title="No config file found"
            description={`The resolved ${sourceLabel} source/config path has no file to inspect. Once a config exists, its payload is organized into focused section tabs automatically.`}
          />
        </Card>
      ) : summary.parseError ? (
        <ParseErrorPanel
          message={summary.parseError}
          contentValue={contentValue}
          hasContent={Boolean(data.content)}
          copiedId={copiedId}
          onCopy={handleCopy}
        />
      ) : summary.emptyObject ? (
        <Card>
          <EmptyState
            icon="file-text"
            title="Config present but empty"
            description="The file exists, but the redacted JSON payload has no inspectable keys. It may be blank, intentionally minimal, or fully redacted before aggregation."
          />
        </Card>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Tabs tabs={tabItems} value={effectiveActiveTab} onChange={handleActiveTabChange} />

          {effectiveActiveTab === 'overview' ? (
            <OverviewTab
              projections={sectionProjections}
              searchQuery={searchQuery}
              onOpenSection={handleActiveTabChange}
            />
          ) : effectiveActiveTab === 'raw' ? (
            <RawJsonPanel content={contentValue} copiedId={copiedId} onCopy={handleCopy} />
          ) : (
            (() => {
              const projection = sectionProjections.find((p) => p.section.key === effectiveActiveTab)
              if (!projection) return null
              if (projection.filteredValue !== null) {
                return (
                  <SectionPanel
                    section={projection.section}
                    visibleValue={projection.filteredValue}
                    searchActive={Boolean(searchQuery)}
                    copiedId={copiedId}
                    onCopy={handleCopy}
                  />
                )
              }
              return (
                <Card title={titleizeKey(projection.section.key)} subtitle={projection.section.key}>
                  <Notice tone="warning" title="No matches">
                    No keys or values in <span style={{ color: 'var(--fg-primary)' }}>{projection.section.key}</span> match the active filter.
                  </Notice>
                  <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                    <Button variant="secondary" size="sm" onClick={() => setSearchValue('')}>Clear filter</Button>
                    <CopyButton
                      copyId={`${projection.section.key}-copy-empty`}
                      copiedId={copiedId}
                      label="Copy section"
                      value={serializeConfigValue(projection.section.value)}
                      onCopy={handleCopy}
                    />
                  </div>
                </Card>
              )
            })()
          )}
        </div>
      )}
    </div>
  )
}

/* ---------- Workspace header ---------- */

interface WorkspaceHeaderProps {
  data: ConfigStats
  sourceLabel: string
  searchValue: string
  searchQuery: string
  visibleSectionCount: number
  totalSectionCount: number
  copiedId: string | null
  contentValue: string
  onCopy: (copyId: string, value: string) => void
  onSearchChange: (value: string) => void
  onClearFilter: () => void
}

function WorkspaceHeader({
  data,
  searchValue,
  searchQuery,
  visibleSectionCount,
  totalSectionCount,
  copiedId,
  contentValue,
  onCopy,
  onSearchChange,
  onClearFilter,
}: WorkspaceHeaderProps) {
  return (
    <Card pad={14}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {/* Path + badges + copy actions */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <Icon name="folder" size={15} color="var(--fg-faint)" />
          <span
            title={data.path}
            style={{ font: '500 12.5px/1.4 var(--font-mono)', color: 'var(--fg-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0, flex: '1 1 240px' }}
          >
            {data.path || 'Unavailable'}
          </span>
          {data.source_id && <Badge>{data.source_id}</Badge>}
          {data.redacted && <Badge tone="warning" dot>redacted</Badge>}
          <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
            <CopyButton copyId="config-path" copiedId={copiedId} label="Copy path" value={data.path || 'Unavailable'} onCopy={onCopy} />
            {data.content && (
              <CopyButton copyId="config-raw" copiedId={copiedId} label="Copy JSON" value={contentValue} onCopy={onCopy} />
            )}
          </div>
        </div>

        {/* Search */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div
            style={{ position: 'relative', flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', height: 34, padding: '0 10px', background: 'var(--ink-850)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-md)' }}
          >
            <Icon name="search" size={15} color="var(--fg-faint)" />
            <input
              type="search"
              value={searchValue}
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder="Filter keys and values…"
              aria-label="Filter config keys and values"
              style={{ flex: 1, minWidth: 0, marginLeft: 8, border: 'none', outline: 'none', background: 'transparent', color: 'var(--fg-primary)', font: '400 13px/1 var(--font-ui)' }}
            />
            {searchValue && (
              <button
                type="button"
                onClick={onClearFilter}
                aria-label="Clear search filter"
                style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', border: 'none', background: 'transparent', color: 'var(--fg-faint)', cursor: 'pointer', padding: 2 }}
              >
                <Icon name="x" size={15} />
              </button>
            )}
          </div>
          {searchQuery && totalSectionCount > 0 && (
            <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
              {visibleSectionCount}/{totalSectionCount} sections
            </span>
          )}
        </div>
      </div>
    </Card>
  )
}

/* ---------- Copy button ---------- */

interface CopyButtonProps {
  copyId: string
  copiedId: string | null
  label: string
  value: string
  onCopy: (copyId: string, value: string) => void
}

function CopyButton({ copyId, copiedId, label, value, onCopy }: CopyButtonProps) {
  const copied = copiedId === copyId
  return (
    <button
      type="button"
      onClick={() => onCopy(copyId, value)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        height: 28,
        padding: '0 10px',
        font: '600 12px/1 var(--font-ui)',
        color: copied ? 'var(--success)' : 'var(--fg-secondary)',
        background: 'transparent',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-md)',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
        flexShrink: 0,
      }}
    >
      <Icon name={copied ? 'check' : 'copy'} size={14} />
      {copied ? 'Copied' : label}
    </button>
  )
}

/* ---------- Overview tab ---------- */

interface OverviewTabProps {
  projections: ConfigSectionProjection[]
  searchQuery: string
  onOpenSection: (key: string) => void
}

function OverviewTab({ projections, searchQuery, onOpenSection }: OverviewTabProps) {
  if (projections.length === 0) {
    return (
      <Card>
        <EmptyState icon="info" title="No sections available" />
      </Card>
    )
  }
  return (
    <Card pad={0}>
      <div>
        {projections.map((projection, idx) => {
          const matches = projection.filteredValue !== null
          const dim = Boolean(searchQuery) && !matches
          return (
            <button
              key={projection.section.key}
              type="button"
              onClick={() => onOpenSection(projection.section.key)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                width: '100%',
                padding: '12px 16px',
                textAlign: 'left',
                border: 'none',
                borderTop: idx === 0 ? 'none' : '1px solid var(--border-subtle)',
                background: 'transparent',
                cursor: 'pointer',
                opacity: dim ? 0.4 : 1,
              }}
            >
              <span style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)' }}>{titleizeKey(projection.section.key)}</span>
              <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{summarizeValue(projection.section.value)}</span>
              <span style={{ color: 'var(--fg-faint)' }}>·</span>
              <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>{formatInteger(projection.insights.leafValues)} values</span>
              {projection.insights.redactedValues > 0 && (
                <Badge tone="warning">{formatInteger(projection.insights.redactedValues)} redacted</Badge>
              )}
              {searchQuery && (
                <span style={{ font: '500 12px/1 var(--font-ui)', color: matches ? 'var(--accent)' : 'var(--fg-muted)' }}>
                  {matches ? `${formatInteger(projection.filteredInsights?.leafValues ?? 0)} matches` : 'No matches'}
                </span>
              )}
              <span style={{ marginLeft: 'auto', display: 'inline-flex', color: 'var(--fg-faint)' }}>
                <Icon name="chevron-right" size={16} />
              </span>
            </button>
          )
        })}
      </div>
    </Card>
  )
}

/* ---------- Section panel ---------- */

interface SectionPanelProps {
  section: ConfigSection
  visibleValue: ConfigJsonValue
  searchActive: boolean
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function SectionPanel({ section, visibleValue, searchActive, copiedId, onCopy }: SectionPanelProps) {
  const totalInsights = collectInsights(section.value)
  const visibleInsights = collectInsights(visibleValue)

  const action = (
    <div style={{ display: 'flex', gap: 6 }}>
      <CopyButton copyId={`${section.key}-section`} copiedId={copiedId} label="Copy section" value={serializeConfigValue(section.value)} onCopy={onCopy} />
      {searchActive && (
        <CopyButton copyId={`${section.key}-filtered`} copiedId={copiedId} label="Copy matches" value={serializeConfigValue(visibleValue)} onCopy={onCopy} />
      )}
    </div>
  )

  const subtitle = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <span style={{ font: '500 11px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>{section.key}</span>
      {totalInsights.redactedValues > 0 && <Badge tone="warning">{formatInteger(totalInsights.redactedValues)} redacted</Badge>}
      {searchActive && <span style={{ color: 'var(--fg-faint)' }}>{formatInteger(visibleInsights.leafValues)} matches</span>}
    </span>
  )

  return (
    <Card title={titleizeKey(section.key)} subtitle={subtitle} action={action}>
      {searchActive && (
        <div style={{ marginBottom: 10 }}>
          <Notice tone="info">Showing only keys and values matching the current filter inside {section.key}.</Notice>
        </div>
      )}
      <ConfigNode
        label={section.key}
        value={visibleValue}
        depth={0}
        path={section.key}
        searchActive={searchActive}
        copiedId={copiedId}
        onCopy={onCopy}
      />
    </Card>
  )
}

/* ---------- Config tree node ---------- */

interface ConfigNodeProps {
  label: string
  value: ConfigJsonValue
  depth: number
  path: string
  searchActive: boolean
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function ConfigNode({ label, value, depth, path, searchActive, copiedId, onCopy }: ConfigNodeProps) {
  if (Array.isArray(value) || isObject(value)) {
    return (
      <CollectionValue
        label={label}
        value={value}
        depth={depth}
        path={path}
        searchActive={searchActive}
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
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function CollectionValue({ label, value, depth, path, searchActive, copiedId, onCopy }: CollectionValueProps) {
  const [open, setOpen] = useState(depth === 0)
  const expanded = searchActive ? true : open
  const displayLabel = formatDisplayLabel(label)
  const isArrayValue = Array.isArray(value)
  const primitiveItems = isArrayValue ? value.every((item) => isPrimitive(item)) : false

  const primitiveEntries = isArrayValue
    ? []
    : Object.entries(value).filter((entry): entry is [string, ConfigJsonPrimitive] => isPrimitive(entry[1]))

  const nestedEntries: Array<readonly [string, ConfigJsonValue]> = isArrayValue
    ? value.map((item, index) => [`[${index}]`, item] as const)
    : Object.entries(value).filter((entry) => !isPrimitive(entry[1]))

  const summary = summarizeValue(value)

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((c) => !c)}
        aria-expanded={expanded}
        aria-label={`Toggle ${displayLabel} section`}
        style={{ display: 'flex', alignItems: 'center', gap: 6, width: '100%', padding: '5px 4px', textAlign: 'left', border: 'none', background: 'transparent', cursor: 'pointer', borderRadius: 'var(--radius-sm)' }}
      >
        <Icon name="chevron-right" size={14} color="var(--fg-muted)" style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
        <span style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)' }}>{displayLabel}</span>
        <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{summary}</span>
        <span style={{ marginLeft: 'auto', flexShrink: 0 }}>
          <CopyButton copyId={`${path}-json`} copiedId={copiedId} label="Copy" value={serializeConfigValue(value)} onCopy={onCopy} />
        </span>
      </button>

      {expanded && (
        <div style={{ marginLeft: 8, paddingLeft: 12, borderLeft: '1px solid var(--border-subtle)' }}>
          {isArrayValue && value.length === 0 && (
            <div style={{ padding: '6px 4px', font: '400 13px/1 var(--font-ui)', fontStyle: 'italic', color: 'var(--fg-muted)' }}>Empty collection</div>
          )}
          {!isArrayValue && primitiveEntries.length === 0 && nestedEntries.length === 0 && (
            <div style={{ padding: '6px 4px', font: '400 13px/1 var(--font-ui)', fontStyle: 'italic', color: 'var(--fg-muted)' }}>Empty object</div>
          )}

          {!isArrayValue && primitiveEntries.length > 0 && (
            <div>
              {primitiveEntries.map(([key, item]) => (
                <PrimitiveField key={key} label={key} value={item} path={`${path}.${key}`} copiedId={copiedId} onCopy={onCopy} />
              ))}
            </div>
          )}

          {isArrayValue && primitiveItems && value.length > 0 && (
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

interface PrimitiveFieldProps {
  label: string
  value: ConfigJsonPrimitive
  path: string
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function PrimitiveField({ label, value, path, copiedId, onCopy }: PrimitiveFieldProps) {
  const redacted = isRedactedValue(value)
  const displayLabel = formatDisplayLabel(label)
  const formattedValue = formatPrimitiveValue(value)
  const valueColor = primitiveValueColor(value, redacted)

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, minHeight: 32, padding: '5px 4px', borderBottom: '1px solid var(--border-subtle)' }}>
      <span style={{ flexShrink: 0, font: '400 13px/1.4 var(--font-mono)', color: 'var(--fg-secondary)' }}>{displayLabel}</span>
      <span style={{ minWidth: 0, flex: 1, font: '400 13px/1.4 var(--font-mono)', color: valueColor, overflowWrap: 'anywhere' }}>{formattedValue}</span>
      <span style={{ flexShrink: 0 }}>
        <CopyButton copyId={path} copiedId={copiedId} label="Copy" value={formattedValue} onCopy={onCopy} />
      </span>
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

/* ---------- Raw JSON panel ---------- */

interface RawJsonPanelProps {
  content: string
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function RawJsonPanel({ content, copiedId, onCopy }: RawJsonPanelProps) {
  const lines = useMemo(() => content.split('\n'), [content])
  return (
    <Card
      title="Raw redacted JSON"
      subtitle="Exact payload for verification — the section tabs are better for browsing."
      action={<CopyButton copyId="raw-tab-copy" copiedId={copiedId} label="Copy JSON" value={content} onCopy={onCopy} />}
      pad={0}
    >
      <div style={{ overflow: 'auto', background: 'var(--ink-850)', maxHeight: 'calc(100vh - 320px)', borderRadius: '0 0 var(--radius-xl) var(--radius-xl)' }}>
        <div style={{ font: '400 12.5px/1.75 var(--font-mono)', minWidth: 'max-content' }}>
          {lines.map((ln, i) => (
            <div key={i} style={{ display: 'flex' }}>
              <span style={{ width: 48, flexShrink: 0, textAlign: 'right', paddingRight: 14, color: 'var(--fg-faint)', userSelect: 'none', borderRight: '1px solid var(--border-subtle)', background: 'var(--ink-850)', position: 'sticky', left: 0, fontVariantNumeric: 'tabular-nums' }}>{i + 1}</span>
              <code style={{ paddingLeft: 16, paddingRight: 16, whiteSpace: 'pre', color: 'var(--fg-secondary)' }}>{ln}</code>
            </div>
          ))}
        </div>
      </div>
    </Card>
  )
}

/* ---------- Parse error fallback ---------- */

interface ParseErrorPanelProps {
  message: string
  contentValue: string
  hasContent: boolean
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

function ParseErrorPanel({ message, contentValue, hasContent, copiedId, onCopy }: ParseErrorPanelProps) {
  return (
    <Card
      title="Structured parsing failed"
      subtitle="The raw redacted snapshot is still available below."
      action={hasContent ? <CopyButton copyId="parse-error-raw" copiedId={copiedId} label="Copy JSON" value={contentValue} onCopy={onCopy} /> : undefined}
    >
      <SectionTitle sub={message}>Fallback view</SectionTitle>
      <Notice tone="warning" title="Parser error">{message}</Notice>
      <div style={{ marginTop: 12, maxHeight: '32rem', overflow: 'auto', borderRadius: 'var(--radius-lg)', border: '1px solid var(--border-default)', background: 'var(--ink-850)', padding: 12 }}>
        <pre style={{ margin: 0, font: '400 12.5px/1.75 var(--font-mono)', color: 'var(--fg-secondary)', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{contentValue}</pre>
      </div>
    </Card>
  )
}
