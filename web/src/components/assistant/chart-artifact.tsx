/**
 * Renders a validated ChartSpec as pure SVG/DOM inside an assistant message.
 *
 * Parsing and validation live in lib/chart-spec (unit-tested there); this file
 * is the projection onto marks. The design rules it enforces come from the
 * dashboard's own charting conventions:
 *
 *  - **The palette is not negotiable.** One series is painted in a single hue
 *    (magnitude is a sequential job); several series take the categorical token
 *    order. A spec cannot supply colors, so no model output can produce an
 *    unreadable or inaccessible chart.
 *  - **Unknown is a gap, never a zero.** Null values break lines, skip bars, and
 *    read as "unknown" in the tooltip and the table.
 *  - **Identity is never color-alone.** Two or more series always get a legend,
 *    and every chart has a data-table view behind one toggle.
 *  - **Narrow-panel first.** The assistant panel is ~420px, and its labels are
 *    model ids and project names, so rankings render as horizontal bars with
 *    direct value labels instead of a column chart with unreadable ticks.
 */
import { useMemo, useState } from 'react'
import {
  chartProvenance,
  formatChartTick,
  formatChartValue,
  parseChartSpec,
  stackTotals,
  type ChartSpec,
  type ChartUnit,
} from '../../lib/chart-spec'
import { niceMax, useWidth } from '../vael/chart-utils'
import { Donut } from '../vael/charts'
import { ArtifactFallback, ArtifactPending, ArtifactShell, ArtifactViewToggle } from './artifact'

const SERIES_COLORS = [
  'var(--cat-1)',
  'var(--cat-2)',
  'var(--cat-3)',
  'var(--cat-4)',
  'var(--cat-5)',
  'var(--cat-6)',
  'var(--cat-7)',
  'var(--cat-8)',
]

/** A lone series is a magnitude comparison, so it stays in one hue. */
function seriesColor(index: number, total: number): string {
  if (total <= 1) return SERIES_COLORS[0]
  return SERIES_COLORS[index % SERIES_COLORS.length]
}

const KIND_LABEL: Record<ChartSpec['kind'], string> = {
  bar: 'Chart',
  column: 'Chart',
  line: 'Chart',
  area: 'Chart',
  donut: 'Chart',
  heatmap: 'Chart',
}

/* ------------------------------------------------------------------ *
 * Shared pieces
 * ------------------------------------------------------------------ */

function Legend({ spec }: { spec: ChartSpec }) {
  if (spec.series.length < 2) return null
  return (
    <div className="md-chart-legend">
      {spec.series.map((series, index) => (
        <span key={index} className="md-chart-legend-item">
          <span className="md-chart-swatch" style={{ background: seriesColor(index, spec.series.length) }} />
          {series.name || `Series ${index + 1}`}
        </span>
      ))}
    </div>
  )
}

interface HoverState {
  index: number
  x: number
}

function HoverTooltip({
  spec,
  hover,
  width,
  total,
}: {
  spec: ChartSpec
  hover: HoverState
  width: number
  total: number | null
}) {
  const rows = spec.series.map((series, index) => ({
    name: series.name || 'Value',
    color: seriesColor(index, spec.series.length),
    value: series.values[hover.index],
  }))
  return (
    <div
      className="md-chart-tooltip"
      style={{ left: Math.min(Math.max(0, hover.x - 70), Math.max(0, width - 148)) }}
      role="presentation"
    >
      <div className="md-chart-tooltip-head">{spec.labels[hover.index]}</div>
      {rows.map((row, index) => (
        <div key={index} className="md-chart-tooltip-row">
          <span className="md-chart-swatch" style={{ background: row.color }} />
          <span className="md-chart-tooltip-name">{row.name}</span>
          <span className={`md-chart-tooltip-value${row.value === null ? ' unknown' : ''}`}>
            {formatChartValue(row.value, spec.unit)}
          </span>
        </div>
      ))}
      {total !== null && spec.series.length > 1 && (
        <div className="md-chart-tooltip-row total">
          <span className="md-chart-swatch" style={{ background: 'transparent' }} />
          <span className="md-chart-tooltip-name">Total</span>
          <span className="md-chart-tooltip-value">{formatChartValue(total, spec.unit)}</span>
        </div>
      )}
    </div>
  )
}

