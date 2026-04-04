import { formatCurrency, formatDateTime, formatTokenCount } from '../../lib/format'
import { cn } from '../../lib/utils'
import type { SessionMessage } from '../../types/api'
import { Badge } from '../ui/badge'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'

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

export function getTokenTotal(message: SessionMessage) {
  if (!message.tokens) return 0
  return message.tokens.input + message.tokens.output + message.tokens.reasoning + message.tokens.cache.read + message.tokens.cache.write
}

export function SessionMessageRow({
  message,
  previousMessage,
  isHighestCost = false,
  isHighestTokens = false,
}: {
  message: SessionMessage
  previousMessage?: SessionMessage
  isHighestCost?: boolean
  isHighestTokens?: boolean
}) {
  const tokenTotal = getTokenTotal(message)
  const cost = message.cost ?? 0
  const isHighlight = isHighestCost || isHighestTokens

  const sameMeta = previousMessage?.model_id === message.model_id 
    && previousMessage?.agent === message.agent
    && previousMessage?.provider_id === message.provider_id
  const metaParts = [
    !sameMeta && message.model_id ? message.model_id : null,
    !sameMeta && message.provider_id ? message.provider_id : null,
    !sameMeta && message.agent ? message.agent : null,
  ].filter(Boolean)

  return (
    <TooltipProvider>
      <div
        className={cn(
          'px-3 py-2',
          isHighlight && 'border-l-2 border-l-accent/60 bg-accent/[0.03]',
        )}
      >
        {/* Line 1: identity */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <Badge tone={getRoleTone(message.role)} className="text-[10px] uppercase">
            {message.role || 'unknown'}
          </Badge>

          <Tooltip>
            <TooltipTrigger asChild>
              <span className="cursor-default font-mono text-xs text-muted-foreground">
                {formatDateTime(message.time_created)}
              </span>
            </TooltipTrigger>
            <TooltipContent>
              <p className="font-mono text-xs">{message.id}</p>
            </TooltipContent>
          </Tooltip>

          {metaParts.length > 0 ? (
            <span className="max-w-[28ch] truncate font-mono text-xs text-muted-foreground/60">
              {metaParts.join(' · ')}
            </span>
          ) : null}

          <div className="flex flex-1 items-center justify-end gap-1.5">
            {isHighestCost ? <Badge tone="accent" className="text-[9px]">highest cost</Badge> : null}
            {isHighestTokens ? <Badge tone="warning" className="text-[9px]">most tokens</Badge> : null}
          </div>
        </div>

        {/* Line 2: metrics — always visible */}
        {(tokenTotal > 0 || cost > 0) ? (
          <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-0.5 pl-1 font-mono text-[11px]">
            <span>
              <span className="text-muted-foreground/50">cost </span>
              <span className="text-foreground/80">{formatCurrency(cost)}</span>
            </span>
            <span>
              <span className="text-muted-foreground/50">input </span>
              <span className="text-foreground/80">{formatTokenCount(message.tokens?.input ?? 0)}</span>
            </span>
            <span>
              <span className="text-muted-foreground/50">output </span>
              <span className="text-foreground/80">{formatTokenCount(message.tokens?.output ?? 0)}</span>
            </span>
            {(message.tokens?.cache.read ?? 0) > 0 ? (
              <span>
                <span className="text-muted-foreground/50">cache·r </span>
                <span className="text-foreground/80">{formatTokenCount(message.tokens!.cache.read)}</span>
              </span>
            ) : null}
            {(message.tokens?.cache.write ?? 0) > 0 ? (
              <span>
                <span className="text-muted-foreground/50">cache·w </span>
                <span className="text-foreground/80">{formatTokenCount(message.tokens!.cache.write)}</span>
              </span>
            ) : null}
            {(message.tokens?.reasoning ?? 0) > 0 ? (
              <span>
                <span className="text-muted-foreground/50">reasoning </span>
                <span className="text-foreground/80">{formatTokenCount(message.tokens!.reasoning)}</span>
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </TooltipProvider>
  )
}
