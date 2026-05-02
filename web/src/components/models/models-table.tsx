import { Progress } from '../ui/progress'
import { SortButton } from '../ui/sort-button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import {
  formatModelsMetricShare,
  getModelsMetricShare,
  getModelsMetricValue,
  getProgressValue,
  type EnrichedModelRow,
  type ModelsMetric,
} from './models-metrics'
import type { ModelEntry } from '../../types/api'
import { formatCompactCurrency, formatCompactInteger, formatCurrency, formatInteger, formatTokenCount } from '../../lib/format'
import { getAriaSort, type SortDirection, type SortState } from '../../lib/table-sort'

export type SortKey = 'cost' | 'messages' | 'sessions' | 'model' | 'provider' | 'avgCostPerMessage' | 'percent'

export const DEFAULT_SORT_DIRECTIONS: Record<SortKey, SortDirection> = {
  avgCostPerMessage: 'asc',
  cost: 'desc',
  messages: 'desc',
  model: 'asc',
  percent: 'desc',
  provider: 'asc',
  sessions: 'desc',
}

export const DEFAULT_TABLE_SORT: SortState<SortKey> = {
  key: 'cost',
  direction: 'desc',
}

export function getModelLabel(model: ModelEntry): string {
  return model.model_id || 'Unknown model'
}

export function getProviderLabel(model: ModelEntry): string {
  return model.provider_id || 'Unknown provider'
}

export function compareRows(
  sortKey: SortKey,
  left: EnrichedModelRow,
  right: EnrichedModelRow,
  metric?: ModelsMetric,
): number {
  switch (sortKey) {
    case 'model':
      return getModelLabel(left).localeCompare(getModelLabel(right))
    case 'provider':
      return getProviderLabel(left).localeCompare(getProviderLabel(right))
    case 'sessions':
      return right.sessions - left.sessions
    case 'messages':
      return right.messages - left.messages
    case 'avgCostPerMessage':
      return right.avgCostPerMessage - left.avgCostPerMessage
    case 'percent': {
      if (!metric) return right.cost - left.cost
      const getShare = (row: EnrichedModelRow): number => {
        switch (metric) {
          case 'cost': return row.costShare
          case 'sessions': return row.sessionShare
          case 'messages': return row.messageShare
          case 'tokens': return row.tokenShare
        }
      }
      return getShare(right) - getShare(left)
    }
    case 'cost':
    default:
      return right.cost - left.cost
  }
}

export interface ModelsTableProps {
  rows: EnrichedModelRow[]
  metric: ModelsMetric
  totalMetricValue: number
  sortState: SortState<SortKey> | null
  onSortChange: (key: SortKey) => void
}

