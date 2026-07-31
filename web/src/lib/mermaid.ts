/**
 * A small Mermaid subset — parser *and* layout engine — for the ```mermaid
 * fences the analytics assistant writes.
 *
 * Why not the real `mermaid` package: it renders by building an HTML/SVG string
 * that the host must inject with `dangerouslySetInnerHTML`, and its per-diagram
 * `%%{init}%%` directives can re-enable `htmlLabels` and raise the security
 * level from inside the diagram text. That is a poor fit for text a language
 * model produced from untrusted tool results, and it contradicts this panel's
 * standing rule that assistant output only ever becomes real DOM nodes. It is
 * also ~2 MB for a dashboard that self-hosts everything so it works offline.
 *
 * So this module does what the rest of the app does: parse into a model, lay it
 * out with plain arithmetic, and let React emit `<rect>`/`<path>`/`<text>`. No
 * DOM is touched here, which keeps the whole thing unit-testable under
 * `node --test`, and text can never become markup.
 *
 * Supported, because these are the shapes worth drawing in a usage report and
 * the shapes the assistant's own prompt asks for:
 *
 *  - `flowchart` / `graph` with TD, TB, BT, LR, RL directions
 *  - `sequenceDiagram` with participants, messages, and notes
 *  - `pie` (returned as slices; the caller renders it with the chart artifact,
 *    so a share diagram gets the same tooltips and table view as a donut)
 *
 * Deliberately ignored: `style`, `classDef`, and `click`. Colors belong to the
 * design system, and `click` is an interaction directive that must never come
 * from model output. Unsupported constructs produce a warning or a diagnostic
 * rather than a wrong picture.
 */

/* ------------------------------------------------------------------ *
 * Public model
 * ------------------------------------------------------------------ */

export type DiagramDirection = 'TD' | 'BT' | 'LR' | 'RL'

export type NodeShape = 'rect' | 'round' | 'stadium' | 'circle' | 'diamond' | 'hexagon' | 'cylinder'

export type EdgeHead = 'arrow' | 'none' | 'cross' | 'circle'

export interface DiagramNode {
  id: string
  lines: string[]
  shape: NodeShape
  /** Center coordinates in the laid-out viewBox. */
  x: number
  y: number
  w: number
  h: number
}

export interface DiagramEdge {
  from: string
  to: string
  /** SVG path data from the source border to the target border. */
  path: string
  head: EdgeHead
  /** Arrowhead polygon points, already oriented along the incoming tangent. */
  headPoints: string
  dashed: boolean
  thick: boolean
  label: string | null
  labelX: number
  labelY: number
  labelW: number
}

export interface FlowchartDiagram {
  kind: 'flowchart'
  direction: DiagramDirection
  width: number
  height: number
  nodes: DiagramNode[]
  edges: DiagramEdge[]
}

export interface SequenceActor {
  id: string
  label: string
  x: number
  w: number
}

export interface SequenceMessage {
  fromX: number
  toX: number
  y: number
  label: string
  dashed: boolean
  head: EdgeHead
  selfCall: boolean
}

export interface SequenceNote {
  x: number
  y: number
  w: number
  h: number
  lines: string[]
}

export interface SequenceDiagram {
  kind: 'sequence'
  width: number
  height: number
  actorHeight: number
  lifelineTop: number
  lifelineBottom: number
  actors: SequenceActor[]
  messages: SequenceMessage[]
  notes: SequenceNote[]
}

export interface PieSlice {
  label: string
  value: number
}

export interface PieDiagram {
  kind: 'pie'
  title: string | null
  slices: PieSlice[]
}

export type Diagram = FlowchartDiagram | SequenceDiagram | PieDiagram

export interface DiagramFailure {
  ok: false
  error: string
  hint: string | null
}

export type DiagramResult =
  | { ok: true; diagram: Diagram; warnings: string[] }
  | DiagramFailure

/* ------------------------------------------------------------------ *
 * Limits
 * ------------------------------------------------------------------ */

const MAX_NODES = 40
const MAX_EDGES = 80
const MAX_ACTORS = 10
const MAX_MESSAGES = 40
const MAX_SLICES = 8
const MAX_LABEL_CHARS = 80
const MAX_LINES = 400

/* ------------------------------------------------------------------ *
 * Geometry constants
 * ------------------------------------------------------------------ */