/** Axis maximum over the values that will actually be drawn. */
function axisMax(spec: ChartSpec): number {
  const values = spec.stacked
    ? stackTotals(spec).filter((value): value is number => value !== null)
    : spec.series.flatMap((series) => series.values.filter((value): value is number => value !== null))
  return niceMax(Math.max(1, ...values))
}

function tickValues(max: number, count = 4): number[] {
  return Array.from({ length: count + 1 }, (_, index) => (max / count) * index)
}

/** Thins x labels so ticks never collide in a narrow panel. */
function showLabelAt(index: number, count: number, budget: number): boolean {
  if (count <= budget) return true
  return index % Math.ceil(count / budget) === 0
}

/* ------------------------------------------------------------------ *
 * Horizontal bars — the default for rankings
 * ------------------------------------------------------------------ */

function BarRows({ spec }: { spec: ChartSpec }) {
  const totals = spec.stacked ? stackTotals(spec) : null
  const max = axisMax(spec)
  return (
    <div className="md-chart-rows">
      {spec.labels.map((label, index) => {
        const rowTotal = totals ? totals[index] : null
        const known = spec.series.some((series) => series.values[index] !== null)
        return (
          <div key={index} className="md-chart-row">
            <span className="md-chart-row-label" title={label}>
              {label}
            </span>
            <span className="md-chart-track">
              {!known && <span className="md-chart-unknown-track" />}
              {spec.stacked
                ? spec.series.map((series, seriesIndex) => {
                    const value = series.values[index]
                    if (value === null || value <= 0) return null
                    return (
                      <span
                        key={seriesIndex}
                        className="md-chart-fill stacked"
                        style={{ width: `${(value / max) * 100}%`, background: seriesColor(seriesIndex, spec.series.length) }}
                        title={`${series.name || 'Value'}: ${formatChartValue(value, spec.unit)}`}
                      />
                    )
                  })
                : spec.series.map((series, seriesIndex) => {
                    const value = series.values[index]
                    if (value === null) return null
                    return (
                      <span key={seriesIndex} className="md-chart-bar-line">
                        <span
                          className="md-chart-fill"
                          style={{
                            width: `${Math.max(0, (value / max) * 100)}%`,
                            background: seriesColor(seriesIndex, spec.series.length),
                          }}
                          title={`${series.name || 'Value'}: ${formatChartValue(value, spec.unit)}`}
                        />
                      </span>
                    )
                  })}
            </span>
            <span className={`md-chart-row-value${known ? '' : ' unknown'}`}>
              {spec.stacked
                ? formatChartValue(rowTotal, spec.unit)
                : formatChartValue(spec.series[0].values[index], spec.unit)}
            </span>
          </div>
        )
      })}
    </div>
  )
}

/* ------------------------------------------------------------------ *
 * Columns — grouped or stacked, over an ordered axis
 * ------------------------------------------------------------------ */

const PAD_L = 42
const PAD_R = 8
const PAD_T = 10
const PAD_B = 20

