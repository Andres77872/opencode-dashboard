/**
 * Renders the Mermaid subset parsed by lib/mermaid as real SVG elements.
 *
 * Every label goes through React as a text child, so diagram text can never
 * become markup — the reason this panel draws diagrams itself instead of
 * injecting a renderer's HTML string. Layout is fully computed upstream; this
 * file only maps geometry onto shapes and paints them with design tokens.
 *
 * A `pie` diagram is delegated to the chart artifact: it is a share-of-total
 * chart written in Mermaid syntax, and routing it through the donut keeps the
 * tooltips, the table view, and the palette identical to a ```chart donut.
 */
import { useMemo } from 'react'
import {
  parseMermaid,
  type DiagramNode,
  type FlowchartDiagram,
  type PieDiagram,
  type SequenceDiagram,
} from '../../lib/mermaid'
import type { ChartSpec } from '../../lib/chart-spec'
import { ArtifactFallback, ArtifactPending, ArtifactShell } from './artifact'
import { ChartFigure } from './chart-artifact'

/* ------------------------------------------------------------------ *
 * Flowchart
 * ------------------------------------------------------------------ */

/** The outline of one node shape, as an SVG element. */
function NodeShape({ node }: { node: DiagramNode }) {
  const { x, y, w, h } = node
  const common = {
    fill: 'var(--ink-850)',
    stroke: 'var(--border-strong)',
    strokeWidth: 1,
  }
  switch (node.shape) {
    case 'circle':
      return <circle cx={x} cy={y} r={Math.min(w, h) / 2} {...common} />
    case 'diamond':
      return (
        <polygon
          points={`${x},${y - h / 2} ${x + w / 2},${y} ${x},${y + h / 2} ${x - w / 2},${y}`}
          {...common}
        />
      )
    case 'hexagon':
      return (
        <polygon
          points={`${x - w / 2 + 10},${y - h / 2} ${x + w / 2 - 10},${y - h / 2} ${x + w / 2},${y} ${x + w / 2 - 10},${y + h / 2} ${x - w / 2 + 10},${y + h / 2} ${x - w / 2},${y}`}
          {...common}
        />
      )
    case 'cylinder':
      return (
        <g>
          <rect x={x - w / 2} y={y - h / 2 + 5} width={w} height={h - 10} {...common} />
          <ellipse cx={x} cy={y - h / 2 + 5} rx={w / 2} ry={5} {...common} />
          <ellipse cx={x} cy={y + h / 2 - 5} rx={w / 2} ry={5} fill="var(--ink-850)" stroke="var(--border-strong)" />
        </g>
      )
    case 'stadium':
      return <rect x={x - w / 2} y={y - h / 2} width={w} height={h} rx={h / 2} {...common} />
    case 'round':
      return <rect x={x - w / 2} y={y - h / 2} width={w} height={h} rx={10} {...common} />
    case 'rect':
    default:
      return <rect x={x - w / 2} y={y - h / 2} width={w} height={h} rx={4} {...common} />
  }
}

function FlowchartView({ diagram }: { diagram: FlowchartDiagram }) {
  return (
    <svg
      className="md-diagram-svg"
      viewBox={`0 0 ${diagram.width} ${diagram.height}`}
      width="100%"
      height={diagram.height}
      role="img"
      aria-label={`Flowchart with ${diagram.nodes.length} nodes and ${diagram.edges.length} links`}
      preserveAspectRatio="xMidYMid meet"
    >
      {diagram.edges.map((edge, index) => (
        <g key={index}>
          <path
            d={edge.path}
            fill="none"
            stroke="var(--fg-faint)"
            strokeWidth={edge.thick ? 2.5 : 1.5}
            strokeDasharray={edge.dashed ? '4 3' : undefined}
          />
          {edge.head === 'arrow' && <polygon points={edge.headPoints} fill="var(--fg-faint)" />}
          {edge.label !== null && (
            <g>
              <rect
                x={edge.labelX - edge.labelW / 2}
                y={edge.labelY - 8}
                width={edge.labelW}
                height={16}
                rx={3}
                fill="var(--ink-800)"
              />
              <text
                x={edge.labelX}
                y={edge.labelY + 3.5}
                textAnchor="middle"
                fontFamily="var(--font-ui)"
                fontSize="10"
                fill="var(--fg-muted)"
              >
                {edge.label}
              </text>
            </g>
          )}
        </g>
      ))}
      {diagram.nodes.map((node) => (
        <g key={node.id}>
          <NodeShape node={node} />
          {node.lines.map((line, index) => (
            <text
              key={index}
              x={node.x}
              y={node.y - ((node.lines.length - 1) * 15) / 2 + index * 15 + 4}
              textAnchor="middle"
              fontFamily="var(--font-ui)"
              fontSize="12"
              fill="var(--fg-secondary)"
            >
              {line}
            </text>
          ))}
        </g>
      ))}
    </svg>
  )
}

/* ------------------------------------------------------------------ *
 * Sequence diagram
 * ------------------------------------------------------------------ */

const SELF_CALL_REACH = 30

