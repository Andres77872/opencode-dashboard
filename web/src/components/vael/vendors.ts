import type { SourceID } from '../../types/api'

/**
 * Per-source ("vendor") presentation metadata: the monogram + brand accent used
 * by VendorChip, SourceStack, chart series, and legends. Brand colors come from
 * the --vendor-* tokens; the monogram text is rendered on a solid chip.
 */
export interface VendorMeta {
  id: SourceID
  name: string
  short: string
  mono: string
  color: string
}

export const VENDORS: Record<SourceID, VendorMeta> = {
  opencode: { id: 'opencode', name: 'OpenCode', short: 'OpenCode', mono: 'oc', color: 'var(--vendor-opencode)' },
  claude_code: { id: 'claude_code', name: 'Claude Code', short: 'Claude', mono: 'C', color: 'var(--vendor-claude)' },
  codex: { id: 'codex', name: 'Codex', short: 'Codex', mono: 'Cx', color: 'var(--vendor-codex)' },
}

export function vendorMeta(id?: SourceID): VendorMeta {
  return (id && VENDORS[id]) || VENDORS.opencode
}
