import type { RequestAccounting, TraceCoverage, UsageStatus, UsageUnavailableReason } from '../types/api.ts'
import { formatInteger } from './format.ts'

export function traceCoverageLabel(coverage: TraceCoverage): string {
  switch (coverage) {
    case 'successful_only':
      return 'successful only'
    default:
      return coverage
  }
}

export function isIncompleteRequestAccounting(accounting: RequestAccounting): boolean {
  return accounting.usage_unavailable > 0 || accounting.trace_coverage !== 'complete'
}

export function requestAccountingDisclosure(accounting: RequestAccounting): string {
  const reasons = accounting.usage_unavailable_reasons
  const reasonParts = [
    reasons.cancelled > 0 ? `${formatInteger(reasons.cancelled)} cancelled` : '',
    reasons.interrupted > 0 ? `${formatInteger(reasons.interrupted)} ${reasons.interrupted === 1 ? 'log ended open' : 'logs ended open'}` : '',
    reasons.failed > 0 ? `${formatInteger(reasons.failed)} failed or retried` : '',
    reasons.unknown > 0 ? `${formatInteger(reasons.unknown)} unexplained` : '',
  ].filter(Boolean)
  const unavailable = accounting.usage_unavailable > 0
    ? `${formatInteger(accounting.usage_unavailable)} observed ${accounting.usage_unavailable === 1 ? 'request has' : 'requests have'} no persisted usage${reasonParts.length > 0 ? ` (${reasonParts.join(', ')})` : ''}.`
    : 'Every observed request has persisted usage.'
  const recovery = accounting.usage_recovered > 0
    ? `${formatInteger(accounting.usage_recovered)} ${accounting.usage_recovered === 1 ? 'request relies' : 'requests rely'} only on step-end recovery.`
    : 'No requests rely only on step-end recovery.'
  return `${formatInteger(accounting.usage_recorded)} requests have canonical usage. ${unavailable} ${recovery} Trace coverage: ${traceCoverageLabel(accounting.trace_coverage)}. Missing usage means tokens and estimated cost are unknown, not zero.`
}

export function usageUnavailableReasonLabel(reason?: UsageUnavailableReason): string {
  switch (reason) {
    case 'cancelled':
      return 'Usage unavailable · cancelled'
    case 'interrupted':
      return 'Usage unavailable · log ended open'
    case 'failed':
      return 'Usage unavailable · failed/retried'
    default:
      return 'Usage unavailable · unexplained'
  }
}

export function usageUnavailableReasonDetail(reason?: UsageUnavailableReason): string {
  switch (reason) {
    case 'cancelled':
      return 'Kimi persisted a cancellation for this outbound request but no token usage evidence.'
    case 'interrupted':
      return 'Kimi’s persisted log ended while this outbound request was still open; this does not prove that the process crashed.'
    case 'failed':
      return 'A later request for the same turn step superseded this attempt without persisted token usage.'
    default:
      return 'Kimi persisted this outbound request but no terminal usage evidence explains the gap.'
  }
}

export function usageStatusLabel(status?: UsageStatus, reason?: UsageUnavailableReason): string {
  switch (status) {
    case 'recorded':
      return 'Usage recorded'
    case 'recovered':
      return 'Usage recovered'
    case 'unavailable':
      return usageUnavailableReasonLabel(reason)
    default:
      return 'Usage unknown'
  }
}