const FONT_SIZE = 12
const LINE_HEIGHT = 15
const NODE_PAD_X = 18
const NODE_MIN_W = 62
const NODE_MAX_W = 190
const NODE_MIN_H = 34
const RANK_GAP = 54
const SIBLING_GAP = 18
const MARGIN = 12
const WRAP_CHARS = 20

const ACTOR_H = 30
const ACTOR_GAP = 26
const MESSAGE_GAP = 34
const SEQUENCE_TOP = 10
const SELF_CALL_WIDTH = 30

/* ------------------------------------------------------------------ *
 * Text measurement
 * ------------------------------------------------------------------ */

const NARROW = new Set([...'iljtfrI.,:;\'|!()[]{}'])
const WIDE = new Set([...'mwMW@%'])

/**
 * Approximates the rendered width of a string. There is no DOM here — and the
 * layout must be deterministic for tests — so character classes stand in for
 * font metrics. It is close enough that boxes never clip their text.
 */
export function measureText(text: string, fontSize = FONT_SIZE): number {
  let units = 0
  for (const character of text) {
    if (NARROW.has(character)) units += 0.33
    else if (WIDE.has(character)) units += 0.92
    else if (character === ' ') units += 0.29
    else if (character >= 'A' && character <= 'Z') units += 0.68
    else units += 0.58
  }
  return units * fontSize
}

/** Greedy word wrap with a hard break for tokens longer than the line budget. */
export function wrapLabel(text: string, maxChars = WRAP_CHARS, maxLines = 3): string[] {
  const words = text.split(/\s+/).filter((word) => word !== '')
  if (words.length === 0) return ['']
  const lines: string[] = []
  let current = ''
  for (const word of words) {
    const candidate = current === '' ? word : `${current} ${word}`
    if (candidate.length <= maxChars) {
      current = candidate
      continue
    }
    if (current !== '') lines.push(current)
    if (word.length <= maxChars) {
      current = word
      continue
    }
    let rest = word
    while (rest.length > maxChars) {
      lines.push(rest.slice(0, maxChars - 1) + '-')
      rest = rest.slice(maxChars - 1)
    }
    current = rest
  }
  if (current !== '') lines.push(current)
  if (lines.length <= maxLines) return lines
  const kept = lines.slice(0, maxLines)
  kept[maxLines - 1] = `${kept[maxLines - 1].slice(0, Math.max(1, maxChars - 1))}…`
  return kept
}

/**
 * Neutralizes display-hostile code points (C0/C1 controls, zero-width and
 * bidirectional overrides) and bounds the length, mirroring the chart artifact's
 * treatment of model-supplied text.
 */
function cleanLabel(raw: string): string {
  let scrubbed = ''
  for (const character of raw) {
    const code = character.codePointAt(0) ?? 0
    const hostile =
      code < 0x20 ||
      (code >= 0x7f && code <= 0x9f) ||
      code === 0xad ||
      (code >= 0x200b && code <= 0x200f) ||
      code === 0x2028 ||
      code === 0x2029 ||
      (code >= 0x202a && code <= 0x202e) ||
      (code >= 0x2060 && code <= 0x2064) ||
      (code >= 0x2066 && code <= 0x2069) ||
      code === 0xfeff
    scrubbed += hostile ? ' ' : character
  }
  const collapsed = scrubbed.replace(/<br\s*\/?>/gi, ' ').replace(/\s+/g, ' ').trim()
  const unquoted = collapsed.replace(/^"(.*)"$/s, '$1').trim()
  return unquoted.length > MAX_LABEL_CHARS ? `${unquoted.slice(0, MAX_LABEL_CHARS - 1)}…` : unquoted
}

/* ------------------------------------------------------------------ *
 * Fence detection and source preparation
 * ------------------------------------------------------------------ */

const DIAGRAM_FENCE_NAMES = new Set(['mermaid', 'diagram'])

/** True when a fence info string opens a diagram artifact. */
export function isDiagramFence(info: string | null): boolean {
  if (info === null) return false
  const first = info.trim().toLowerCase().split(/[\s|:,]+/)[0]
  return DIAGRAM_FENCE_NAMES.has(first)
}

interface SourceLine {
  text: string
  number: number
}

/**
 * Splits the fence body into meaningful lines. Comments and `%%{init}%%`
 * directives are dropped here: the directive form is Mermaid's own configuration
 * escape hatch, and nothing in it is honored by this renderer.
 */
function readLines(source: string): SourceLine[] {
  const out: SourceLine[] = []
  source.split('\n').forEach((raw, index) => {
    const text = raw.replace(/\t/g, '  ').trim()
    if (text === '' || text.startsWith('%%')) return
    out.push({ text, number: index + 1 })
  })
  return out
}

