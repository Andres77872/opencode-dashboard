/* Source view: the redacted original config file text, syntax-highlighted by
   lib/syntax.ts tokens. Line-numbered with a sticky gutter; an active filter
   highlights matching lines (never hides them — raw text stays byte-faithful
   with true line numbers). */
import { useMemo } from 'react'
import { Card } from '../vael'
import { CopyButton } from './copy-button'
import { highlightSource, type SyntaxToken, type SyntaxTokenType } from '../../lib/syntax'
import type { ConfigDocument } from '../../types/config'

const TOKEN_COLORS: Record<SyntaxTokenType, string> = {
  key: 'var(--fg-secondary)',
  string: 'var(--cat-4)',
  number: 'var(--cat-1)',
  boolean: 'var(--cat-5)',
  null: 'var(--fg-faint)',
  punct: 'var(--fg-faint)',
  comment: 'var(--fg-faint)',
  section: 'var(--accent)',
  redacted: 'var(--warning)',
  plain: 'var(--fg-secondary)',
}

const FORMAT_LABELS: Record<ConfigDocument['format'], string> = {
  json: 'JSON',
  toml: 'TOML',
  yaml: 'YAML',
}

export interface SourcePaneProps {
  doc: ConfigDocument
  /** Normalized filter query; '' when inactive. */
  searchQuery: string
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

export function SourcePane({ doc, searchQuery, copiedId, onCopy }: SourcePaneProps) {
  const tokenLines = useMemo(() => highlightSource(doc.raw, doc.format), [doc.raw, doc.format])
  const rawLines = useMemo(() => doc.raw.split('\n'), [doc.raw])
  const matches = useMemo(() => {
    if (!searchQuery) return null
    return rawLines.map((line) => line.toLowerCase().includes(searchQuery))
  }, [rawLines, searchQuery])
  const matchCount = useMemo(() => (matches ? matches.filter(Boolean).length : 0), [matches])

  const subtitle = doc.rawSynthesized
    ? 'Re-serialized from the structured payload (original file text unavailable).'
    : 'Original redacted file text.'

  return (
    <Card
      title={`${FORMAT_LABELS[doc.format]} source`}
      subtitle={
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <span>{subtitle}</span>
          {matches && (
            <span style={{ color: matchCount > 0 ? 'var(--accent)' : 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
              {matchCount} matching line{matchCount === 1 ? '' : 's'}
            </span>
          )}
        </span>
      }
      action={<CopyButton copyId="source-copy" copiedId={copiedId} label="Copy file" value={doc.raw} onCopy={onCopy} />}
      pad={0}
    >
      <div
        style={{
          overflow: 'auto',
          background: 'var(--ink-850)',
          maxHeight: 'calc(100vh - 280px)',
          borderRadius: '0 0 var(--radius-xl) var(--radius-xl)',
        }}
      >
        <div style={{ font: '400 12.5px/1.75 var(--font-mono)', minWidth: 'max-content', padding: '6px 0' }}>
          {tokenLines.map((tokens, i) => (
            <div key={i} style={{ display: 'flex', background: matches?.[i] ? 'var(--accent-soft)' : 'transparent' }}>
              <span
                style={{
                  width: 48,
                  flexShrink: 0,
                  textAlign: 'right',
                  paddingRight: 14,
                  color: 'var(--fg-faint)',
                  userSelect: 'none',
                  borderRight: '1px solid var(--border-subtle)',
                  background: 'var(--ink-850)',
                  position: 'sticky',
                  left: 0,
                  fontVariantNumeric: 'tabular-nums',
                }}
              >
                {i + 1}
              </span>
              <code style={{ paddingLeft: 16, paddingRight: 16, whiteSpace: 'pre' }}>
                {tokens.length === 0 ? ' ' : tokens.map((token, j) => <TokenSpan key={j} token={token} />)}
              </code>
            </div>
          ))}
        </div>
      </div>
    </Card>
  )
}

function TokenSpan({ token }: { token: SyntaxToken }) {
  const isComment = token.type === 'comment'
  const isRedacted = token.type === 'redacted'
  return (
    <span
      style={{
        color: TOKEN_COLORS[token.type],
        fontStyle: isComment ? 'italic' : 'normal',
        background: isRedacted ? 'var(--warning-soft)' : 'transparent',
        borderRadius: isRedacted ? 2 : 0,
      }}
    >
      {token.text}
    </span>
  )
}
