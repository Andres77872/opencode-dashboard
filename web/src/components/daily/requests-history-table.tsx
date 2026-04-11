import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Skeleton } from '../ui/skeleton'
import { SortButton } from '../ui/sort-button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { formatCompactCurrency, formatDateTime, formatInteger, formatTokenCount } from '../../lib/format'
import { getAriaSort } from '../../lib/table-sort'
import type { SortDirection, SortState } from '../../lib/table-sort'
import type { DailyPeriod, MessageEntry, MessageList, TokenStats } from '../../types/api'

const FALLBACK_PAGE_SIZE = 12

export type RequestsSortKey = 'role' | 'time' | 'model' | 'cost' | 'tokens'

export const REQUESTS_SORT_DEFAULTS: Record<RequestsSortKey, SortDirection> = {
  cost: 'desc',
  model: 'asc',
  role: 'asc',
  time: 'desc',
  tokens: 'desc',
}

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

function getMessageSessionLabel(message: Pick<MessageEntry, 'session_title'>) {
  return message.session_title || 'Untitled session'
}

function getTokenTotal(tokens?: TokenStats) {
  if (!tokens) {
    return 0
  }

  return tokens.input + tokens.output + tokens.reasoning + tokens.cache.read + tokens.cache.write
}

function PaginationButton({
  disabled,
  onClick,
  children,
}: {
  disabled: boolean
  onClick: () => void
  children: string
}) {
  return (
    <Button variant="ghost" disabled={disabled} onClick={onClick} className="min-w-24">
      {children}
    </Button>
  )
}

function LoadingRows() {
  return Array.from({ length: 5 }).map((_, index) => (
    <TableRow key={index} className="bg-card/40">
      <TableCell className="px-4 py-3">
        <Skeleton className="h-5 w-16" />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Skeleton className="h-5 w-24" />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Skeleton className="h-5 w-32" />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Skeleton className="h-5 w-24" />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Skeleton className="h-5 w-16" />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Skeleton className="h-5 w-20" />
      </TableCell>
      <TableCell className="px-4 py-3">
        <Skeleton className="h-8 w-16" />
      </TableCell>
    </TableRow>
  ))
}

function EmptyRow({ colSpan, copy }: { colSpan: number; copy: string }) {
  return (
    <TableRow className="bg-card/40 hover:bg-card/40">
      <TableCell colSpan={colSpan} className="px-4 py-8 text-center text-sm text-muted-foreground">
        {copy}
      </TableCell>
    </TableRow>
  )
}

