import { useEffect, useMemo, useState, type MutableRefObject } from 'react'
import { TokenBreakdownList } from '../overview/token-breakdown-card'
import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'
import { Skeleton } from '../ui/skeleton'
import { getMessageDetail } from '../../lib/api'
import { formatCurrency, formatDateTime, formatInteger, formatTokenCount } from '../../lib/format'
import type { MessageDetail, MessagePart, TokenStats, ToolPart, ToolState } from '../../types/api'

function getRoleTone(role: string) {
  switch (role) {
    case 'assistant':
      return 'accent' as const
    case 'user':
      return 'success' as const
    case 'system':
      return 'warning' as const
    default:
      return 'default' as const
  }
}

function getMessageSessionLabel(message: Pick<MessageDetail, 'session_title'>) {
  return message.session_title || 'Untitled session'
}

function getModelLabel(message: Pick<MessageDetail, 'model_id' | 'provider_id'>) {
  if (message.model_id && message.provider_id) {
    return `${message.model_id} · ${message.provider_id}`
  }

  return message.model_id || message.provider_id || 'No model metadata'
}

function getTokenTotal(tokens?: TokenStats) {
  if (!tokens) {
    return 0
  }

  return tokens.input + tokens.output + tokens.reasoning + tokens.cache.read + tokens.cache.write
}

function getToolStatusTone(status?: string) {
  switch (status) {
    case 'completed':
      return 'success' as const
    case 'error':
      return 'danger' as const
    case 'running':
      return 'accent' as const
    case 'pending':
      return 'warning' as const
    default:
      return 'default' as const
  }
}

function truncateToolValue(value: string, maxChars = 240) {
  if (value.length <= maxChars) {
    return value
  }

  return `${value.slice(0, maxChars)}…`
}