/* ------------------------------------------------------------------ *
 * Entry point
 * ------------------------------------------------------------------ */

/** Parses and lays out one Mermaid fence body. */
export function parseMermaid(source: string): DiagramResult {
  const lines = readLines(source)
  if (lines.length === 0) {
    return { ok: false, error: 'The diagram block is empty.', hint: 'Start it with "flowchart TD", "sequenceDiagram", or "pie".' }
  }
  if (lines.length > MAX_LINES) {
    return { ok: false, error: `The diagram has ${lines.length} statements; the limit is ${MAX_LINES}.`, hint: null }
  }

  const header = lines[0].text
  const flowchart = /^(?:flowchart|graph)\b\s*([A-Za-z]{2})?/i.exec(header)
  if (flowchart) {
    return parseFlowchart(lines.slice(1), normalizeDirection(flowchart[1]))
  }
  if (/^sequenceDiagram\b/i.test(header)) {
    return parseSequence(lines.slice(1))
  }
  if (/^pie\b/i.test(header)) {
    return parsePie(header, lines.slice(1))
  }
  return {
    ok: false,
    error: `"${cleanLabel(header.split(/\s+/)[0] ?? header)}" is not a supported diagram type.`,
    hint: 'Supported types are flowchart (or graph), sequenceDiagram, and pie.',
  }
}

function normalizeDirection(raw: string | undefined): DiagramDirection {
  switch ((raw ?? 'TD').toUpperCase()) {
    case 'LR':
      return 'LR'
    case 'RL':
      return 'RL'
    case 'BT':
      return 'BT'
    default:
      return 'TD'
  }
}

/* ------------------------------------------------------------------ *
 * Flowchart parsing
 * ------------------------------------------------------------------ */

interface RawNode {
  id: string
  lines: string[]
  shape: NodeShape
  w: number
  h: number
}

interface RawEdge {
  from: string
  to: string
  head: EdgeHead
  dashed: boolean
  thick: boolean
  label: string | null
}

/** Node declaration forms, longest delimiters first so `[[x]]` beats `[x]`. */
const NODE_FORMS: { open: string; close: string; shape: NodeShape }[] = [
  { open: '([', close: '])', shape: 'stadium' },
  { open: '[[', close: ']]', shape: 'rect' },
  { open: '[(', close: ')]', shape: 'cylinder' },
  { open: '((', close: '))', shape: 'circle' },
  { open: '{{', close: '}}', shape: 'hexagon' },
  { open: '[/', close: '/]', shape: 'rect' },
  { open: '[', close: ']', shape: 'rect' },
  { open: '(', close: ')', shape: 'round' },
  { open: '{', close: '}', shape: 'diamond' },
  { open: '>', close: ']', shape: 'rect' },
]

// Ids stay conservative on purpose: allowing "-" or "." would make "A-->B"
// ambiguous with an id, and the assistant's prompt asks for simple ids anyway.
const ID_PATTERN = /^[A-Za-z0-9_]+/

/**
 * Reads one node reference at `index`, returning its id, any inline label, and
 * where the reference ends. A bare id (`B`) is a reference to a node that may be
 * declared with its label elsewhere.
 */
function readNodeRef(text: string, index: number): { id: string; label: string | null; shape: NodeShape; end: number } | null {
  const idMatch = ID_PATTERN.exec(text.slice(index))
  if (!idMatch) return null
  const id = idMatch[0]
  const cursor = index + id.length
  for (const form of NODE_FORMS) {
    if (!text.startsWith(form.open, cursor)) continue
    const close = text.indexOf(form.close, cursor + form.open.length)
    if (close < 0) continue
    return {
      id,
      label: text.slice(cursor + form.open.length, close),
      shape: form.shape,
      end: close + form.close.length,
    }
  }
  return { id, label: null, shape: 'rect', end: cursor }
}

/**
 * Matches one link operator: solid/dotted/thick lines, an optional inline label
 * (`-- text -->`), and the arrowhead variants Mermaid spells `>`, `x`, and `o`.
 */
const LINK_PATTERN =
  /^\s*(?:(-{2,}|={2,}|-\.-|-\.{1,})\s*([^->=|]*?)\s*(-{1,}\.?-*>|={2,}>|-{2,}>|-{2,}x|-{2,}o|={2,}|-{2,}|-\.->)|(-\.->|-{2,}>|={2,}>|-{2,}x|-{2,}o|-\.-|-{2,}|={2,}))\s*(?:\|([^|]*)\|)?\s*/

