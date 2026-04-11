import { Progress } from '../ui/progress'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import {
  formatModelsMetricShare,
  getModelsMetricMeta,
  getModelsMetricShare,
  getModelsMetricValue,
  getProgressValue,
  type EnrichedModelRow,
  type ModelsMetric,
} from './models-metrics'
import { getModelLabel, getProviderLabel } from './models-table'
import { formatCompactCurrency, formatCompactInteger, formatCurrency, formatInteger, formatTokenCount } from '../../lib/format'

export interface ModelsRowCardProps {
  row: EnrichedModelRow
  metric: ModelsMetric
  totalMetricValue: number
}

export function ModelsRowCard({ row, metric, totalMetricValue }: ModelsRowCardProps) {
  const metricMeta = getModelsMetricMeta(metric)
  const metricValue = getModelsMetricValue(row, metric)
  const metricShare = getModelsMetricShare(row, metric, totalMetricValue)
  const progressValue = getProgressValue(metricShare, metricValue > 0)

  return (
    <div className="rounded-2xl border border-border/70 bg-panel/65 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate font-medium text-foreground">{getModelLabel(row)}</div>
          <div className="mt-1 text-xs uppercase tracking-[0.14em] text-muted-foreground">{getProviderLabel(row)}</div>
        </div>
        <div className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</div>
      </div>

      <div className="mt-3 flex items-center gap-3">
        <Progress value={progressValue} className="flex-1" />
        <span className="font-mono text-xs text-muted-foreground">{formatModelsMetricShare(metricShare)}</span>
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
        <div className="rounded-lg bg-background/40 px-2.5 py-2">
          <div className="uppercase tracking-[0.14em]">Sessions</div>
          <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</div>
        </div>
        <div className="rounded-lg bg-background/40 px-2.5 py-2">
          <div className="uppercase tracking-[0.14em]">Messages</div>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="mt-1 cursor-default font-mono text-sm text-foreground transition-opacity hover:opacity-80">
                  {formatCompactInteger(row.messages)}
                </div>
              </TooltipTrigger>
              <TooltipContent side="top" className="max-w-xs text-center">
                <p>Assistant message count. One API request may produce multiple messages.</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <div className="rounded-lg bg-background/40 px-2.5 py-2">
          <div className="uppercase tracking-[0.14em]">Input</div>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="mt-1 cursor-default font-mono text-sm text-foreground transition-opacity hover:opacity-80">
                  {formatTokenCount(row.tokens.input)}
                </div>
              </TooltipTrigger>
              <TooltipContent side="top" className="font-mono">
                <p>{formatInteger(row.tokens.input)}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <div className="rounded-lg bg-background/40 px-2.5 py-2">
          <div className="uppercase tracking-[0.14em]">Output</div>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="mt-1 cursor-default font-mono text-sm text-foreground transition-opacity hover:opacity-80">
                  {formatTokenCount(row.tokens.output)}
                </div>
              </TooltipTrigger>
              <TooltipContent side="top" className="font-mono">
                <p>{formatInteger(row.tokens.output)}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <div className="rounded-lg bg-background/40 px-2.5 py-2">
          <div className="uppercase tracking-[0.14em]">Avg / msg</div>
          <div className="mt-1 font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerMessage)}</div>
        </div>
        <div className="rounded-lg bg-background/40 px-2.5 py-2">
          <div className="uppercase tracking-[0.14em]">{metricMeta.progressLabel}</div>
          <div className="mt-1 font-mono text-sm text-foreground">{formatModelsMetricShare(metricShare)}</div>
        </div>
      </div>
    </div>
  )
}