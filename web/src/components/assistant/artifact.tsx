/**
 * The shared chrome around an assistant artifact — a chart or a diagram.
 *
 * Three states matter and each has a deliberate shape:
 *
 *  - **pending**: the fence is still streaming. A spec is only meaningful once
 *    its closing fence arrives, so an unfinished block shows a quiet placeholder
 *    rather than a parse error flashing on every delta.
 *  - **rendered**: a titled figure with the source's provenance, an optional
 *    table view, and a copy control that always yields the original fence text.
 *  - **fallback**: the block did not validate. The diagnostic is shown *with*
 *    the raw source, because the numbers the model computed are the valuable
 *    part and a syntax slip must never discard them.
 */
import type { ReactNode } from 'react'
import { CopyButton } from './copy-button'
import { Icon } from '../vael/icon'

export function ArtifactPending({ label }: { label: string }) {
  return (
    <div className="md-artifact md-artifact-pending" aria-live="polite">
      <div className="md-artifact-head">
        <span className="md-artifact-kind">{label}</span>
      </div>
      <div className="md-artifact-pending-body">
        <span className="md-artifact-pending-bar" />
        <span className="md-artifact-pending-bar" />
        <span className="md-artifact-pending-bar" />
      </div>
    </div>
  )
}

export function ArtifactShell({
  kind,
  title,
  meta,
  source,
  actions,
  children,
  footnotes = [],
}: {
  kind: string
  title: string | null
  meta: string | null
  /** The original fence body, so Copy reproduces what the model wrote. */
  source: string
  actions?: ReactNode
  children: ReactNode
  footnotes?: string[]
}) {
  return (
    <figure className="md-artifact">
      <div className="md-artifact-head">
        <div className="md-artifact-heading">
          <span className="md-artifact-kind">{kind}</span>
          {title !== null && <figcaption className="md-artifact-title">{title}</figcaption>}
          {meta !== null && <span className="md-artifact-meta">{meta}</span>}
        </div>
        <div className="md-artifact-actions">
          {actions}
          <CopyButton value={source} label="Copy" className="md-artifact-copy" showLabel={false} />
        </div>
      </div>
      <div className="md-artifact-body">{children}</div>
      {footnotes.length > 0 && (
        <div className="md-artifact-footnotes">
          {footnotes.map((note, index) => (
            <p key={index} className="md-artifact-footnote">
              {note}
            </p>
          ))}
        </div>
      )}
    </figure>
  )
}

/**
 * Shown when a spec cannot be rendered. The message names what is wrong and the
 * hint says how to write it, which is also what the model reads back when the
 * user pastes the panel's output into the next question.
 */
export function ArtifactFallback({
  kind,
  error,
  hint,
  source,
  lang,
}: {
  kind: string
  error: string
  hint: string | null
  source: string
  lang: string
}) {
  return (
    <div className="md-artifact md-artifact-invalid">
      <div className="md-artifact-head">
        <div className="md-artifact-heading">
          <span className="md-artifact-kind">{kind}</span>
          <span className="md-artifact-error">
            <Icon name="alert-triangle" size={12} />
            {error}
          </span>
          {hint !== null && <span className="md-artifact-meta">{hint}</span>}
        </div>
        <div className="md-artifact-actions">
          <CopyButton value={source} label="Copy" className="md-artifact-copy" showLabel={false} />
        </div>
      </div>
      <pre className="md-artifact-source">
        <code>{`\`\`\`${lang}\n${source}\n\`\`\``}</code>
      </pre>
    </div>
  )
}

/** A two-state view switch (chart ⇄ table) sized for the assistant panel. */
export function ArtifactViewToggle({
  showingTable,
  onToggle,
}: {
  showingTable: boolean
  onToggle: () => void
}) {
  const label = showingTable ? 'Show chart' : 'Show data table'
  return (
    <button
      type="button"
      className="md-artifact-toggle"
      onClick={onToggle}
      aria-pressed={showingTable}
      title={label}
      aria-label={label}
    >
      <Icon name={showingTable ? 'bar-chart' : 'file-text'} size={12} />
    </button>
  )
}