export function ModelsTable({ rows, metric, totalMetricValue, sortState, onSortChange }: ModelsTableProps) {
  const isSortedBy = (key: SortKey) => sortState?.key === key
  const getSortDirection = (key: SortKey) => (sortState?.key === key ? sortState.direction : undefined)

  return (
    <Table className="overflow-hidden rounded-2xl border border-border/70">
      <TableHeader className="bg-panel/75">
        <TableRow className="border-b border-border/70 hover:bg-transparent">
          <TableHead className="min-w-[14rem]" aria-sort={getAriaSort(sortState, 'model')}>
            <SortButton
              active={isSortedBy('model')}
              direction={getSortDirection('model')}
              label="Model"
              onClick={() => onSortChange('model')}
            />
          </TableHead>
          <TableHead className="w-[9rem]" aria-sort={getAriaSort(sortState, 'provider')}>
            <SortButton
              active={isSortedBy('provider')}
              direction={getSortDirection('provider')}
              label="Provider"
              onClick={() => onSortChange('provider')}
            />
          </TableHead>
          <TableHead className="w-[5.5rem]" aria-sort={getAriaSort(sortState, 'sessions')}>
            <SortButton
              active={isSortedBy('sessions')}
              direction={getSortDirection('sessions')}
              label="Sessions"
              onClick={() => onSortChange('sessions')}
            />
          </TableHead>
          <TableHead className="w-[6rem]" aria-sort={getAriaSort(sortState, 'messages')}>
            <SortButton
              active={isSortedBy('messages')}
              direction={getSortDirection('messages')}
              label="Messages"
              onClick={() => onSortChange('messages')}
            />
          </TableHead>
          <TableHead className="w-[9rem]" aria-sort={getAriaSort(sortState, 'percent')}>
            <SortButton
              active={isSortedBy('percent')}
              direction={getSortDirection('percent')}
              label="Share"
              onClick={() => onSortChange('percent')}
            />
          </TableHead>
          <TableHead className="w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Input
          </TableHead>
          <TableHead className="w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Output
          </TableHead>
          <TableHead className="hidden md:table-cell w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Reasoning
          </TableHead>
          <TableHead className="hidden lg:table-cell w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Cache Read
          </TableHead>
          <TableHead className="hidden lg:table-cell w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Cache Write
          </TableHead>
          <TableHead className="hidden md:table-cell lg:hidden w-[7rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Cache
          </TableHead>
          <TableHead className="w-[7rem]" aria-sort={getAriaSort(sortState, 'cost')}>
            <SortButton
              active={isSortedBy('cost')}
              direction={getSortDirection('cost')}
              label="Cost"
              onClick={() => onSortChange('cost')}
            />
          </TableHead>
          <TableHead className="w-[8rem]" aria-sort={getAriaSort(sortState, 'avgCostPerMessage')}>
            <SortButton
              active={isSortedBy('avgCostPerMessage')}
              direction={getSortDirection('avgCostPerMessage')}
              label="Avg / msg"
              onClick={() => onSortChange('avgCostPerMessage')}
            />
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => {
          const metricValue = getModelsMetricValue(row, metric)
          const metricShare = getModelsMetricShare(row, metric, totalMetricValue)
          const progressValue = getProgressValue(metricShare, metricValue > 0)

          return (
            <TableRow key={`${row.provider_id}:${row.model_id}`} className="bg-card/40 hover:bg-white/4">
              <TableCell className="min-w-[14rem]">
                <div className="truncate font-medium text-foreground">{getModelLabel(row)}</div>
              </TableCell>
              <TableCell className="truncate font-mono text-sm text-muted-foreground">{getProviderLabel(row)}</TableCell>
              <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</TableCell>
              <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.messages)}</TableCell>
              <TableCell className="w-[9rem]">
                <div className="space-y-2">
                  <Progress value={progressValue} className="flex-1" />
                  <span className="font-mono text-xs text-muted-foreground">{formatModelsMetricShare(metricShare)}</span>
                </div>
              </TableCell>
              <TableCell className="font-mono text-sm text-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default transition-opacity hover:opacity-80">{formatTokenCount(row.tokens.input)}</span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(row.tokens.input)}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableCell>
              <TableCell className="font-mono text-sm text-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default transition-opacity hover:opacity-80">{formatTokenCount(row.tokens.output)}</span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(row.tokens.output)}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableCell>
              <TableCell className="hidden md:table-cell font-mono text-sm text-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default transition-opacity hover:opacity-80">{formatTokenCount(row.tokens.reasoning)}</span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(row.tokens.reasoning)}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableCell>
              <TableCell className="hidden lg:table-cell font-mono text-sm text-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default transition-opacity hover:opacity-80">{formatTokenCount(row.tokens.cache.read)}</span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(row.tokens.cache.read)}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableCell>
              <TableCell className="hidden lg:table-cell font-mono text-sm text-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default transition-opacity hover:opacity-80">{formatTokenCount(row.tokens.cache.write)}</span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(row.tokens.cache.write)}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableCell>
              <TableCell className="hidden md:table-cell lg:hidden font-mono text-sm text-foreground">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="cursor-default transition-opacity hover:opacity-80">{formatTokenCount(row.tokens.cache.read + row.tokens.cache.write)}</span>
                    </TooltipTrigger>
                    <TooltipContent side="top" className="font-mono">
                      <p>{formatInteger(row.tokens.cache.read + row.tokens.cache.write)}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </TableCell>
              <TableCell className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</TableCell>
              <TableCell className="font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerMessage)}</TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}