function ColumnChart({ spec, width }: { spec: ChartSpec; width: number }) {
  const [hover, setHover] = useState<HoverState | null>(null)
  const height = 176
  const innerW = Math.max(40, width - PAD_L - PAD_R)
  const innerH = height - PAD_T - PAD_B
  const max = axisMax(spec)
  const count = spec.labels.length
  const slot = innerW / Math.max(1, count)
  const barWidth = Math.max(2, slot - 3)
  const totals = stackTotals(spec)
  const y = (value: number) => PAD_T + innerH - (value / max) * innerH

  return (
    <div className="md-chart-plot" style={{ position: 'relative' }}>
      <svg
        width={width}
        height={height}
        role="img"
        aria-label={describe(spec)}
        onMouseMove={(event) => {
          const rect = event.currentTarget.getBoundingClientRect()
          const index = Math.floor((event.clientX - rect.left - PAD_L) / (slot || 1))
          setHover({ index: Math.max(0, Math.min(count - 1, index)), x: PAD_L + (index + 0.5) * slot })
        }}
        onMouseLeave={() => setHover(null)}
      >
        {tickValues(max).map((value, index) => (
          <g key={index}>
            <line x1={PAD_L} x2={width - PAD_R} y1={y(value)} y2={y(value)} stroke="var(--border-subtle)" />
            <text x={PAD_L - 6} y={y(value) + 3.5} textAnchor="end" fontFamily="var(--font-mono)" fontSize="9" fill="var(--fg-faint)">
              {formatChartTick(value, spec.unit)}
            </text>
          </g>
        ))}
        {spec.labels.map((label, index) => {
          const left = PAD_L + index * slot + (slot - barWidth) / 2
          const active = hover?.index === index
          const groupWidth = spec.stacked ? barWidth : barWidth / spec.series.length
          let stackAcc = 0
          return (
            <g key={index}>
              {active && <rect x={left - 1} y={PAD_T} width={barWidth + 2} height={innerH} fill="var(--ink-750)" rx="2" />}
              {spec.series.map((series, seriesIndex) => {
                const value = series.values[index]
                if (value === null || value <= 0) return null
                const barHeight = (value / max) * innerH
                if (spec.stacked) {
                  const top = y(stackAcc + value)
                  stackAcc += value
                  return (
                    <rect
                      key={seriesIndex}
                      x={left}
                      y={top}
                      width={barWidth}
                      // A 2px surface gap keeps neighbouring segments readable.
                      height={Math.max(1, barHeight - (barHeight > 4 ? 2 : 0))}
                      rx="1"
                      fill={seriesColor(seriesIndex, spec.series.length)}
                      opacity={hover === null || active ? 1 : 0.45}
                    />
                  )
                }
                return (
                  <rect
                    key={seriesIndex}
                    x={left + seriesIndex * groupWidth}
                    y={y(value)}
                    width={Math.max(1.5, groupWidth - 1)}
                    height={Math.max(0, barHeight)}
                    rx="1"
                    fill={seriesColor(seriesIndex, spec.series.length)}
                    opacity={hover === null || active ? 1 : 0.45}
                  />
                )
              })}
              {spec.series.every((series) => series.values[index] === null) && (
                <line
                  x1={left}
                  x2={left + barWidth}
                  y1={y(0) - 1}
                  y2={y(0) - 1}
                  stroke="var(--fg-faint)"
                  strokeWidth="2"
                  strokeDasharray="2 2"
                />
              )}
              {showLabelAt(index, count, 8) && (
                <text
                  x={left + barWidth / 2}
                  y={height - 6}
                  textAnchor="middle"
                  fontFamily="var(--font-mono)"
                  fontSize="9"
                  fill="var(--fg-faint)"
                >
                  {shortLabel(label)}
                </text>
              )}
            </g>
          )
        })}
      </svg>
      {hover !== null && <HoverTooltip spec={spec} hover={hover} width={width} total={totals[hover.index]} />}
    </div>
  )
}

/* ------------------------------------------------------------------ *
 * Lines and areas — trends, with gaps where evidence is missing
 * ------------------------------------------------------------------ */

