import { useEffect, useMemo, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { ConfigSkeleton } from '../components/config/config-skeleton'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { getConfig } from '../lib/api'
import { formatInteger } from '../lib/format'
import type { ConfigStats } from '../types/api'

type ConfigJsonPrimitive = string | number | boolean | null
type ConfigJsonValue = ConfigJsonPrimitive | ConfigJsonObject | ConfigJsonValue[]

interface ConfigJsonObject {
  [key: string]: ConfigJsonValue
}

interface ConfigSection {
  key: string
  value: ConfigJsonValue
}

interface ConfigInsights {
  leafValues: number
  redactedValues: number
  collections: number
}

const REDACTED_VALUE = '[REDACTED]'

function isObject(value: ConfigJsonValue): value is ConfigJsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isPrimitive(value: ConfigJsonValue): value is ConfigJsonPrimitive {
  return !isObject(value) && !Array.isArray(value)
}

function isRedactedValue(value: ConfigJsonValue) {
  return typeof value === 'string' && value === REDACTED_VALUE
}

function titleizeKey(key: string) {
  if (!key) {
    return 'Root'
  }

  return key
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/[_.-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function formatPrimitiveValue(value: ConfigJsonPrimitive) {
  if (value === null) {
    return 'null'
  }

  if (typeof value === 'boolean') {
    return value ? 'true' : 'false'
  }

  return String(value)
}

function summarizeValue(value: ConfigJsonValue) {
  if (Array.isArray(value)) {
    return `${formatInteger(value.length)} item${value.length === 1 ? '' : 's'}`
  }

  if (isObject(value)) {
    const count = Object.keys(value).length
    return `${formatInteger(count)} key${count === 1 ? '' : 's'}`
  }

  if (value === null) {
    return 'Null'
  }

  return typeof value
}

function collectInsights(value: ConfigJsonValue): ConfigInsights {
  if (isPrimitive(value)) {
    return {
      leafValues: 1,
      redactedValues: isRedactedValue(value) ? 1 : 0,
      collections: 0,
    }
  }

  if (Array.isArray(value)) {
    return value.reduce<ConfigInsights>(
      (accumulator, item) => {
        const next = collectInsights(item)

        return {
          leafValues: accumulator.leafValues + next.leafValues,
          redactedValues: accumulator.redactedValues + next.redactedValues,
          collections: accumulator.collections + next.collections,
        }
      },
      { leafValues: 0, redactedValues: 0, collections: 1 },
    )
  }

  return Object.values(value).reduce<ConfigInsights>(
    (accumulator, item) => {
      const next = collectInsights(item)

      return {
        leafValues: accumulator.leafValues + next.leafValues,
        redactedValues: accumulator.redactedValues + next.redactedValues,
        collections: accumulator.collections + next.collections,
      }
    },
    { leafValues: 0, redactedValues: 0, collections: 1 },
  )
}

function parseConfigContent(content?: string) {
  if (!content) {
    return {
      parsed: null as ConfigJsonValue | null,
      parseError: 'The API did not return a config payload to inspect.',
    }
  }

  try {
    return {
      parsed: JSON.parse(content) as ConfigJsonValue,
      parseError: null,
    }
  } catch (error) {
    return {
      parsed: null,
      parseError: error instanceof Error ? error.message : 'Failed to parse config JSON',
    }
  }
}

function getSections(value: ConfigJsonValue | null): ConfigSection[] {
  if (!value) {
    return []
  }

  if (isObject(value)) {
    const entries = Object.entries(value)
    if (entries.length > 0) {
      return entries.map(([key, sectionValue]) => ({ key, value: sectionValue }))
    }
  }

  return [{ key: 'root', value }]
}

function PrimitiveField({ label, value }: { label: string; value: ConfigJsonPrimitive }) {
  const redacted = isRedactedValue(value)

  return (
    <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
        {redacted ? <Badge tone="warning">Redacted</Badge> : null}
      </div>
      <div className="mt-2 break-all font-mono text-sm text-foreground">{formatPrimitiveValue(value)}</div>
    </div>
  )
}

function ArrayValue({ label, value }: { label: string; value: ConfigJsonValue[] }) {
  const primitiveItems = value.every((item) => isPrimitive(item))

  return (
    <div className="rounded-2xl border border-border/70 bg-background/35 px-3 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
          <div className="mt-1 text-sm text-muted-foreground">Array · {summarizeValue(value)}</div>
        </div>
        <Badge>{value.length === 0 ? 'Empty' : 'Collection'}</Badge>
      </div>

      {value.length === 0 ? (
        <div className="mt-3 text-sm text-muted-foreground">No items in this collection.</div>
      ) : primitiveItems ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {value.map((item, index) => (
            <span
              key={`${label}-${index}`}
              className="inline-flex max-w-full items-center rounded-full border border-border/70 bg-panel/55 px-3 py-1.5 font-mono text-xs text-foreground"
            >
              {isRedactedValue(item) ? REDACTED_VALUE : formatPrimitiveValue(item)}
            </span>
          ))}
        </div>
      ) : (
        <div className="mt-3 space-y-2">
          {value.map((item, index) => (
            <ConfigNode key={`${label}-${index}`} label={`[${index}]`} value={item} depth={1} />
          ))}
        </div>
      )}
    </div>
  )
}

function ObjectValue({ label, value, depth }: { label: string; value: ConfigJsonObject; depth: number }) {
  const entries = Object.entries(value)
  const primitiveEntries = entries.filter(
    (entry): entry is [string, ConfigJsonPrimitive] => isPrimitive(entry[1]),
  )
  const nestedEntries = entries.filter((entry) => !isPrimitive(entry[1]))

  return (
    <div className="rounded-2xl border border-border/70 bg-background/35 px-3 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
          <div className="mt-1 text-sm text-muted-foreground">Object · {summarizeValue(value)}</div>
        </div>
        <Badge>{depth === 0 ? 'Section' : 'Nested'}</Badge>
      </div>

      {entries.length === 0 ? <div className="mt-3 text-sm text-muted-foreground">No nested keys.</div> : null}

      {primitiveEntries.length > 0 ? (
        <div className="mt-3 grid gap-2 sm:grid-cols-2">
          {primitiveEntries.map(([key, item]) => (
            <PrimitiveField key={key} label={key} value={item} />
          ))}
        </div>
      ) : null}

      {nestedEntries.length > 0 ? (
        <div className="mt-3 space-y-2">
          {nestedEntries.map(([key, item]) => (
            <ConfigNode key={key} label={key} value={item} depth={depth + 1} />
          ))}
        </div>
      ) : null}
    </div>
  )
}

function ConfigNode({ label, value, depth }: { label: string; value: ConfigJsonValue; depth: number }) {
  if (Array.isArray(value)) {
    return <ArrayValue label={label} value={value} />
  }

  if (isObject(value)) {
    return <ObjectValue label={label} value={value} depth={depth} />
  }

  return <PrimitiveField label={label} value={value} />
}

function ConfigSectionCard({ section }: { section: ConfigSection }) {
  const sectionInsights = collectInsights(section.value)

  return (
    <Card className="border-border/70 bg-linear-to-b from-card to-panel">
      <CardHeader className="gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1.5">
          <CardDescription>Grouped section</CardDescription>
          <CardTitle className="text-base">{titleizeKey(section.key)}</CardTitle>
        </div>

        <div className="flex flex-wrap gap-2">
          <Badge>{section.key}</Badge>
          <Badge tone="accent">{summarizeValue(section.value)}</Badge>
          {sectionInsights.redactedValues > 0 ? <Badge tone="warning">{formatInteger(sectionInsights.redactedValues)} redacted</Badge> : null}
        </div>
      </CardHeader>

      <CardContent>
        <ConfigNode label={section.key} value={section.value} depth={0} />
      </CardContent>
    </Card>
  )
}

export function ConfigView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [data, setData] = useState<ConfigStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const hasLoadedOnceRef = useRef(false)

  useEffect(() => {
    const controller = new AbortController()
    let ignore = false

    async function loadConfig() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      }

      try {
        const next = await getConfig(controller.signal)

        if (ignore) {
          return
        }

        setData(next)
        hasLoadedOnceRef.current = true
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted || ignore) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load config')
      } finally {
        if (!controller.signal.aborted && !ignore) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadConfig()

    return () => {
      ignore = true
      controller.abort()
    }
  }, [refreshNonce, setLastUpdatedAt, setRefreshing])

  const parsedState = useMemo(() => parseConfigContent(data?.content), [data?.content])

  const summary = useMemo(() => {
    if (!data) {
      return null
    }

    const sections = getSections(parsedState.parsed)
    const insights = parsedState.parsed ? collectInsights(parsedState.parsed) : { leafValues: 0, redactedValues: 0, collections: 0 }

    return {
      sections,
      insights,
      parseError: parsedState.parseError,
      emptyObject: data.exists && sections.length === 0,
    }
  }, [data, parsedState.parseError, parsedState.parsed])

  const handleRetry = () => {
    requestRefresh()
  }

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Config</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Secret-safe config inspection from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/config</code>, rendered as grouped cards instead of a lazy raw blob.
            </p>
          </div>
        </div>
        <ConfigSkeleton />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Config</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Dense read-only inspection of the detected OpenCode config file, with grouped sections, explicit redaction cues, and zero fake visibility into secrets the backend already masked.
          </p>
        </div>

        <div className="text-sm text-muted-foreground">
          Endpoint: <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/config</code>
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Config failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard
              label="File status"
              value={data?.exists ? 'Present' : 'Missing'}
              hint={data?.exists ? 'Config file detected at the resolved XDG path' : 'No file found at the resolved XDG path'}
            />
            <MetricCard
              label="Grouped sections"
              value={formatInteger(summary.sections.length)}
              hint={summary.sections.length === 1 ? 'Single inspection card in the current payload' : 'Top-level cards derived from the redacted JSON payload'}
            />
            <MetricCard
              label="Visible values"
              value={formatInteger(summary.insights.leafValues)}
              hint={`${formatInteger(summary.insights.collections)} nested object/array collections in the current payload`}
            />
            <MetricCard
              label="Redacted fields"
              value={formatInteger(summary.insights.redactedValues)}
              hint="Sensitive-looking values were masked server-side before this UI received them"
            />
          </div>

          <div className="grid gap-4 xl:grid-cols-[1.2fr_0.8fr]">
            <Card>
              <CardHeader>
                <CardDescription>Runtime and detection</CardDescription>
                <CardTitle>Config file status</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <div className="rounded-2xl border border-border/70 bg-background/40 px-4 py-4">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Detected path</div>
                  <div className="mt-2 break-all font-mono text-sm text-foreground">{data?.path ?? 'Unavailable'}</div>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="rounded-2xl border border-border/70 bg-background/40 px-4 py-4">
                    <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Availability</div>
                    <div className="mt-2 font-mono text-base text-foreground">{data?.exists ? 'File found' : 'No file found'}</div>
                    <div className="mt-1 text-sm text-muted-foreground">
                      {data?.exists ? 'The config endpoint returned a redacted JSON snapshot.' : 'This can be normal on a fresh machine or if OpenCode never wrote a config file.'}
                    </div>
                  </div>

                  <div className="rounded-2xl border border-border/70 bg-background/40 px-4 py-4">
                    <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Payload contract</div>
                    <div className="mt-2 font-mono text-base text-foreground">Minimal and honest</div>
                    <div className="mt-1 text-sm text-muted-foreground">
                      The API exposes path, existence, and a redacted JSON string. This view groups that payload client-side instead of inventing hidden backend fields.
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card className="border-border/70 bg-panel/55">
              <CardHeader>
                <CardDescription>Inspection cues</CardDescription>
                <CardTitle>What this route guarantees</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Secret handling</div>
                  <div className="mt-2 leading-6 text-muted-foreground">
                    The backend redacts exact sensitive keys like <span className="font-mono text-foreground">apiKey</span>, <span className="font-mono text-foreground">token</span>, <span className="font-mono text-foreground">secret</span>, and masks arrays under <span className="font-mono text-foreground">env*</span> and <span className="font-mono text-foreground">header*</span> prefixes before anything reaches the browser.
                  </div>
                </div>

                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Primary artifact</div>
                  <div className="mt-2 leading-6 text-muted-foreground">
                    Config is rendered as grouped cards because the web plan calls for <span className="font-semibold text-foreground">stacked accordions/cards</span>, not a sloppy JSON wall pretending to be UX.
                  </div>
                </div>

                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Caveat</div>
                  <div className="mt-2 leading-6 text-muted-foreground">
                    If backend redaction rules expand later, this route will automatically show more <span className="font-semibold text-foreground">Redacted</span> markers without changing the page contract.
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          {!data?.exists ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>No config file found</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>
                  Nothing is wrong by default. The route checked the resolved XDG config path and found no file to inspect, so the shell stays usable and tells you exactly where it looked.
                </p>
                <p>
                  Once <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">opencode.json</code> exists, this page will group the returned sections and flag any values the backend masked as sensitive.
                </p>
              </CardContent>
            </Card>
          ) : summary.parseError ? (
            <Card>
              <CardHeader>
                <CardDescription>Fallback state</CardDescription>
                <CardTitle>Structured parsing failed</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4 text-sm text-muted-foreground">
                <p>
                  The config endpoint returned a payload, but the browser could not re-parse it into grouped cards. The raw redacted snapshot is shown below so inspection stays possible.
                </p>
                <Alert tone="warning">{summary.parseError}</Alert>
                <div className="max-h-[32rem] overflow-auto rounded-2xl border border-border/70 bg-background/40 p-4 font-mono text-xs leading-6 text-foreground">
                  <pre className="whitespace-pre-wrap break-words">{data.content}</pre>
                </div>
              </CardContent>
            </Card>
          ) : summary.emptyObject ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>Config file is present but has no inspectable keys</CardTitle>
              </CardHeader>
              <CardContent className="text-sm text-muted-foreground">
                The file exists, but the redacted JSON payload is effectively empty. That usually means the file is blank, minimal, or all meaningful content was removed before aggregation.
              </CardContent>
            </Card>
          ) : (
            <div className="grid gap-4 xl:grid-cols-2">
              {summary.sections.map((section) => (
                <ConfigSectionCard key={section.key} section={section} />
              ))}
            </div>
          )}
        </>
      ) : null}
    </section>
  )
}