function stringifyToolValue(value: unknown) {
  if (typeof value === 'string') {
    return value
  }

  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function summarizeToolInput(input: ToolState['input']) {
  if (!input || Object.keys(input).length === 0) {
    return 'No input recorded'
  }

  const summary = Object.entries(input)
    .slice(0, 4)
    .map(([key, value]) => `${key}=${truncateToolValue(stringifyToolValue(value), 48)}`)
    .join(' • ')

  const remaining = Object.keys(input).length - 4
  if (remaining > 0) {
    return `${summary} • +${remaining} more`
  }

  return summary
}

function getToolPrimarySummary(state: ToolState) {
  if (state.status === 'error' && state.error) {
    return truncateToolValue(state.error)
  }

  if (state.status === 'completed' && state.output) {
    return truncateToolValue(state.output)
  }

  if (state.title) {
    return truncateToolValue(state.title)
  }

  return null
}

function ToolSection({ parts }: { parts: ToolPart[] }) {
  return (
    <Card className="border-warning/30 bg-warning/[0.04]">
      <CardHeader className="gap-3 md:flex-row md:items-end md:justify-between">
        <div className="space-y-1.5">
          <CardDescription>Structured tool execution trace</CardDescription>
          <CardTitle>Tool activity</CardTitle>
        </div>

        <Badge tone="warning">{formatInteger(parts.length)} parts</Badge>
      </CardHeader>

      <CardContent>
        {parts.length === 0 ? (
          <div className="rounded-2xl border border-border/60 bg-background/35 px-4 py-5 text-sm text-muted-foreground">
            No tool activity was returned for this request.
          </div>
        ) : (
          <div className="space-y-3">
            {parts.map((part, index) => {
              const primarySummary = getToolPrimarySummary(part.state)

              return (
                <div key={part.call_id || `${part.tool}-${index}`} className="rounded-2xl border border-border/60 bg-background/55 px-4 py-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-sm text-foreground">{part.tool || 'unknown-tool'}</span>
                        <Badge tone={getToolStatusTone(part.state.status)}>{part.state.status || 'unknown'}</Badge>
                        {part.state.title ? <span className="text-sm text-muted-foreground">{part.state.title}</span> : null}
                      </div>

                      {part.call_id ? <div className="font-mono text-[11px] text-muted-foreground">call {part.call_id}</div> : null}
                    </div>
                  </div>

                  <div className="mt-3 space-y-2 text-sm leading-6">
                    <div className="rounded-xl border border-border/50 bg-background/45 px-3 py-2">
                      <span className="mr-2 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Input</span>
                      <span className="break-words font-mono text-xs text-foreground">{summarizeToolInput(part.state.input)}</span>
                    </div>

                    {primarySummary ? (
                      <div className="rounded-xl border border-border/50 bg-background/45 px-3 py-2 whitespace-pre-wrap break-words">
                        <span className="mr-2 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                          {part.state.status === 'error' ? 'Error' : part.state.status === 'completed' && part.state.output ? 'Output' : 'Summary'}
                        </span>
                        <span className={part.state.status === 'error' ? 'text-danger' : 'text-foreground'}>{primarySummary}</span>
                      </div>
                    ) : null}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function DetailMetric({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      <div className="mt-2 font-mono text-lg text-foreground sm:text-xl">{value}</div>
      <div className="mt-2 text-sm leading-6 text-muted-foreground">{hint}</div>
    </div>
  )
}

function DetailFact({ label, value, subtle = false }: { label: string; value: string; subtle?: boolean }) {
  return (
    <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      <div className={`mt-2 break-all font-mono text-sm ${subtle ? 'text-muted-foreground' : 'text-foreground'}`}>{value}</div>
    </div>
  )
}

function ContentSection({
  badgeTone,
  description,
  emptyCopy,
  parts,
  title,
  toneClassName,
}: {
  badgeTone: 'default' | 'accent' | 'success' | 'warning' | 'danger'
  description: string
  emptyCopy: string
  parts: MessagePart[]
  title: string
  toneClassName: string
}) {
  return (
    <Card className={toneClassName}>
      <CardHeader className="gap-3 md:flex-row md:items-end md:justify-between">
        <div className="space-y-1.5">
          <CardDescription>{description}</CardDescription>
          <CardTitle>{title}</CardTitle>
        </div>

        <Badge tone={badgeTone}>{formatInteger(parts.length)} parts</Badge>
      </CardHeader>

      <CardContent>
        {parts.length === 0 ? (
          <div className="rounded-2xl border border-border/60 bg-background/35 px-4 py-5 text-sm text-muted-foreground">
            {emptyCopy}
          </div>
        ) : (
          <div className="space-y-3">
            {parts.map((part, index) => (
              <div
                key={`${part.type}-${index}`}
                className="rounded-2xl border border-border/60 bg-background/55 px-4 py-4 text-sm leading-6 whitespace-pre-wrap break-words text-foreground"
              >
                {part.text}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function DetailSkeleton() {
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="rounded-2xl border border-border/70 bg-panel/45 px-4 py-4">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="mt-3 h-8 w-28" />
            <Skeleton className="mt-3 h-4 w-40" />
          </div>
        ))}
      </div>

      <div className="grid gap-4 2xl:grid-cols-[minmax(0,1.55fr)_minmax(18rem,22rem)]">
        <Skeleton className="h-[28rem] w-full rounded-2xl" />
        <Skeleton className="h-[22rem] w-full rounded-2xl" />
      </div>
    </div>
  )
}

export function MessageDetailSheet({
  messageId,
  onOpenChange,
  triggerRef,
}: {
  messageId: string | null
  onOpenChange: (open: boolean) => void
  triggerRef: MutableRefObject<HTMLElement | null>
}) {
  const [detail, setDetail] = useState<MessageDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [detailRequestNonce, setDetailRequestNonce] = useState(0)

  useEffect(() => {
    if (!messageId) {
      setDetail(null)
      setDetailError(null)
      setDetailLoading(false)
      return
    }

    const activeMessageId = messageId
    const controller = new AbortController()

    async function loadDetail() {
      setDetail(null)
      setDetailError(null)
      setDetailLoading(true)

      try {
        const next = await getMessageDetail(activeMessageId, controller.signal)
        setDetail(next)
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setDetailError(caught instanceof Error ? caught.message : 'Failed to load request detail')
      } finally {
        if (!controller.signal.aborted) {
          setDetailLoading(false)
        }
      }
    }

    void loadDetail()

    return () => controller.abort()
  }, [detailRequestNonce, messageId])

  const detailStats = useMemo(() => {
    const textCount = detail?.content.text_parts.length ?? 0
    const reasoningCount = detail?.content.reasoning_parts.length ?? 0
    const toolCount = detail?.content.tool_parts?.length ?? 0
    const tokenTotal = getTokenTotal(detail?.tokens)

    return {
      textCount,
      reasoningCount,
      toolCount,
      tokenTotal,
      hasContent: textCount + reasoningCount + toolCount > 0,
    }
  }, [detail])

  return (
    <Sheet open={messageId !== null} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex h-full w-full max-w-[calc(100vw-0.75rem)] flex-col overflow-hidden border-l border-border/70 bg-background shadow-[0_24px_100px_-32px_rgba(0,0,0,0.95)] sm:max-w-[42rem] xl:max-w-[min(100vw-2rem,72rem)] 2xl:max-w-[78rem]"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          triggerRef.current?.focus()
        }}
      >
        <SheetHeader className="sticky top-0 z-10 border-b border-border/70 bg-background/95 px-4 py-4 pr-14 backdrop-blur-xl sm:px-6 sm:pr-16">
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              {detail ? <Badge tone={getRoleTone(detail.role)}>{detail.role || 'unknown'}</Badge> : <Badge tone="accent">Loading</Badge>}
              {detail ? <Badge>{getMessageSessionLabel(detail)}</Badge> : null}
              {detail ? <span className="font-mono text-xs text-muted-foreground">id {detail.id.slice(0, 12)}</span> : null}
            </div>

            <div className="space-y-2">
              <div>
                <SheetTitle className="sr-only">Message detail</SheetTitle>
                <h3 className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
                  {detail ? 'Request detail' : 'Loading request detail'}
                </h3>
                <SheetDescription className="sr-only">Detailed message drawer with separated text, reasoning, and tool activity preview.</SheetDescription>
              </div>

              <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                {detail ? (
                  <>
                     <span className="font-mono text-foreground">{formatDateTime(detail.time_created)}</span>
                     <span aria-hidden="true">•</span>
                     <span>{getModelLabel(detail)}</span>
                     <span aria-hidden="true">•</span>
                     <span>{detailStats.textCount} text · {detailStats.reasoningCount} reasoning · {detailStats.toolCount} tools</span>
                   </>
                 ) : (
                   <span>Fetching verbose request content…</span>
                 )}
               </div>

               <div className="rounded-xl border border-border/70 bg-panel/40 px-3 py-2 text-sm text-muted-foreground">
                 Text, reasoning, and tool activity stay deliberately separated here so the table can remain dense instead of turning into a messy transcript dump.
               </div>
             </div>
           </div>
        </SheetHeader>

        <div className="min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-5 sm:px-6">
          {detailLoading ? (
            <DetailSkeleton />
          ) : detailError ? (
            <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <div className="font-medium text-foreground">Request detail failed to load</div>
                <div className="text-sm opacity-90">{detailError}</div>
              </div>
              <Button variant="ghost" onClick={() => setDetailRequestNonce((current) => current + 1)}>
                Retry detail
              </Button>
            </Alert>
          ) : detail ? (
            <div className="space-y-6">
              <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-4">
                <DetailMetric
                  label="Request spend"
                  value={formatCurrency(detail.cost)}
                  hint={`${detail.role || 'unknown'} role · ${detail.model_id || 'model unavailable'}`}
                />
                <DetailMetric
                  label="Token load"
                  value={formatTokenCount(detailStats.tokenTotal)}
                  hint={detail.tokens ? `${formatTokenCount(detail.tokens.input)} input · ${formatTokenCount(detail.tokens.output)} output · ${formatTokenCount(detail.tokens.reasoning)} reasoning` : 'No token telemetry recorded'}
                />
                <DetailMetric
                  label="Content blocks"
                  value={formatInteger(detailStats.textCount + detailStats.reasoningCount + detailStats.toolCount)}
                  hint={`${formatInteger(detailStats.textCount)} text parts · ${formatInteger(detailStats.reasoningCount)} reasoning parts · ${formatInteger(detailStats.toolCount)} tool parts`}
                />
                <DetailMetric
                  label="Recorded at"
                  value={formatDateTime(detail.time_created)}
                  hint={`Session ${detail.session_id.slice(0, 12)} · ${detailStats.hasContent ? 'Verbose preview available' : 'No text preview returned'}`}
                />
              </div>

              <div className="grid min-w-0 gap-4 2xl:grid-cols-[minmax(0,1.55fr)_minmax(18rem,22rem)]">
                <div className="min-w-0 space-y-4">
                  <ContentSection
                    badgeTone="default"
                    title="Message text"
                    description="Primary output"
                    emptyCopy="No normal text preview was returned for this request."
                    parts={detail.content.text_parts}
                    toneClassName="border-border/70 bg-panel/45"
                  />

                  <ContentSection
                    badgeTone="accent"
                    title="Reasoning"
                    description="Deliberately separated context"
                    emptyCopy="No reasoning preview was returned for this request."
                    parts={detail.content.reasoning_parts}
                    toneClassName="border-accent/30 bg-accent/[0.05]"
                  />

                  <ToolSection parts={detail.content.tool_parts ?? []} />
                </div>

                <div className="order-last min-w-0 space-y-4 2xl:order-none 2xl:pt-1">
                  <Card className="border-border/60 bg-background/25 shadow-none">
                    <CardHeader>
                      <CardDescription>Secondary context</CardDescription>
                      <CardTitle>Request facts</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-3 text-sm text-muted-foreground">
                      <div className="flex items-center gap-2 rounded-xl border border-border/60 bg-background/35 px-3 py-2 text-xs leading-5 text-muted-foreground">
                        <span className="font-mono text-foreground">{formatTokenCount(detailStats.tokenTotal)}</span>
                        <span>token load</span>
                        <span className="text-border">·</span>
                        <span>{formatCurrency(detail.cost)} spend</span>
                      </div>

                      {detail.tokens ? (
                        <div className="rounded-xl border border-border/60 bg-background/35 px-3 py-3">
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Token mix</div>
                          <TokenBreakdownList className="mt-3 border-t border-border/50 pt-3" hideZeroItems tokens={detail.tokens} variant="compact" />
                        </div>
                      ) : null}

                      <div className="grid gap-3 sm:grid-cols-2 2xl:grid-cols-1">
                        <DetailFact label="Session" value={getMessageSessionLabel(detail)} />
                        <DetailFact label="Session ID" value={detail.session_id} subtle={false} />
                        <DetailFact label="Model / provider" value={getModelLabel(detail)} subtle={!detail.model_id && !detail.provider_id} />
                        <DetailFact label="Message ID" value={detail.id} subtle={false} />
                      </div>
                    </CardContent>
                  </Card>
                </div>
              </div>
            </div>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}
