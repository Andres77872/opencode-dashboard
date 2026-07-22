import type { RequestAccounting, TraceCoverage, UsageStatus } from '../types/api.ts'
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
  return `${formatInteger(accounting.usage_recorded)} usage records, ${formatInteger(accounting.usage_recovered)} recovered from step-end evidence, and ${formatInteger(accounting.usage_unavailable)} requests without persisted usage. Trace coverage: ${traceCoverageLabel(accounting.trace_coverage)}. Missing usage means tokens and estimated cost are unknown, not zero.`
}

export function usageStatusLabel(status?: UsageStatus): string {
  switch (status) {
    case 'recorded':
      return 'Usage recorded'
    case 'recovered':
      return 'Usage recovered'
    case 'unavailable':
      return 'Usage unavailable'
    default:
      return 'Usage unknown'
  }
}