export function RequestsHistoryTable({
  data,
  error,
  loading,
  page,
  period: _period,
  sortState,
  onOpenMessage,
  onPageChange,
  onSortChange,
  onRetry,
}: {
  data: MessageList | null
  error: string | null
  loading: boolean
  page: number
  period: DailyPeriod
  sortState: SortState<RequestsSortKey> | null
  onOpenMessage: (messageId: string, trigger: HTMLElement) => void
  onPageChange: (page: number) => void
  onSortChange: (key: RequestsSortKey) => void
  onRetry: () => void
}) {
  const currentPage = data?.page ?? page
  const total = data?.total ?? 0
  const pageSize = data?.page_size ?? FALLBACK_PAGE_SIZE
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const firstVisible = total === 0 ? 0 : (currentPage - 1) * pageSize + 1
  const lastVisible = total === 0 ? 0 : firstVisible + (data?.messages.length ?? 0) - 1

  const isSortedBy = (key: RequestsSortKey) => sortState?.key === key
  const getSortDirection = (key: RequestsSortKey) => (sortState?.key === key ? sortState.direction : undefined)

  return (
    <Card>
      <CardHeader>
        <CardDescription>Always-on drill-down</CardDescription>
        <CardTitle>Requests history</CardTitle>
      </CardHeader>

      <CardContent className="space-y-4">
        {error ? (
          <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="font-medium text-foreground">Requests history failed to load</div>
              <div className="text-sm opacity-90">{error}</div>
            </div>
            <Button variant="ghost" onClick={onRetry}>
              Retry requests
            </Button>
          </Alert>
        ) : null}

        <div className="space-y-3">
          {/* Desktop table */}
          <div className="hidden md:block">
            <Table className="min-w-[42rem] overflow-hidden rounded-2xl border border-border/70 bg-card/40">
              <TableHeader className="bg-panel/75">
                <TableRow className="border-b border-border/70 hover:bg-transparent">
                  <TableHead className="w-[5rem]" aria-sort={getAriaSort(sortState, 'role')}>
                    <SortButton active={isSortedBy('role')} direction={getSortDirection('role')} label="Role" onClick={() => onSortChange('role')} />
                  </TableHead>
                  <TableHead className="w-[9rem]" aria-sort={getAriaSort(sortState, 'time')}>
                    <SortButton active={isSortedBy('time')} direction={getSortDirection('time')} label="Time" onClick={() => onSortChange('time')} />
                  </TableHead>
                  <TableHead className="min-w-[12rem] px-4 py-3 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Session</TableHead>
                  <TableHead className="w-[10rem]" aria-sort={getAriaSort(sortState, 'model')}>
                    <SortButton active={isSortedBy('model')} direction={getSortDirection('model')} label="Model" onClick={() => onSortChange('model')} />
                  </TableHead>
                  <TableHead className="w-[6rem]" aria-sort={getAriaSort(sortState, 'cost')}>
                    <SortButton active={isSortedBy('cost')} direction={getSortDirection('cost')} label="Cost" onClick={() => onSortChange('cost')} />
                  </TableHead>
                  <TableHead className="w-[7rem]" aria-sort={getAriaSort(sortState, 'tokens')}>
                    <SortButton active={isSortedBy('tokens')} direction={getSortDirection('tokens')} label="Tokens" onClick={() => onSortChange('tokens')} />
                  </TableHead>
                  <TableHead className="w-[5rem] px-4 py-3 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Open</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody className="divide-y divide-border/60">
                {loading && !data ? <LoadingRows /> : null}

                {!loading && (data?.messages.length ?? 0) === 0 ? (
                  <EmptyRow colSpan={7} copy="No requests recorded for this Daily window yet." />
                ) : null}

                {data?.messages.map((message) => {
                  const tokenTotal = getTokenTotal(message.tokens)

                  return (
                    <TableRow
                      key={message.id}
                      tabIndex={0}
                      role="button"
                      onClick={(event) => onOpenMessage(message.id, event.currentTarget)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          onOpenMessage(message.id, event.currentTarget)
                        }
                      }}
                      aria-label={`View details for ${getMessageSessionLabel(message)} at ${formatDateTime(message.time_created)}`}
                      className="bg-card/40 cursor-pointer hover:bg-white/4 focus-visible:bg-white/4"
                    >
                      <TableCell className="w-[5rem] px-4 py-3">
                        <Badge tone={getRoleTone(message.role)} className="text-[10px] uppercase tracking-[0.16em]">
                          {message.role || 'unknown'}
                        </Badge>
                      </TableCell>

                      <TableCell className="w-[9rem] px-4 py-3">
                        <span className="font-mono text-sm text-foreground">{formatDateTime(message.time_created)}</span>
                      </TableCell>

                      <TableCell className="min-w-[12rem] px-4 py-3">
                        <div className="truncate font-medium text-foreground">{getMessageSessionLabel(message)}</div>
                      </TableCell>

                      <TableCell className="w-[10rem] px-4 py-3">
                        {message.model_id ? (
                          <span className="font-mono text-sm text-foreground">{message.model_id}</span>
                        ) : (
                          <span className="text-sm text-muted-foreground">—</span>
                        )}
                      </TableCell>

                      <TableCell className="w-[6rem] px-4 py-3 text-right">
                        <span className="font-mono text-sm text-foreground">{formatCompactCurrency(message.cost)}</span>
                      </TableCell>

                      <TableCell className="w-[7rem] px-4 py-3 text-right">
                        {tokenTotal > 0 ? (
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="cursor-default font-mono text-sm text-foreground transition-opacity hover:opacity-80">
                                  {formatTokenCount(tokenTotal)}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="font-mono">
                                <p>{formatInteger(tokenTotal)} tokens</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        ) : (
                          <span className="text-sm text-muted-foreground">&mdash;</span>
                        )}
                      </TableCell>

                      <TableCell className="w-[5rem] px-4 py-3">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="w-full justify-center text-accent"
                          onClick={(event) => {
                            event.stopPropagation()
                            onOpenMessage(message.id, event.currentTarget)
                          }}
                          aria-label={`Open request detail for ${getMessageSessionLabel(message)}`}
                        >
                          Open
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>

          {/* Mobile cards */}
          <div className="space-y-3 md:hidden">
            {loading && !data
              ? Array.from({ length: 4 }).map((_, index) => (
                  <div key={index} className="rounded-2xl border border-border/70 bg-panel/65 p-4">
                    <Skeleton className="h-5 w-32" />
                    <Skeleton className="mt-3 h-4 w-48" />
                  </div>
                ))
              : null}

            {!loading && (data?.messages.length ?? 0) === 0 ? (
              <div className="rounded-2xl border border-border/70 bg-panel/65 px-4 py-5 text-sm text-muted-foreground">
                No requests recorded for this Daily window yet.
              </div>
            ) : null}

            {data?.messages.map((message) => {
              const tokenTotal = getTokenTotal(message.tokens)

              return (
                <button
                  key={message.id}
                  type="button"
                  onClick={(event) => onOpenMessage(message.id, event.currentTarget)}
                  className="w-full rounded-2xl border border-border/70 bg-panel/65 p-4 text-left transition-colors hover:bg-panel/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70"
                  aria-label={`View details for ${getMessageSessionLabel(message)}`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex items-center gap-2">
                      <Badge tone={getRoleTone(message.role)} className="text-[10px] uppercase tracking-[0.16em]">
                        {message.role || 'unknown'}
                      </Badge>
                      <span className="truncate font-medium text-foreground">{getMessageSessionLabel(message)}</span>
                    </div>

<div className="text-right">
                       <div className="font-mono text-sm text-foreground">{formatCompactCurrency(message.cost)}</div>
                       {tokenTotal > 0 ? (
                         <TooltipProvider>
                           <Tooltip>
                             <TooltipTrigger asChild>
                               <div className="mt-0.5 cursor-default font-mono text-[11px] text-muted-foreground transition-opacity hover:opacity-80">
                                 {formatTokenCount(tokenTotal)}
                               </div>
                             </TooltipTrigger>
                             <TooltipContent side="top" className="font-mono">
                               <p>{formatInteger(tokenTotal)} tokens</p>
                             </TooltipContent>
                           </Tooltip>
                         </TooltipProvider>
                       ) : null}
                     </div>
                  </div>

                  <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span className="font-mono text-foreground">{formatDateTime(message.time_created)}</span>
                    {message.model_id ? (
                      <>
                        <span aria-hidden="true">·</span>
                        <span className="font-mono text-foreground">{message.model_id}</span>
                      </>
                    ) : null}
                  </div>
                </button>
              )
            })}
          </div>

          {/* Pagination footer */}
          <div className="flex flex-col gap-3 rounded-2xl border border-border/70 bg-panel/50 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="text-sm text-muted-foreground">
              <span className="font-medium text-foreground">
                Showing {firstVisible}–{lastVisible} of {formatInteger(total)} requests
              </span>
            </div>

            <div className="flex flex-wrap gap-2">
              <PaginationButton disabled={currentPage <= 1 || loading} onClick={() => onPageChange(Math.max(1, currentPage - 1))}>
                Previous
              </PaginationButton>
              <PaginationButton disabled={currentPage >= totalPages || loading} onClick={() => onPageChange(currentPage + 1)}>
                Next
              </PaginationButton>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