type LinkStyle = Omit<RawEdge, 'from' | 'to'>

function classifyLink(operator: string, tail: string, inlineLabel: string, pipeLabel: string | undefined): LinkStyle {
  const combined = `${operator}${tail}`
  const dashed = combined.includes('.')
  const thick = combined.includes('=')
  const end = tail || operator
  let head: EdgeHead = 'arrow'
  if (end.endsWith('x')) head = 'cross'
  else if (end.endsWith('o')) head = 'circle'
  else if (!end.endsWith('>')) head = 'none'
  // A dotted link writes its label between the dots (-. text .->), so the
  // opening dot of the closing marker lands at the end of the captured label.
  const raw = (pipeLabel ?? (dashed ? inlineLabel.replace(/\s*\.$/, '') : inlineLabel) ?? '').trim()
  const label = raw === '' ? null : cleanLabel(raw)
  return { head, dashed, thick, label }
}

function parseFlowchart(lines: SourceLine[], direction: DiagramDirection): DiagramResult {
  const nodes = new Map<string, RawNode>()
  const edges: RawEdge[] = []
  const warnings: string[] = []
  const warn = (message: string) => {
    if (!warnings.includes(message)) warnings.push(message)
  }

  const declare = (id: string, label: string | null, shape: NodeShape) => {
    const existing = nodes.get(id)
    if (existing) {
      if (label !== null) {
        existing.lines = wrapLabel(cleanLabel(label))
        existing.shape = shape
      }
      return
    }
    nodes.set(id, { id, lines: wrapLabel(cleanLabel(label ?? id)), shape, w: 0, h: 0 })
  }

  for (const line of lines) {
    const text = line.text
    if (/^(?:style|classDef|class|linkStyle|click)\b/i.test(text)) {
      warn('Styling and interaction directives are ignored; the dashboard theme paints the diagram.')
      continue
    }
    if (/^subgraph\b/i.test(text) || /^end$/i.test(text)) {
      warn('Subgraph grouping is not drawn; its nodes appear in the main graph.')
      continue
    }
    if (/^direction\b/i.test(text)) continue
    if (text.includes('&')) {
      return {
        ok: false,
        error: 'Multi-node links written with "&" are not supported.',
        hint: 'Write one link per line, for example "A --> C" and "B --> C".',
      }
    }

    // A statement is a chain: node (link node)*. `pending` carries the link
    // seen after the previous node until its target is read.
    let cursor = 0
    let previous: string | null = null
    let pending: LinkStyle | null = null
    while (cursor < text.length) {
      const ref = readNodeRef(text, cursor)
      if (ref === null) {
        return {
          ok: false,
          error: `Line ${line.number} is not a node or link statement.`,
          hint: 'Use "A[Label] --> B[Label]" with ids made of letters, digits, and underscores.',
        }
      }
      declare(ref.id, ref.label, ref.shape)
      if (previous !== null && pending !== null) {
        edges.push({ ...pending, from: previous, to: ref.id })
        pending = null
      }
      previous = ref.id
      cursor = ref.end

      const rest = text.slice(cursor)
      if (rest.trim() === '') break
      const link = LINK_PATTERN.exec(rest)
      if (!link) {
        return {
          ok: false,
          error: `Line ${line.number} has a link this renderer does not understand.`,
          hint: 'Supported links are -->, ---, -.->, ==>, --x, --o, with an optional |label|.',
        }
      }
      pending = classifyLink(link[1] ?? link[4] ?? '', link[3] ?? '', link[2] ?? '', link[5])
      cursor += link[0].length
    }
    if (pending !== null) {
      return { ok: false, error: `Line ${line.number} ends with a link that has no target.`, hint: null }
    }

    if (nodes.size > MAX_NODES) {
      return { ok: false, error: `The diagram has more than ${MAX_NODES} nodes.`, hint: 'Summarize the structure or split it into two diagrams.' }
    }
    if (edges.length > MAX_EDGES) {
      return { ok: false, error: `The diagram has more than ${MAX_EDGES} links.`, hint: 'Summarize the structure or split it into two diagrams.' }
    }
  }

  if (nodes.size === 0) {
    return { ok: false, error: 'The flowchart declares no nodes.', hint: 'Add statements such as "A[Start] --> B[End]".' }
  }

  return { ok: true, diagram: layoutFlowchart([...nodes.values()], edges, direction), warnings }
}