function LineChart({ spec, width }: { spec: ChartSpec; width: number }) {
  const [hover, setHover] = useState<HoverState | null>(null)
  const height = 176
  const innerW = Math.max(40, width - PAD_L - PAD_R)
  const innerH = height - PAD_T - PAD_B
  const max = axisMax(spec)
  const count = spec.labels.length
  const x = (index: number) => PAD_L + (count <= 1 ? innerW / 2 : (index / (count - 1)) * innerW)
  const y = (value: number) => PAD_T + innerH - (value / max) * innerH
  const totals = stackTotals(spec)

  return (
    <div className="md-chart-plot" style={{ position: 'relative' }}>
      <svg
        width={width}
        height={height}
        role="img"
        aria-label={describe(spec)}
        onMouseMove={(event) => {
          const rect = event.currentTarget.getBoundingClientRect()
          const raw = count <= 1 ? 0 : Math.round(((event.clientX - rect.left - PAD_L) / innerW) * (count - 1))
          const index = Math.max(0, Math.min(count - 1, raw))
          setHover({ index, x: x(index) })
        }}
        onMouseLeave={() => setHover(null)}
      >
        {tickValues(max).map((value, index) => (
          <g key={index}>
            <line x1={PAD_L} x2={width - PAD_R} y1={y(value)} y2={y(value)} stroke="var(--border-subtle)" />
            <text x={PAD_L - 6} y={y(value) + 3.5} textAnchor="end" fontFamily="var(--font-mono)" fontSize="9" fill="var(--fg-faint)">
              {formatChartTick(value, spec.unit)}
            </text>
          </g>
        ))}
        {spec.series.map((series, seriesIndex) => {
          const color = seriesColor(seriesIndex, spec.series.length)
          const segments = pathSegments(series.values, x, y)
          return (
            <g key={seriesIndex}>
              {spec.kind === 'area' &&
                segments.map((segment, index) => (
                  <path
                    key={`fill-${index}`}
                    d={`${segment.path} L${segment.endX} ${y(0)} L${segment.startX} ${y(0)} Z`}
                    fill={color}
                    fillOpacity={spec.series.length > 1 ? 0.1 : 0.18}
                  />
                ))}
              {segments.map((segment, index) => (
                <path
                  key={`line-${index}`}
                  d={segment.path}
                  fill="none"
                  stroke={color}
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              ))}
              {count <= 24 &&
                series.values.map((value, index) =>
                  value === null ? null : (
                    <circle key={index} cx={x(index)} cy={y(value)} r="2.5" fill={color} />
                  ),
                )}
            </g>
          )
        })}
        {spec.labels.map((label, index) =>
          showLabelAt(index, count, 6) ? (
            <text
              key={index}
              x={x(index)}
              y={height - 6}
              textAnchor={index === 0 ? 'start' : index === count - 1 ? 'end' : 'middle'}
              fontFamily="var(--font-mono)"
              fontSize="9"
              fill="var(--fg-faint)"
            >
              {shortLabel(label)}
            </text>
          ) : null,
        )}
        {hover !== null && (
          <g>
            <line x1={x(hover.index)} x2={x(hover.index)} y1={PAD_T} y2={PAD_T + innerH} stroke="var(--border-strong)" />
            {spec.series.map((series, seriesIndex) => {
              const value = series.values[hover.index]
              if (value === null) return null
              return (
                <circle
                  key={seriesIndex}
                  cx={x(hover.index)}
                  cy={y(value)}
                  r="3.5"
                  fill="var(--ink-900)"
                  stroke={seriesColor(seriesIndex, spec.series.length)}
                  strokeWidth="2"
                />
              )
            })}
          </g>
        )}
      </svg>
      {hover !== null && <HoverTooltip spec={spec} hover={hover} width={width} total={totals[hover.index]} />}
    </div>
  )
}

/** Splits a series into drawable runs so unknown values become visible gaps. */
function pathSegments(
  values: (number | null)[],
  x: (index: number) => number,
  y: (value: number) => number,
): { path: string; startX: number; endX: number }[] {
  const segments: { path: string; startX: number; endX: number }[] = []
  let current: string[] = []
  let startX = 0
  let endX = 0
  const flush = () => {
    if (current.length === 0) return
    // A single known point between two gaps still deserves a mark: draw a
    // zero-length line so the round cap renders as a dot.
    if (current.length === 1) current.push(current[0].replace('M', 'L'))
    segments.push({ path: current.join(' '), startX, endX })
    current = []
  }
  values.forEach((value, index) => {
    if (value === null) {
      flush()
      return
    }
    const px = x(index)
    if (current.length === 0) startX = px
    endX = px
    current.push(`${current.length === 0 ? 'M' : 'L'}${px.toFixed(1)} ${y(value).toFixed(1)}`)
  })
  flush()
  return segments
}

/* ------------------------------------------------------------------ *
 * Donut and heatmap
 * ------------------------------------------------------------------ */