function SequenceView({ diagram }: { diagram: SequenceDiagram }) {
  return (
    <svg
      className="md-diagram-svg"
      viewBox={`0 0 ${diagram.width} ${diagram.height}`}
      width="100%"
      height={diagram.height}
      role="img"
      aria-label={`Sequence diagram with ${diagram.actors.length} participants and ${diagram.messages.length} messages`}
      preserveAspectRatio="xMidYMid meet"
    >
      {diagram.actors.map((actor) => (
        <g key={actor.id}>
          <line
            x1={actor.x}
            x2={actor.x}
            y1={diagram.lifelineTop}
            y2={diagram.lifelineBottom}
            stroke="var(--border-subtle)"
            strokeDasharray="3 3"
          />
          <rect
            x={actor.x - actor.w / 2}
            y={diagram.lifelineTop - diagram.actorHeight}
            width={actor.w}
            height={diagram.actorHeight}
            rx={4}
            fill="var(--ink-850)"
            stroke="var(--border-strong)"
          />
          <text
            x={actor.x}
            y={diagram.lifelineTop - diagram.actorHeight / 2 + 4}
            textAnchor="middle"
            fontFamily="var(--font-ui)"
            fontSize="11.5"
            fill="var(--fg-secondary)"
          >
            {actor.label}
          </text>
        </g>
      ))}
      {diagram.notes.map((note, index) => (
        <g key={`note-${index}`}>
          <rect x={note.x} y={note.y} width={note.w} height={note.h} rx={3} fill="var(--accent-soft)" stroke="var(--border-accent)" />
          {note.lines.map((line, lineIndex) => (
            <text
              key={lineIndex}
              x={note.x + note.w / 2}
              y={note.y + 16 + lineIndex * 14}
              textAnchor="middle"
              fontFamily="var(--font-ui)"
              fontSize="10.5"
              fill="var(--fg-secondary)"
            >
              {line}
            </text>
          ))}
        </g>
      ))}
      {diagram.messages.map((message, index) => {
        const direction = message.toX >= message.fromX ? 1 : -1
        if (message.selfCall) {
          const right = message.fromX + SELF_CALL_REACH
          return (
            <g key={`message-${index}`}>
              <path
                d={`M${message.fromX} ${message.y} L${right} ${message.y} L${right} ${message.y + 18} L${message.fromX + 4} ${message.y + 18}`}
                fill="none"
                stroke="var(--fg-muted)"
                strokeWidth={1.5}
                strokeDasharray={message.dashed ? '4 3' : undefined}
              />
              <polygon
                points={`${message.fromX},${message.y + 18} ${message.fromX + 7},${message.y + 14.5} ${message.fromX + 7},${message.y + 21.5}`}
                fill="var(--fg-muted)"
              />
              <text x={right + 6} y={message.y + 4} fontFamily="var(--font-ui)" fontSize="10.5" fill="var(--fg-secondary)">
                {message.label}
              </text>
            </g>
          )
        }
        const endX = message.toX - direction * 7
        return (
          <g key={`message-${index}`}>
            <line
              x1={message.fromX}
              x2={endX}
              y1={message.y}
              y2={message.y}
              stroke="var(--fg-muted)"
              strokeWidth={1.5}
              strokeDasharray={message.dashed ? '4 3' : undefined}
            />
            {message.head === 'arrow' && (
              <polygon
                points={`${message.toX},${message.y} ${endX},${message.y - 3.5} ${endX},${message.y + 3.5}`}
                fill="var(--fg-muted)"
              />
            )}
            {message.head === 'circle' && <circle cx={message.toX - direction * 3} cy={message.y} r={3.5} fill="var(--fg-muted)" />}
            {message.head === 'cross' && (
              <path
                d={`M${message.toX - 4} ${message.y - 4} L${message.toX + 4} ${message.y + 4} M${message.toX + 4} ${message.y - 4} L${message.toX - 4} ${message.y + 4}`}
                stroke="var(--fg-muted)"
                strokeWidth={1.5}
              />
            )}
            <text
              x={(message.fromX + message.toX) / 2}
              y={message.y - 6}
              textAnchor="middle"
              fontFamily="var(--font-ui)"
              fontSize="10.5"
              fill="var(--fg-secondary)"
            >
              {message.label}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

/* ------------------------------------------------------------------ *
 * Pie → donut
 * ------------------------------------------------------------------ */

function pieToChartSpec(diagram: PieDiagram): ChartSpec {
  return {
    kind: 'donut',
    stacked: false,
    title: diagram.title,
    unit: 'none',
    labels: diagram.slices.map((slice) => slice.label),
    series: [{ name: '', values: diagram.slices.map((slice) => slice.value) }],
    source: null,
    period: null,
    note: null,
    hasUnknown: false,
  }
}

/* ------------------------------------------------------------------ *
 * Entry point
 * ------------------------------------------------------------------ */

export function DiagramArtifact({ source, info, closed }: { source: string; info: string | null; closed: boolean }) {
  const result = useMemo(() => (closed ? parseMermaid(source) : null), [source, closed])

  if (result === null) return <ArtifactPending label="Diagram" />
  if (!result.ok) {
    return <ArtifactFallback kind="Diagram" error={result.error} hint={result.hint} source={source} lang={info ?? 'mermaid'} />
  }
  if (result.diagram.kind === 'pie') {
    return <ChartFigure spec={pieToChartSpec(result.diagram)} source={source} kind="Diagram" />
  }
  return (
    <ArtifactShell kind="Diagram" title={null} meta={null} source={source} footnotes={result.warnings}>
      <div className="md-diagram-scroll">
        {result.diagram.kind === 'flowchart' ? (
          <FlowchartView diagram={result.diagram} />
        ) : (
          <SequenceView diagram={result.diagram} />
        )}
      </div>
    </ArtifactShell>
  )
}