/* ------------------------------------------------------------------ *
 * Flowchart layout
 * ------------------------------------------------------------------ */

function sizeNode(node: RawNode): void {
  const textWidth = Math.max(...node.lines.map((line) => measureText(line)))
  // A rhombus only contains half its bounding width at mid-height, so a
  // diamond has to grow with its text or the label spills past the points.
  const extra =
    node.shape === 'diamond'
      ? Math.max(28, textWidth * 0.55)
      : node.shape === 'hexagon'
        ? 22
        : node.shape === 'circle'
          ? 26
          : 0
  node.w = Math.min(NODE_MAX_W + 40, Math.max(NODE_MIN_W, Math.round(textWidth + NODE_PAD_X * 2 + extra)))
  node.h = Math.max(NODE_MIN_H, node.lines.length * LINE_HEIGHT + 16) + (node.shape === 'diamond' ? 14 : 0)
  if (node.shape === 'circle') {
    const side = Math.max(node.w, node.h)
    node.w = side
    node.h = side
  }
}

/**
 * Longest-path layering. Relaxation is capped by the node count so a cyclic
 * graph — which a model will occasionally write — terminates with a sensible
 * layering instead of spinning.
 */
function assignRanks(nodes: RawNode[], edges: RawEdge[]): Map<string, number> {
  const rank = new Map<string, number>(nodes.map((node) => [node.id, 0]))
  const targets = new Set(edges.map((edge) => edge.to))
  for (const node of nodes) if (!targets.has(node.id)) rank.set(node.id, 0)
  const limit = Math.max(1, nodes.length)
  for (let pass = 0; pass < limit; pass++) {
    let moved = false
    for (const edge of edges) {
      const from = rank.get(edge.from) ?? 0
      const to = rank.get(edge.to) ?? 0
      if (to < from + 1 && from + 1 <= limit) {
        rank.set(edge.to, from + 1)
        moved = true
      }
    }
    if (!moved) break
  }
  return rank
}

/** One barycenter sweep per direction: cheap, and enough to untangle small graphs. */
function orderRanks(ranks: Map<number, RawNode[]>, edges: RawEdge[], rank: Map<string, number>): void {
  const position = new Map<string, number>()
  const reindex = () => {
    for (const group of ranks.values()) group.forEach((node, index) => position.set(node.id, index))
  }
  reindex()
  const sortedRanks = [...ranks.keys()].sort((a, b) => a - b)
  for (const pass of [0, 1]) {
    const order = pass === 0 ? sortedRanks : [...sortedRanks].reverse()
    for (const rankIndex of order) {
      const group = ranks.get(rankIndex)
      if (!group || group.length < 2) continue
      const barycenter = new Map<string, number>()
      for (const node of group) {
        const neighbors = edges
          .filter((edge) =>
            pass === 0
              ? edge.to === node.id && (rank.get(edge.from) ?? 0) < rankIndex
              : edge.from === node.id && (rank.get(edge.to) ?? 0) > rankIndex,
          )
          .map((edge) => position.get(pass === 0 ? edge.from : edge.to) ?? 0)
        barycenter.set(node.id, neighbors.length === 0 ? position.get(node.id) ?? 0 : neighbors.reduce((a, b) => a + b, 0) / neighbors.length)
      }
      group.sort((a, b) => (barycenter.get(a.id) ?? 0) - (barycenter.get(b.id) ?? 0))
      reindex()
    }
  }
}

/**
 * Where a straight line from a node's center toward (dx, dy) leaves its
 * outline. Diamonds and circles get their real boundary rather than the
 * bounding box, so an arrowhead lands on the shape instead of floating beside
 * it or hiding inside it.
 */
function borderPoint(node: { x: number; y: number; w: number; h: number; shape: NodeShape }, dx: number, dy: number): [number, number] {
  if (dx === 0 && dy === 0) return [node.x, node.y]
  const hw = node.w / 2
  const hh = node.h / 2
  let scale: number
  if (node.shape === 'circle') {
    scale = Math.min(hw, hh) / Math.hypot(dx, dy)
  } else if (node.shape === 'diamond') {
    scale = 1 / (Math.abs(dx) / hw + Math.abs(dy) / hh)
  } else {
    const scaleX = dx === 0 ? Number.POSITIVE_INFINITY : hw / Math.abs(dx)
    const scaleY = dy === 0 ? Number.POSITIVE_INFINITY : hh / Math.abs(dy)
    scale = Math.min(scaleX, scaleY)
  }
  return [node.x + dx * scale, node.y + dy * scale]
}