function DonutView({ spec }: { spec: ChartSpec }) {
  const values = spec.series[0].values.map((value) => value ?? 0)
  const total = values.reduce((sum, value) => sum + value, 0)
  const [active, setActive] = useState<number | null>(null)
  const segments = spec.labels.map((label, index) => ({
    value: values[index],
    color: seriesColor(index, Math.max(2, spec.labels.length)),
    label,
    valueText: formatChartValue(values[index], spec.unit),
    shareText: total === 0 ? '—' : `${((values[index] / total) * 100).toFixed(1)}%`,
  }))
  return (
    <div className="md-chart-donut">
      <Donut
        segments={segments}
        size={132}
        thickness={15}
        centerTop={formatChartTick(total, spec.unit)}
        centerBottom="Total"
        activeIndex={active}
        onHoverIndex={setActive}
        ariaLabel={describe(spec)}
      />
      <div className="md-chart-donut-legend">
        {segments.map((segment, index) => (
          <div
            key={index}
            className={`md-chart-donut-row${active === index ? ' active' : ''}`}
            onMouseEnter={() => setActive(index)}
            onMouseLeave={() => setActive(null)}
          >
            <span className="md-chart-swatch" style={{ background: segment.color }} />
            <span className="md-chart-donut-label" title={segment.label}>
              {segment.label}
            </span>
            <span className="md-chart-donut-value">{segment.valueText}</span>
            <span className="md-chart-donut-share">{segment.shareText}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function HeatmapView({ spec }: { spec: ChartSpec }) {
  const values = spec.series.flatMap((series) => series.values.filter((value): value is number => value !== null))
  const max = Math.max(1, ...values)
  const showColumnLabels = spec.labels.length <= 26
  return (
    <div className="md-chart-heatmap">
      {spec.series.map((series, rowIndex) => (
        <div key={rowIndex} className="md-chart-heatmap-row">
          <span className="md-chart-heatmap-label" title={series.name}>
            {series.name || `Row ${rowIndex + 1}`}
          </span>
          <span className="md-chart-heatmap-cells" style={{ gridTemplateColumns: `repeat(${spec.labels.length}, 1fr)` }}>
            {series.values.map((value, columnIndex) => (
              <span
                key={columnIndex}
                className={`md-chart-cell${value === null ? ' unknown' : ''}`}
                title={`${spec.labels[columnIndex]} · ${series.name || 'value'}: ${formatChartValue(value, spec.unit)}`}
                style={
                  value === null
                    ? undefined
                    : { background: `color-mix(in srgb, var(--cat-1) ${Math.round(10 + (value / max) * 90)}%, var(--ink-850))` }
                }
              />
            ))}
          </span>
        </div>
      ))}
      {showColumnLabels && (
        <div className="md-chart-heatmap-row">
          <span className="md-chart-heatmap-label" />
          <span className="md-chart-heatmap-axis" style={{ gridTemplateColumns: `repeat(${spec.labels.length}, 1fr)` }}>
            {spec.labels.map((label, index) => (
              <span key={index} className="md-chart-heatmap-tick">
                {showLabelAt(index, spec.labels.length, 12) ? shortLabel(label, 4) : ''}
              </span>
            ))}
          </span>
        </div>
      )}
    </div>
  )
}

/** The sequential scale a heatmap reads against, with its real endpoints. */
function HeatmapScale({ spec }: { spec: ChartSpec }) {
  const values = spec.series.flatMap((series) => series.values.filter((value): value is number => value !== null))
  if (values.length === 0) return null
  const min = Math.min(...values)
  const max = Math.max(...values)
  return (
    <div className="md-chart-legend">
      <span className="md-chart-legend-item">{formatChartValue(min, spec.unit)}</span>
      <span className="md-chart-scale" aria-hidden="true">
        {[0.15, 0.35, 0.55, 0.75, 1].map((step) => (
          <span key={step} style={{ background: `color-mix(in srgb, var(--cat-1) ${Math.round(step * 100)}%, var(--ink-850))` }} />
        ))}
      </span>
      <span className="md-chart-legend-item">{formatChartValue(max, spec.unit)}</span>
    </div>
  )
}

/* ------------------------------------------------------------------ *
 * Table view
 * ------------------------------------------------------------------ */

function TableView({ spec }: { spec: ChartSpec }) {
  return (
    <div className="md-table-wrap">
      <table className="md-table">
        <thead>
          <tr>
            <th>{spec.kind === 'heatmap' ? 'Row' : 'Label'}</th>
            {spec.kind === 'heatmap'
              ? spec.labels.map((label, index) => <th key={index}>{label}</th>)
              : spec.series.map((series, index) => (
                  <th key={index} style={{ textAlign: 'right' }}>
                    {series.name || 'Value'}
                  </th>
                ))}
          </tr>
        </thead>
        <tbody>
          {spec.kind === 'heatmap'
            ? spec.series.map((series, rowIndex) => (
                <tr key={rowIndex}>
                  <td>{series.name || `Row ${rowIndex + 1}`}</td>
                  {series.values.map((value, columnIndex) => (
                    <td key={columnIndex} style={{ textAlign: 'right' }}>
                      {formatChartValue(value, spec.unit)}
                    </td>
                  ))}
                </tr>
              ))
            : spec.labels.map((label, index) => (
                <tr key={index}>
                  <td>{label}</td>
                  {spec.series.map((series, seriesIndex) => (
                    <td key={seriesIndex} style={{ textAlign: 'right' }}>
                      {formatChartValue(series.values[index], spec.unit)}
                    </td>
                  ))}
                </tr>
              ))}
        </tbody>
      </table>
    </div>
  )
}

/* ------------------------------------------------------------------ *
 * Helpers
 * ------------------------------------------------------------------ */

/** A date-shaped label is trimmed to its most informative part for an axis. */
function shortLabel(label: string, max = 7): string {
  const isoDay = /^(\d{4})-(\d{2})-(\d{2})/.exec(label)
  if (isoDay) {
    const hour = /T(\d{2})/.exec(label)
    return hour ? `${isoDay[3]}·${hour[1]}` : `${isoDay[2]}-${isoDay[3]}`
  }
  return label.length > max ? `${label.slice(0, max - 1)}…` : label
}

/** One sentence describing the chart for assistive technology. */
function describe(spec: ChartSpec): string {
  const form = spec.stacked ? `stacked ${spec.kind}` : spec.kind
  const names = spec.series.map((series) => series.name).filter((name) => name !== '')
  const seriesPart = names.length > 0 ? ` of ${names.join(', ')}` : ''
  const unitPart = spec.unit === 'none' ? '' : ` in ${unitNoun(spec.unit)}`
  return `${form} chart${seriesPart}${unitPart} across ${spec.labels.length} categories${spec.title ? `: ${spec.title}` : ''}`
}

function unitNoun(unit: ChartUnit): string {
  switch (unit) {
    case 'usd':
      return 'US dollars'
    case 'ms':
      return 'milliseconds'
    case 'seconds':
      return 'seconds'
    case 'percent':
      return 'percent'
    default:
      return unit
  }
}

/* ------------------------------------------------------------------ *
 * Entry point
 * ------------------------------------------------------------------ */

/**
 * Renders an already-validated spec. Split out from the fence entry point so a
 * `pie` diagram — which is a share-of-total chart wearing Mermaid syntax — gets
 * the same donut, tooltips, table view, and copy control.
 */
export function ChartFigure({ spec, source, kind }: { spec: ChartSpec; source: string; kind?: string }) {
  const [showTable, setShowTable] = useState(false)
  const [bodyRef, width] = useWidth(340)

  const footnotes: string[] = []
  if (spec.note !== null) footnotes.push(spec.note)
  if (spec.hasUnknown) footnotes.push('Gaps are values the evidence does not establish. They are unknown, not zero.')

  const plotWidth = Math.max(220, width)
  return (
    <ArtifactShell
      kind={kind ?? KIND_LABEL[spec.kind]}
      title={spec.title}
      meta={chartProvenance(spec)}
      source={source}
      footnotes={footnotes}
      actions={<ArtifactViewToggle showingTable={showTable} onToggle={() => setShowTable((value) => !value)} />}
    >
      <div ref={bodyRef}>
        {showTable ? (
          <TableView spec={spec} />
        ) : (
          <>
            {spec.kind === 'bar' && <BarRows spec={spec} />}
            {spec.kind === 'column' && <ColumnChart spec={spec} width={plotWidth} />}
            {(spec.kind === 'line' || spec.kind === 'area') && <LineChart spec={spec} width={plotWidth} />}
            {spec.kind === 'donut' && <DonutView spec={spec} />}
            {spec.kind === 'heatmap' && <HeatmapView spec={spec} />}
            {/* A heatmap encodes magnitude in one hue, so a categorical legend
                would claim an identity its rows do not have. */}
            {spec.kind === 'heatmap' ? <HeatmapScale spec={spec} /> : spec.kind !== 'donut' && <Legend spec={spec} />}
          </>
        )}
      </div>
    </ArtifactShell>
  )
}

/** Parses one ```chart / ```plot fence and renders it, or explains why it cannot. */
export function ChartArtifact({ source, info, closed }: { source: string; info: string | null; closed: boolean }) {
  const result = useMemo(() => (closed ? parseChartSpec(source, info) : null), [source, info, closed])
  if (result === null) return <ArtifactPending label="Chart" />
  if (!result.ok) {
    return <ArtifactFallback kind="Chart" error={result.error} hint={result.hint} source={source} lang={info ?? 'chart'} />
  }
  return <ChartFigure spec={result.spec} source={source} />
}