function arrowPolygon(x: number, y: number, angle: number, size = 7): string {
  const spread = 0.42
  const p1 = [x - size * Math.cos(angle - spread), y - size * Math.sin(angle - spread)]
  const p2 = [x - size * Math.cos(angle + spread), y - size * Math.sin(angle + spread)]
  return `${round(x)},${round(y)} ${round(p1[0])},${round(p1[1])} ${round(p2[0])},${round(p2[1])}`
}

function round(value: number): number {
  return Math.round(value * 10) / 10
}

function layoutFlowchart(rawNodes: RawNode[], rawEdges: RawEdge[], direction: DiagramDirection): FlowchartDiagram {
  for (const node of rawNodes) sizeNode(node)

  const rank = assignRanks(rawNodes, rawEdges)
  const ranks = new Map<number, RawNode[]>()
  for (const node of rawNodes) {
    const index = rank.get(node.id) ?? 0
    const group = ranks.get(index)
    if (group) group.push(node)
    else ranks.set(index, [node])
  }
  orderRanks(ranks, rawEdges, rank)

  const horizontal = direction === 'LR' || direction === 'RL'
  const sortedRanks = [...ranks.keys()].sort((a, b) => a - b)

  // Rank axis: successive layers; cross axis: siblings inside a layer.
  const rankExtent = new Map<number, number>()
  let rankCursor = MARGIN
  const rankOffset = new Map<number, number>()
  for (const index of sortedRanks) {
    const group = ranks.get(index) ?? []
    const depth = Math.max(...group.map((node) => (horizontal ? node.w : node.h)))
    rankExtent.set(index, depth)
    rankOffset.set(index, rankCursor + depth / 2)
    rankCursor += depth + RANK_GAP
  }
  const rankTotal = rankCursor - RANK_GAP + MARGIN

  let crossTotal = 0
  const positions = new Map<string, { rankAxis: number; crossAxis: number }>()
  for (const index of sortedRanks) {
    const group = ranks.get(index) ?? []
    const span = group.reduce((sum, node) => sum + (horizontal ? node.h : node.w), 0) + SIBLING_GAP * Math.max(0, group.length - 1)
    let cursor = 0
    for (const node of group) {
      const size = horizontal ? node.h : node.w
      positions.set(node.id, { rankAxis: rankOffset.get(index) ?? 0, crossAxis: cursor + size / 2 - span / 2 })
      cursor += size + SIBLING_GAP
    }
    crossTotal = Math.max(crossTotal, span)
  }
  const crossCenter = crossTotal / 2 + MARGIN

  const flipRank = direction === 'BT' || direction === 'RL'
  const nodes: DiagramNode[] = rawNodes.map((node) => {
    const position = positions.get(node.id) ?? { rankAxis: 0, crossAxis: 0 }
    const rankAxis = flipRank ? rankTotal - position.rankAxis : position.rankAxis
    const crossAxis = crossCenter + position.crossAxis
    return {
      id: node.id,
      lines: node.lines,
      shape: node.shape,
      x: round(horizontal ? rankAxis : crossAxis),
      y: round(horizontal ? crossAxis : rankAxis),
      w: node.w,
      h: node.h,
    }
  })

  const byId = new Map(nodes.map((node) => [node.id, node]))
  const edges: DiagramEdge[] = []
  for (const edge of rawEdges) {
    const from = byId.get(edge.from)
    const to = byId.get(edge.to)
    if (!from || !to) continue
    const dx = to.x - from.x
    const dy = to.y - from.y
    const [x1, y1] = borderPoint(from, dx, dy)
    const [x2, y2] = borderPoint(to, -dx, -dy)
    // Control points offset along the rank axis give a calm S-curve between
    // layers and a straight line when the nodes are already aligned.
    const bend = horizontal ? Math.abs(x2 - x1) / 2 : Math.abs(y2 - y1) / 2
    const sign = horizontal ? Math.sign(x2 - x1) || 1 : Math.sign(y2 - y1) || 1
    const c1 = horizontal ? [x1 + bend * sign, y1] : [x1, y1 + bend * sign]
    const c2 = horizontal ? [x2 - bend * sign, y2] : [x2, y2 - bend * sign]
    const path = `M${round(x1)} ${round(y1)} C${round(c1[0])} ${round(c1[1])} ${round(c2[0])} ${round(c2[1])} ${round(x2)} ${round(y2)}`
    // Tangent of a cubic at t=1 is 3·(P3−P2); the arrowhead follows it.
    const angle = Math.atan2(y2 - c2[1], x2 - c2[0])
    edges.push({
      from: edge.from,
      to: edge.to,
      path,
      head: edge.head,
      headPoints: edge.head === 'arrow' ? arrowPolygon(x2, y2, angle) : '',
      dashed: edge.dashed,
      thick: edge.thick,
      label: edge.label,
      labelX: round((x1 + x2) / 2),
      labelY: round((y1 + y2) / 2),
      labelW: edge.label === null ? 0 : Math.round(measureText(edge.label, 10) + 10),
    })
  }

  const width = Math.round(horizontal ? rankTotal : crossTotal + MARGIN * 2)
  const height = Math.round(horizontal ? crossTotal + MARGIN * 2 : rankTotal)
  return { kind: 'flowchart', direction, width: Math.max(width, 120), height: Math.max(height, 80), nodes, edges }
}

/* ------------------------------------------------------------------ *
 * Sequence diagrams
 * ------------------------------------------------------------------ */

const SEQUENCE_MESSAGE = /^([A-Za-z0-9_]+)\s*(-{1,2}>>?|-{1,2}x|-{1,2}\))\s*([A-Za-z0-9_]+)\s*:\s*(.*)$/
const SEQUENCE_NOTE = /^note\s+(left of|right of|over)\s+([^:]+):\s*(.*)$/i
const SEQUENCE_PARTICIPANT = /^(?:participant|actor)\s+([A-Za-z0-9_]+)(?:\s+as\s+(.+))?$/i

interface RawMessage {
  from: string
  to: string
  label: string
  dashed: boolean
  head: EdgeHead
}

interface RawNote {
  anchors: string[]
  lines: string[]
  /** Index into the message list this note follows. */
  after: number
}

function parseSequence(lines: SourceLine[]): DiagramResult {
  const order: string[] = []
  const labels = new Map<string, string>()
  const messages: RawMessage[] = []
  const notes: RawNote[] = []
  const warnings: string[] = []
  const warn = (message: string) => {
    if (!warnings.includes(message)) warnings.push(message)
  }
  const ensure = (id: string) => {
    if (!order.includes(id)) {
      order.push(id)
      if (!labels.has(id)) labels.set(id, id)
    }
  }

  for (const line of lines) {
    const text = line.text
    if (/^(?:activate|deactivate|autonumber|box|rect|link|links)\b/i.test(text)) continue
    if (/^(?:loop|alt|else|opt|par|and|critical|break)\b/i.test(text) || /^end$/i.test(text)) {
      warn('Grouping blocks (loop, alt, opt) are not drawn; their messages appear in order.')
      continue
    }
    const participant = SEQUENCE_PARTICIPANT.exec(text)
    if (participant) {
      ensure(participant[1])
      labels.set(participant[1], cleanLabel(participant[2] ?? participant[1]))
      continue
    }
    const note = SEQUENCE_NOTE.exec(text)
    if (note) {
      const anchors = note[2].split(',').map((part) => part.trim()).filter((part) => part !== '')
      anchors.forEach(ensure)
      notes.push({ anchors, lines: wrapLabel(cleanLabel(note[3]), 28, 2), after: messages.length })
      continue
    }
    const message = SEQUENCE_MESSAGE.exec(text)
    if (message) {
      ensure(message[1])
      ensure(message[3])
      const operator = message[2]
      messages.push({
        from: message[1],
        to: message[3],
        label: cleanLabel(message[4]),
        dashed: operator.startsWith('--'),
        head: operator.endsWith('x') ? 'cross' : operator.endsWith(')') ? 'circle' : 'arrow',
      })
      continue
    }
    return {
      ok: false,
      error: `Line ${line.number} is not a participant, message, or note.`,
      hint: 'Use "A->>B: text", "B-->>A: text", "participant A as Label", or "Note over A,B: text".',
    }
  }

  if (order.length === 0) {
    return { ok: false, error: 'The sequence diagram has no participants.', hint: 'Add messages such as "web->>backend: POST /chat".' }
  }
  if (order.length > MAX_ACTORS) {
    return { ok: false, error: `The sequence diagram has ${order.length} participants; the limit is ${MAX_ACTORS}.`, hint: null }
  }
  if (messages.length > MAX_MESSAGES) {
    return { ok: false, error: `The sequence diagram has ${messages.length} messages; the limit is ${MAX_MESSAGES}.`, hint: null }
  }

  return { ok: true, diagram: layoutSequence(order, labels, messages, notes), warnings }
}

function layoutSequence(
  order: string[],
  labels: Map<string, string>,
  rawMessages: RawMessage[],
  rawNotes: RawNote[],
): SequenceDiagram {
  const actors: SequenceActor[] = []
  let cursor = MARGIN
  for (const id of order) {
    const label = labels.get(id) ?? id
    const w = Math.max(72, Math.round(measureText(label) + 26))
    actors.push({ id, label, x: round(cursor + w / 2), w })
    cursor += w + ACTOR_GAP
  }
  const width = Math.round(cursor - ACTOR_GAP + MARGIN)
  const centerOf = new Map(actors.map((actor) => [actor.id, actor.x]))

  const lifelineTop = SEQUENCE_TOP + ACTOR_H
  let y = lifelineTop + MESSAGE_GAP
  const messages: SequenceMessage[] = []
  const notes: SequenceNote[] = []
  const emitNotes = (after: number) => {
    for (const note of rawNotes.filter((candidate) => candidate.after === after)) {
      const anchors = note.anchors.map((id) => centerOf.get(id)).filter((x): x is number => x !== undefined)
      const textWidth = Math.max(...note.lines.map((line) => measureText(line, 11)))
      const w = Math.round(Math.max(80, textWidth + 20))
      const centre = anchors.length === 0 ? width / 2 : anchors.reduce((a, b) => a + b, 0) / anchors.length
      const h = note.lines.length * 14 + 12
      notes.push({ x: round(centre - w / 2), y: round(y - 12), w, h, lines: note.lines })
      y += h + 12
    }
  }

  emitNotes(0)
  rawMessages.forEach((message, index) => {
    const fromX = centerOf.get(message.from) ?? MARGIN
    const toX = centerOf.get(message.to) ?? MARGIN
    const selfCall = message.from === message.to
    messages.push({ fromX: round(fromX), toX: round(toX), y: round(y), label: message.label, dashed: message.dashed, head: message.head, selfCall })
    y += selfCall ? MESSAGE_GAP + 18 : MESSAGE_GAP
    emitNotes(index + 1)
  })

  const lifelineBottom = Math.round(y - MESSAGE_GAP / 2 + 10)
  return {
    kind: 'sequence',
    width: Math.max(width, 160),
    height: lifelineBottom + MARGIN,
    actorHeight: ACTOR_H,
    lifelineTop,
    lifelineBottom,
    actors,
    messages,
    notes,
  }
}

/** Widest point a self-call loop reaches, exported for the renderer's geometry. */
export const SELF_CALL_OFFSET = SELF_CALL_WIDTH

/* ------------------------------------------------------------------ *
 * Pie
 * ------------------------------------------------------------------ */

const PIE_SLICE = /^"?([^":]+?)"?\s*:\s*([0-9]*\.?[0-9]+)$/

function parsePie(header: string, lines: SourceLine[]): DiagramResult {
  const titleMatch = /^pie\s+(?:showData\s+)?(?:title\s+)?(.*)$/i.exec(header)
  let title = titleMatch && titleMatch[1].trim() !== '' ? cleanLabel(titleMatch[1]) : null
  const slices: PieSlice[] = []

  for (const line of lines) {
    const explicitTitle = /^title\s+(.+)$/i.exec(line.text)
    if (explicitTitle) {
      title = cleanLabel(explicitTitle[1])
      continue
    }
    const slice = PIE_SLICE.exec(line.text)
    if (!slice) {
      return {
        ok: false,
        error: `Line ${line.number} is not a pie slice.`,
        hint: 'Write slices as "label" : 42, one per line.',
      }
    }
    const value = Number(slice[2])
    if (!Number.isFinite(value) || value < 0) {
      return { ok: false, error: `Line ${line.number} has a value that is not a non-negative number.`, hint: null }
    }
    slices.push({ label: cleanLabel(slice[1]), value })
  }

  if (slices.length === 0) {
    return { ok: false, error: 'The pie diagram has no slices.', hint: 'Add lines such as "kimi-k2" : 62.' }
  }
  if (slices.length > MAX_SLICES) {
    return {
      ok: false,
      error: `The pie diagram has ${slices.length} slices; the limit is ${MAX_SLICES}.`,
      hint: 'Keep the largest slices plus one "Other", or use a chart block with a bar chart.',
    }
  }

  return { ok: true, diagram: { kind: 'pie', title, slices }, warnings: [] }
}
